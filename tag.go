package bencode

import (
	"reflect"
	"sort"
	"strings"
	"sync"
)

type encodeStructField struct {
	i         int
	tag       string
	omitEmpty bool
}

type encodeFieldsSortType []encodeStructField

func (ef encodeFieldsSortType) Len() int           { return len(ef) }
func (ef encodeFieldsSortType) Swap(i, j int)      { ef[i], ef[j] = ef[j], ef[i] }
func (ef encodeFieldsSortType) Less(i, j int) bool { return ef[i].tag < ef[j].tag }

var encodeFieldCache sync.Map

func cachedTypeFields(t reflect.Type) []encodeStructField {
	if f, ok := encodeFieldCache.Load(t); ok {
		return f.([]encodeStructField)
	}
	f, _ := encodeFieldCache.LoadOrStore(t, encodeFields(t))
	return f.([]encodeStructField)
}

func encodeFields(t reflect.Type) []encodeStructField {
	var current []encodeStructField

	for i, n := 0, t.NumField(); i < n; i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Anonymous {
			continue
		}

		tv := getTag(f.Tag)
		if tv.Ignore() {
			continue
		}
		ef := encodeStructField{
			i:         i,
			tag:       f.Name,
			omitEmpty: tv.OmitEmpty(),
		}
		if tv.Key() != "" {
			ef.tag = tv.Key()
		}

		current = append(current, ef)
	}
	fss := encodeFieldsSortType(current)
	sort.Sort(fss)
	return fss
}

func getTag(st reflect.StructTag) tag {
	return parseTag(st.Get("bencode"))
}

type tag []string

func parseTag(tagStr string) tag {
	return strings.Split(tagStr, ",")
}

func (t tag) Ignore() bool {
	return t[0] == "-"
}

func (t tag) Key() string {
	return t[0]
}

func (t tag) HasOpt(opt string) bool {
	for _, s := range t[1:] {
		if s == opt {
			return true
		}
	}
	return false
}

func (t tag) OmitEmpty() bool {
	return t.HasOpt("omitempty")
}

func (t tag) IgnoreUnmarshalTypeError() bool {
	return t.HasOpt("ignore_unmarshal_type_error")
}

type structField struct {
	r   reflect.StructField
	tag tag
}

var decodeFieldCache sync.Map

func getStructFieldForKey(t reflect.Type, key string) (structField, bool) {
	v, ok := decodeFieldCache.Load(t)
	if !ok {
		v, _ = decodeFieldCache.LoadOrStore(t, decodeFields(t))
	}

	sf, h := v.(map[string]structField)[key]
	return sf, h
}

func decodeFields(t reflect.Type) map[string]structField {
	m := make(map[string]structField)

	for i, n := 0, t.NumField(); i < n; i++ {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		tagStr := f.Tag.Get("bencode")
		if tagStr == "-" {
			continue
		}
		tags := parseTag(tagStr)
		key := tags.Key()
		if key == "" {
			key = f.Name
		}

		m[key] = structField{f, tags}
	}
	return m
}
