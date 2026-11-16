package strictfile

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

var (
	// rawMessage is left to whoever decodes it later, with its own type.
	rawMessage      = reflect.TypeFor[json.RawMessage]()
	jsonUnmarshaler = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// unknownFieldPrefix starts encoding/json's error for a field the target
// does not declare; only that error is explained. Should its wording
// change, the error is kept as it is.
const unknownFieldPrefix = "json: unknown field "

// explainDecodeError adds to an unknown-field error where the field is and
// which fields v's type allows there, which encoding/json does not say. Any
// other error, or one that cannot be placed for certain, is kept as it is.
func explainDecodeError(err error, data []byte, v any) error {
	key, isUnknown := unknownKey(err)
	var doc any
	if !isUnknown || json.NewDecoder(bytes.NewReader(data)).Decode(&doc) != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if msg, ok := (finder{key: key}).in(doc, reflect.TypeOf(v), "$"); ok {
		return fmt.Errorf("parse: %s", msg)
	}
	return fmt.Errorf("parse: %w", err)
}

// unknownKey reads the key out of encoding/json's unknown-field error.
func unknownKey(err error) (string, bool) {
	quoted, ok := strings.CutPrefix(err.Error(), unknownFieldPrefix)
	if !ok {
		return "", false
	}
	key, unquoteErr := strconv.Unquote(quoted)
	return key, unquoteErr == nil
}

// finder looks for the key encoding/json rejected, walking the document
// alongside the type it was decoded into.
type finder struct {
	key string
}

func (f finder) in(doc any, t reflect.Type, at string) (string, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if decodesItself(t) {
		return "", false
	}
	switch t.Kind() {
	case reflect.Struct:
		return f.inObject(doc, t, at)
	case reflect.Slice:
		return f.inList(doc, t.Elem(), -1, at)
	case reflect.Array:
		return f.inList(doc, t.Elem(), t.Len(), at)
	case reflect.Map:
		return f.inMap(doc, t.Elem(), at)
	}
	return "", false
}

// decodesItself reports a type encoding/json hands the raw value to, which
// may accept keys its fields do not name.
func decodesItself(t reflect.Type) bool {
	pointer := reflect.PointerTo(t)
	return t == rawMessage || pointer.Implements(jsonUnmarshaler) || pointer.Implements(textUnmarshaler)
}

// inList walks a list's items; for a Go array, only the first limit, since
// encoding/json drops the rest unread. A negative limit walks them all.
func (f finder) inList(doc any, elem reflect.Type, limit int, at string) (string, bool) {
	items, _ := doc.([]any)
	if limit >= 0 && len(items) > limit {
		items = items[:limit]
	}
	for i, item := range items {
		if msg, ok := f.in(item, elem, fmt.Sprintf("%s[%d]", at, i)); ok {
			return msg, true
		}
	}
	return "", false
}

// inMap walks a map's values. A map key comes from the file, so it is
// quoted in the path and cannot carry control characters into the message.
func (f finder) inMap(doc any, elem reflect.Type, at string) (string, bool) {
	entries, _ := doc.(map[string]any)
	for _, key := range sortedKeys(entries) {
		if msg, ok := f.in(entries[key], elem, fmt.Sprintf("%s[%q]", at, key)); ok {
			return msg, true
		}
	}
	return "", false
}

// inObject checks an object's keys against the struct's JSON fields,
// folding case as encoding/json does. The path names fields as declared, so
// it never repeats a key from the file.
func (f finder) inObject(doc any, t reflect.Type, at string) (string, bool) {
	object, ok := doc.(map[string]any)
	fields, names, modelled := structFields(t)
	if !ok || !modelled {
		return "", false
	}
	for _, key := range sortedKeys(object) {
		field, known := fields[foldKey(key)]
		if !known && key == f.key {
			return fmt.Sprintf("unknown field %q in %s; allowed fields there: %s", key, at, strings.Join(names, ", ")), true
		}
		if !known {
			continue
		}
		if msg, ok := f.in(object[key], field.typ, at+"."+field.name); ok {
			return msg, true
		}
	}
	return "", false
}

// jsonField is a field as encoding/json names it.
type jsonField struct {
	name string
	typ  reflect.Type
}

// structFields lists the JSON names of t's fields in declaration order. A
// struct with embedded fields is not modelled: encoding/json's promotion
// and shadowing rules decide which of them a key reaches, and a guess could
// name the wrong field.
func structFields(t reflect.Type) (map[string]jsonField, []string, bool) {
	fields := map[string]jsonField{}
	var names []string
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			return nil, nil, false
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[foldKey(name)] = jsonField{name: name, typ: f.Type}
		names = append(names, name)
	}
	return fields, names, true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
