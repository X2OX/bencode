package bencode

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strconv"
)

type Unmarshaler interface {
	UnmarshalBencode([]byte) error
}

var unmarshalerType = reflect.TypeOf(func() *Unmarshaler {
	var i Unmarshaler
	return &i
}()).Elem()

type decodeState struct {
	bytes.Buffer
	Scanner interface {
		io.ByteScanner
		io.Reader
	}
	Offset int64
}

func Unmarshal(data []byte, v interface{}) error {
	return (&decodeState{Scanner: bytes.NewBuffer(data)}).unmarshal(v)
}

func (d *decodeState) unmarshal(v interface{}) (err error) {
	defer func() {
		ee, ok := recover().(Error)
		if ee != nil {
			if ok {
				err = ee
			} else {
				panic(ee)
			}
		}
	}()

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return newError("invalid unmarshal arg error")
	}

	var ok bool

	if ok, err = parseValue(d, rv.Elem()); err != nil {
		return err
	} else if !ok {
		err = newError("syntax error (Offset: %d): unexpected 'e'", d.Offset-1)
	}
	return
}

func parseValue(d *decodeState, v reflect.Value) (bool, error) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Type().Implements(unmarshalerType) ||
		(v.Type().Kind() != reflect.Ptr && reflect.PtrTo(v.Type()).Implements(unmarshalerType)) {
		if err := unmarshalerDecoder(d, v); err != nil {
			return false, err
		}
		return true, nil
	}

	b := d.readByte()
	switch b {
	case 'e':
		return false, nil
	case 'd':
		return true, parseDict(d, v)
	case 'l':
		return true, parseList(d, v)
	case 'i':
		return true, parseInteger(d, v)
	default:
		if b >= '0' && b <= '9' {
			d.Reset()
			if err := d.WriteByte(b); err != nil {
				return false, err
			}
			return true, parseByteString(d, v)
		}
	}

	return false, newUnknownValueType(d.Offset-1, b)
}

func parseByteString(d *decodeState, v reflect.Value) error {
	length := d.readStringLength() // 读取长度
	b := d.readLength(length)      // 根据长度读取数据

	switch v.Kind() {
	case reflect.String:
		v.SetString(bytesAsString(b))
		return nil
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			break
		}
		v.SetBytes(b)
		return nil
	case reflect.Array:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			break
		}
		reflect.Copy(v, reflect.ValueOf(b))
		return nil
	case reflect.Interface:
		v.Set(reflect.ValueOf(bytesAsString(b)))
		return nil
	}
	return newError("cannot unmarshal a bencode %s into a %s", v, v.Type())
}

func parseInteger(d *decodeState, v reflect.Value) error {
	s := d.readInt()
	if v.Type() == bigIntType || (v.Kind() == reflect.Ptr && v.Elem().Type() == bigIntType) {
		return bigIntDecoder(d, v)
	}
	switch v.Kind() {
	case reflect.Interface:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return newError("cannot unmarshal a bencode %s into a %s", v, v.Type)
		}
		v.Set(reflect.ValueOf(n))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v.OverflowInt(n) {
			return newError("cannot unmarshal a bencode %s into a %s", v, v.Type)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil || v.OverflowUint(n) {
			return newError("cannot unmarshal a bencode %s into a %s", v, v.Type)
		}
		v.SetUint(n)
	case reflect.Bool:
		v.SetBool(s != "0")
	default:
		return newUnknownType()
	}
	return nil
}
func parseList(d *decodeState, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		for i := 0; ; i++ {
			v.Set(reflect.Append(v, reflect.Zero(v.Type().Elem())))
			if end, err := parseValue(d, v.Index(i)); err != nil {
				return err
			} else if end {
				break
			}
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if end, err := parseValue(d, v.Index(i)); err != nil {
				return err
			} else if !end {
				v.Index(i).Set(reflect.Zero(v.Type().Elem()))
			}
		}
	case reflect.Interface:
		var x []interface{}
		v.Set(reflect.ValueOf(&x).Elem())
		return parseList(d, v.Elem())
	default:

	}

	return nil
}

