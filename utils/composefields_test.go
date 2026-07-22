package utils

import (
	"reflect"
	"testing"
	"time"
)

// Fields corgi computes rather than reads from the compose file.
var computedServiceFields = map[string]bool{
	"ServiceName":  true,
	"AbsolutePath": true,
	"Path":         true,
	"CacheScope":   true,
}

var computedRequiredFields = map[string]bool{
	"Name": true,
}

func setRecognisableValues(t *testing.T, v reflect.Value) {
	t.Helper()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString("set")
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int64:
			if f.Type() == reflect.TypeOf(time.Duration(0)) {
				f.SetInt(int64(3 * time.Second))
				continue
			}
			f.SetInt(7)
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		case reflect.Map:
			f.Set(reflect.MakeMap(f.Type()))
		case reflect.Ptr:
			f.Set(reflect.New(f.Type().Elem()))
		case reflect.Struct:
			setRecognisableValues(t, f)
		}
	}
}

func assertNoFieldDropped(t *testing.T, in, out reflect.Value, computed map[string]bool, builder string) {
	t.Helper()
	var checked int
	for i := 0; i < in.NumField(); i++ {
		field := in.Type().Field(i)
		// CanSet is false here: reflect.ValueOf on a non-pointer is not
		// addressable. Exportedness is the question, and checking CanSet
		// instead made this loop skip every field and pass vacuously.
		if computed[field.Name] || !field.IsExported() || in.Field(i).IsZero() {
			continue
		}
		checked++
		if out.Field(i).IsZero() {
			t.Errorf("%s dropped %s: it parses from the compose file and never reaches the code that uses it",
				builder, field.Name)
		}
	}
	if checked == 0 {
		t.Fatalf("%s: compared no fields, so this proves nothing", builder)
	}
}

// skipInCi and warmup both shipped doing nothing because a builder copied
// fields one at a time. This walks the struct so the next one cannot.
func TestBuildServiceKeepsEveryComposeField(t *testing.T) {
	var parsed Service
	setRecognisableValues(t, reflect.ValueOf(&parsed).Elem())
	parsed.Path = "./web"
	parsed.CloneFrom = ""

	built := buildService("web", parsed)

	assertNoFieldDropped(t,
		reflect.ValueOf(parsed), reflect.ValueOf(built),
		computedServiceFields, "buildService")
}

func TestParseRequiredKeepsEveryComposeField(t *testing.T) {
	var parsed Required
	setRecognisableValues(t, reflect.ValueOf(&parsed).Elem())

	out := parseRequired(map[string]Required{"docker": parsed}, false)
	if len(out) != 1 {
		t.Fatalf("expected one tool, got %d", len(out))
	}

	assertNoFieldDropped(t,
		reflect.ValueOf(parsed), reflect.ValueOf(out[0]),
		computedRequiredFields, "parseRequired")
}
