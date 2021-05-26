package bencode

import (
	"bytes"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"sync"
)

func Marshal(v interface{}) ([]byte, error) {
	e := newEncodeState()
	if err := e.marshal(v); err != nil {
		return nil, err
	}
	buf := append([]byte(nil), e.Bytes()...)
	encodeStatePool.Put(e)
	return buf, nil
}

type encodeState struct {
	bytes.Buffer
	scratch  [64]byte
	ptrLevel uint
	ptrSeen  map[interface{}]struct{}
}

// Marshaler is the interface implemented by types that
// can marshal themselves into valid JSON.
type Marshaler interface {
	MarshalBencode() ([]byte, error)
}

var encodeStatePool sync.Pool

func newEncodeState() *encodeState {
	if v := encodeStatePool.Get(); v != nil {
		e := v.(*encodeState)
		e.Reset()
		if len(e.ptrSeen) > 0 {
			panic("ptrEncoder.encode should have emptied ptrSeen via defers")
		}
		e.ptrLevel = 0
		return e
	}
	return &encodeState{ptrSeen: make(map[interface{}]struct{})}
}

// bencodeError is an error wrapper type for internal use only.
// Panics with errors are wrapped in bencodeError so that the top-level recover
// can distinguish intentional panics from this package.
type bencodeError struct{ error }

func (e *encodeState) marshal(v interface{}) (err error) {
	// defer func() {
	// 	if r := recover(); r != nil {
	// 		if je, ok := r.(error); ok {
	// 			err = je
	// 		} else {
	// 			panic(err)
	// 		}
	// 	}
	// }()

	return e.reflectValue(reflect.ValueOf(v))
}
func (e *encodeState) reflectValue(v reflect.Value) error {
	if !v.IsValid() {
		return fmt.Errorf("invalid Value Encoder")
	}
	return typeEncoder(v.Type())(e, v)
}

type encoderFunc func(e *encodeState, v reflect.Value) error

var encoderCache sync.Map // map[reflect.Type]encoderFunc

func typeEncoder(t reflect.Type) encoderFunc {
	if fi, ok := encoderCache.Load(t); ok {
		return fi.(encoderFunc)
	}

	var (
		wg sync.WaitGroup
		f  encoderFunc
	)
	wg.Add(1)
	fi, loaded := encoderCache.LoadOrStore(t, encoderFunc(func(e *encodeState, v reflect.Value) error {
		wg.Wait()
		return f(e, v)
	}))
	if loaded {
		return fi.(encoderFunc)
	}

	// Compute the real encoder and replace the indirect func with it.
	f = newTypeEncoder(t)
	wg.Done()
	encoderCache.Store(t, f)
	return f
}

// Wow Go is retarded.
var marshalerType = reflect.TypeOf(func() *Marshaler {
	var m Marshaler
	return &m
}()).Elem()

var bigIntType = reflect.TypeOf(big.Int{})

// newTypeEncoder constructs an encoderFunc for a type.
// The returned encoder only checks CanAddr when allowAddr is true.
func newTypeEncoder(t reflect.Type) encoderFunc {
	if t.Implements(marshalerType) || (t.Kind() != reflect.Ptr && reflect.PtrTo(t).Implements(marshalerType)) {
		return marshalerEncoder
	}
	if t == bigIntType {
		return bigIntEncoder
	}

	switch t.Kind() {
	case reflect.Bool:
		return boolEncoder
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intEncoder
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return uintEncoder
	case reflect.String:
		return stringEncoder
	case reflect.Interface:
		return interfaceEncoder
	case reflect.Struct:
		return newStructEncoder
	case reflect.Map:
		return newMapEncoder
	case reflect.Slice:
		return newSliceEncoder
	case reflect.Array:
		return newArrayEncoder
	case reflect.Ptr:
		return newPtrEncoder
	default:
		return unsupportedTypeEncoder
	}
}
func marshalerEncoder(e *encodeState, v reflect.Value) error {
	if v.Kind() != reflect.Ptr && v.CanAddr() && v.Addr().IsNil() {
		v = v.Addr()
	}
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return fmt.Errorf("ptr is nil")
	}

	m, ok := v.Interface().(Marshaler)
	if !ok {
		return fmt.Errorf("reflect.Value.Addr of unaddressable value: %s", v.Type())
	}
	b, err := m.MarshalBencode()
	if err != nil {
		return err
	}
	_, err = e.Write(b)
	return err
}

