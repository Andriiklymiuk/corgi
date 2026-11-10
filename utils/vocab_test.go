package utils

import (
	"encoding/json"
	"testing"
)

func TestComposeFieldsCoverEverySchemaKey(t *testing.T) {
	var doc struct {
		Properties  map[string]json.RawMessage `json:"properties"`
		Definitions map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ComposeJSONSchema()), &doc); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, n := range ComposeFieldNames() {
		names[n] = true
	}
	want := 0
	for k := range doc.Properties {
		want++
		if !names[k] {
			t.Errorf("top-level %s missing", k)
		}
	}
	for d, def := range doc.Definitions {
		if _, ok := definitionPaths[d]; !ok {
			t.Errorf("definition %s has no path in definitionPaths", d)
		}
		for k := range def.Properties {
			want++
			if !names[k] {
				t.Errorf("%s.%s missing", d, k)
			}
		}
	}
	if len(ComposeFields()) != want {
		t.Errorf("got %d fields, schema has %d", len(ComposeFields()), want)
	}
}

func TestLookupComposeFieldNamesEveryPlace(t *testing.T) {
	got := LookupComposeField("depends_on_db")
	if len(got) != 1 || got[0].Path != "services.<name>.depends_on_db" || got[0].Type != "dependsOnDb[]" || got[0].Description == "" {
		t.Fatalf("got %+v", got)
	}
	ports := LookupComposeField("port")
	if len(ports) < 2 {
		t.Fatalf("port is a key of services and db_services: %+v", ports)
	}
	if byPath := LookupComposeField("services.<name>.port"); len(byPath) != 1 {
		t.Fatalf("a full path resolves to one place: %+v", byPath)
	}
}

func TestValidationSuggestsTheRightSpelling(t *testing.T) {
	UnknownComposeFields = []string{"helthCheck"}
	defer func() { UnknownComposeFields = nil }()
	c := &CorgiCompose{
		Services:         []Service{{ServiceName: "api", DependsOnDb: []DependsOnDb{{Name: "pg"}}}},
		DatabaseServices: []DatabaseService{{ServiceName: "pgg", Driver: "postgress"}},
	}
	errs, warns := ValidateCompose(c)
	fixes := map[string]string{}
	for _, i := range append(errs, warns...) {
		fixes[i.Code] = i.Fix
	}
	want := map[string]string{
		ErrUnknownDriver: `did you mean "postgres"?`,
		ErrDanglingDep:   `did you mean "pgg"?`,
		WarnUnknownField: `did you mean "healthCheck"?`,
	}
	for code, fix := range want {
		if fixes[code] != fix {
			t.Errorf("%s: fix %q, want %q", code, fixes[code], fix)
		}
	}
	for _, i := range append(errs, warns...) {
		if i.Fix == "" {
			t.Errorf("%s has no fix", i.Code)
		}
	}
}