func parseDict(d *decodeState, v reflect.Value) error {
	if v.Kind() == reflect.Interface {
		var x map[string]interface{}
		v.Set(reflect.ValueOf(&x).Elem())
		return parseDict(d, v.Elem())
	}

	for {
		var key string
		if end, err := parseValue(d, reflect.ValueOf(&key).Elem()); err != nil {
			return err
		} else if !end {
			return nil
		}

		switch v.Kind() {
		case reflect.Map:
			value := reflect.New(v.Type().Elem()).Elem()
			if end, err := parseValue(d, value); err != nil {
				return newParseError(key, err)
			} else if !end {
				return fmt.Errorf("missing value for key %q", key)
			}
			if v.IsNil() {
				v.Set(reflect.MakeMap(v.Type()))
			}
			v.SetMapIndex(reflect.ValueOf(key).Convert(v.Type().Key()), value)
		case reflect.Struct:
			sf, ok := getStructFieldForKey(v.Type(), key)
			if !ok || sf.r.PkgPath != "" {
				return newError("")
			}
			value := v.FieldByIndex(sf.r.Index)
			if end, err := parseValue(d, value); err != nil {
				return newParseError(key, err)
			} else if !end {
				return newError("missing value for key %q", key)
			}
		}
	}

}

func unmarshalerDecoder(d *decodeState, v reflect.Value) error {
	if !v.Type().Implements(unmarshalerType) && v.Addr().Type().Implements(unmarshalerType) {
		v = v.Addr()
	}
	m, ok := v.Interface().(Unmarshaler)
	if !ok {
		return newError("reflect.Value.Addr of unaddressable value: %s", v.Type())
	}
	d.Reset()
	if !d.readValue() {
		return newError("a")
	}

	return m.UnmarshalBencode(d.Bytes())
}

func bigIntDecoder(d *decodeState, v reflect.Value) error {
	s := d.readInt()
	if s == "" {
		return newError("")
	}

	bi, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return newError("")
	}

	if v.Type() != bigIntType {
		v.Set(reflect.ValueOf(bi))
	} else {
		v.Set(reflect.ValueOf(*bi))
	}
	return nil
}

func (d *decodeState) readByte() byte {
	b, err := d.Scanner.ReadByte()
	if err != nil {
		panic(newSyntaxError(d.Offset, err))
	}
	d.Offset++
	return b
}
func (d *decodeState) unreadByte() {
	if err := d.Scanner.UnreadByte(); err != nil {
		panic(newSyntaxError(d.Offset, err))
	}
	d.Offset--
}
func (d *decodeState) readUntil(sep byte) {
	for {
		b := d.readByte()
		if b == sep {
			return
		}
		if err := d.WriteByte(b); err != nil {
			panic(Error(err))
		}
	}
}
func (d *decodeState) readInt() string {
	d.readUntil('e')
	if d.Len() == 0 {
		return ""
	}
	defer d.Reset()
	return bytesAsString(d.Bytes())
}

func (d *decodeState) readValue() bool {
	b := d.readByte()
	if b == 'e' {
		d.unreadByte()
		return false
	}
	if err := d.WriteByte(b); err != nil {
		panic(Error(err))
	}

	switch b {
	case 'd', 'l':
		for d.readValue() {
		}
		b = d.readByte()
		if err := d.WriteByte(b); err != nil {
			panic(Error(err))
		}
	case 'i':
		d.readUntil('e')
		if _, err := d.WriteString("e"); err != nil {
			panic(Error(err))
		}
	default:
		if b >= '0' && b <= '9' {
			start := d.Len() - 1
			d.readUntil(':')
			length, err := strconv.ParseInt(bytesAsString(d.Bytes()[start:]), 10, 64)
			if err != nil {
				panic(Error(err))
			}
			if _, err = d.WriteString(":"); err != nil {
				panic(Error(err))
			}
			length, err = io.CopyN(d, d.Scanner, length)
			d.Offset += length
			if err != nil {
				panic(Error(err))
			}
			break
		}
		panic(newUnknownValueType(d.Offset-1, b))
	}
	return true
}

func (d *decodeState) readStringLength() int64 {
	d.readUntil(':')
	length, err := strconv.ParseInt(bytesAsString(d.Bytes()), 10, 0)
	if err != nil {
		panic(Error(err))
	}
	defer d.Reset()
	return length
}

func (d *decodeState) readLength(length int64) []byte {
	b := make([]byte, length)
	n, err := io.ReadFull(d.Scanner, b)
	d.Offset += int64(n)
	if err != nil {
		panic(Error(err))
	}
	return b
}