func bigIntEncoder(e *encodeState, v reflect.Value) error {
	if _, err := e.WriteString("i"); err != nil {
		return err
	}
	bi := v.Interface().(big.Int)
	if _, err := e.WriteString(bi.String()); err != nil {
		return err
	}
	if _, err := e.WriteString("e"); err != nil {
		return err
	}
	return nil
}
func boolEncoder(e *encodeState, v reflect.Value) (err error) {
	if v.Bool() {
		_, err = e.WriteString("i1e")
	} else {
		_, err = e.WriteString("i0e")
	}
	return
}
func intEncoder(e *encodeState, v reflect.Value) error {
	if _, err := e.WriteString("i"); err != nil {
		return err
	}
	if _, err := e.Write(strconv.AppendInt(e.scratch[:0], v.Int(), 10)); err != nil {
		return err
	}
	if _, err := e.WriteString("e"); err != nil {
		return err
	}
	return nil
}
func uintEncoder(e *encodeState, v reflect.Value) error {
	if _, err := e.WriteString("i"); err != nil {
		return err
	}
	if _, err := e.Write(strconv.AppendUint(e.scratch[:0], v.Uint(), 10)); err != nil {
		return err
	}
	if _, err := e.WriteString("e"); err != nil {
		return err
	}
	return nil
}
func stringEncoder(e *encodeState, v reflect.Value) error {
	if _, err := e.Write(strconv.AppendInt(e.scratch[:0], int64(len(v.String())), 10)); err != nil {
		return err
	}
	if _, err := e.WriteString(":" + v.String()); err != nil {
		return err
	}
	return nil
}
func interfaceEncoder(e *encodeState, v reflect.Value) error {
	return e.reflectValue(v.Elem())
}
func newStructEncoder(e *encodeState, v reflect.Value) error {
	if _, err := e.WriteString("d"); err != nil {
		return err
	}
	for _, ef := range cachedTypeFields(v.Type()) {
		fieldValue := v.Field(ef.i)
		if ef.omitEmpty && isEmptyValue(fieldValue) {
			continue
		}
		if _, err := e.Write(strconv.AppendInt(e.scratch[:0], int64(len(ef.tag)), 10)); err != nil {
			return err
		}
		if _, err := e.WriteString(":" + ef.tag); err != nil {
			return err
		}
		if err := e.reflectValue(fieldValue); err != nil {
			return err
		}
	}
	if _, err := e.WriteString("e"); err != nil {
		return err
	}

	return nil
}
func newMapEncoder(e *encodeState, v reflect.Value) error {
	if v.Type().Key().Kind() != reflect.String {
		return unsupportedTypeEncoder(e, v)
	}
	if v.IsNil() {
		_, err := e.WriteString("de")
		return err
	}
	if _, err := e.WriteString("d"); err != nil {
		return err
	}
	sv := stringValues(v.MapKeys())
	sort.Sort(sv)
	for _, key := range sv {
		if err := stringEncoder(e, key); err != nil {
			return err
		}
		if err := e.reflectValue(v.MapIndex(key)); err != nil {
			return err
		}
	}
	if _, err := e.WriteString("e"); err != nil {
		return err
	}
	return nil
}

func newSliceEncoder(e *encodeState, v reflect.Value) error {
	if v.Type().Elem().Kind() == reflect.Uint8 {
		s := v.Bytes()
		_, err := e.Write(strconv.AppendInt(e.scratch[:0], int64(len(s)), 10))
		if err != nil {
			return err
		}
		if _, err = e.WriteString(":"); err != nil {
			return err
		}
		_, err = e.Write(s)
		return err
	}
	if v.IsNil() {
		_, err := e.WriteString("le")
		return err
	}

	return newArrayEncoder(e, v)
}
func newArrayEncoder(e *encodeState, v reflect.Value) error {
	if _, err := e.WriteString("l"); err != nil {
		return err
	}
	for i, j := 0, v.Len(); i < j; i++ {
		if err := e.reflectValue(v.Index(i)); err != nil {
			return err
		}
	}

	if _, err := e.WriteString("e"); err != nil {
		return err
	}
	return nil
}
func newPtrEncoder(e *encodeState, v reflect.Value) error {
	if v.IsNil() {
		v = reflect.Zero(v.Type().Elem())
	} else {
		v = v.Elem()
	}
	return e.reflectValue(v)
}
func unsupportedTypeEncoder(_ *encodeState, v reflect.Value) error {
	return fmt.Errorf("unsupported type: %s", v.Type())
}

// error aborts the encoding by panicking with err wrapped in jsonError.
func (e *encodeState) error(err error) {
	panic(bencodeError{err})
}

type stringValues []reflect.Value

func (sv stringValues) Len() int           { return len(sv) }
func (sv stringValues) Swap(i, j int)      { sv[i], sv[j] = sv[j], sv[i] }
func (sv stringValues) Less(i, j int) bool { return sv.get(i) < sv.get(j) }
func (sv stringValues) get(i int) string   { return sv[i].String() }
