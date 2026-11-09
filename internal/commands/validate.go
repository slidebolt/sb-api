package commands

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// fieldRanges documents value ranges that the Go types cannot express.
var fieldRanges = map[string]string{
	"brightness": "0-254",
	"mireds":     "153-500",
	"rgb":        "[r,g,b] 0-255",
	"r":          "0-255",
	"g":          "0-255",
	"b":          "0-255",
	"rgbw":       "[r,g,b,w] 0-255",
	"rgbww":      "[r,g,b,cw,ww] 0-255",
	"hs":         "[hue 0-360, sat 0-100]",
	"xy":         "[x,y] 0-1",
	"transition": "seconds",
	"position":   "0-100",
	"percentage": "0-100",
	"volume":     "0-100",
}

// Rejection explains why a command was not sent and what would work.
type Rejection struct {
	Reason string
	Guide  string
}

func (r *Rejection) Error() string {
	if r.Guide == "" {
		return r.Reason
	}
	return r.Reason + "\n" + r.Guide
}

// Validate checks a command before it is published: the entity must exist and
// list the action, and a payload for a known action must decode and pass the
// action's own Validate. It returns the entity so callers can verify the result.
// With strict, payload fields the action does not define are rejected (a
// misspelled field would otherwise decode as zero, e.g. brightness 0).
func Validate(store storage.Storage, key, action string, payload json.RawMessage, strict bool) (EntityRecord, error) {
	rec, err := LoadEntity(store, key)
	if err != nil {
		return rec, &Rejection{Reason: fmt.Sprintf("not sent: %v. Find the entity with get_inventory first.", err)}
	}
	if len(rec.Commands) > 0 && !rec.HasCommand(action) {
		return rec, &Rejection{
			Reason: fmt.Sprintf("not sent: %s (%s) does not take the action %q.", key, rec.DisplayName(), action),
			Guide:  "It takes:\n" + CommandGuide(rec.Commands),
		}
	}
	if err := ValidatePayload(action, payload, strict); err != nil {
		return rec, &Rejection{
			Reason: fmt.Sprintf("not sent: invalid payload for %s: %v.", action, err),
			Guide:  "Expected: " + ActionGuide(action),
		}
	}
	return rec, nil
}

// ValidatePayload decodes the payload into the registered command type, when
// the action is known to sb-domain, and runs its Validate.
func ValidatePayload(action string, payload json.RawMessage, strict bool) error {
	typ, ok := domain.LookupCommand(action)
	if !ok {
		return nil
	}
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	v := reflect.New(typ).Interface()
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return err
	}
	if val, ok := reflect.ValueOf(v).Elem().Interface().(interface{ Validate() error }); ok {
		return val.Validate()
	}
	return nil
}

// CommandGuide renders one line per action with its payload fields.
func CommandGuide(actions []string) string {
	sorted := append([]string(nil), actions...)
	sort.Strings(sorted)
	lines := make([]string, 0, len(sorted))
	for _, a := range sorted {
		lines = append(lines, "  "+ActionGuide(a))
	}
	return strings.Join(lines, "\n")
}

// ActionGuide renders "light_set_brightness {brightness: int 0-254, transition?: seconds}".
func ActionGuide(action string) string {
	typ, ok := domain.LookupCommand(action)
	if !ok {
		return action
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ.NumField() == 0 {
		return action + " {}"
	}
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		optional := strings.Contains(tag, "omitempty") || f.Type.Kind() == reflect.Pointer
		desc := kindName(f.Type)
		if r, ok := fieldRanges[name]; ok {
			desc += " " + r
		}
		if optional {
			name += "?"
		}
		fields = append(fields, name+": "+desc)
	}
	return action + " {" + strings.Join(fields, ", ") + "}"
}

func kindName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	case reflect.Slice, reflect.Array:
		return "list"
	default:
		return "object"
	}
}
