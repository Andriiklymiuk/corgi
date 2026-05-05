package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func withStdin(t *testing.T, input string) {
	t.Helper()

	prev := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = prev
		_ = r.Close()
	})
}

func TestCopyDatabaseService(t *testing.T) {
	orig := &utils.DatabaseService{ServiceName: "x", Driver: "postgres", Port: 5432}
	got := copyDatabaseService(orig)
	if got == orig {
		t.Error("expected new pointer")
	}
	if got.ServiceName != "x" || got.Driver != "postgres" || got.Port != 5432 {
		t.Errorf("got %+v", got)
	}
	got.ServiceName = "y"
	if orig.ServiceName != "x" {
		t.Error("modifying copy should not affect original")
	}
}

func TestCopyService(t *testing.T) {
	orig := &utils.Service{ServiceName: "api", Port: 3000}
	got := copyService(orig)
	if got == orig {
		t.Error("expected new pointer")
	}
	if got.ServiceName != "api" {
		t.Errorf("got %+v", got)
	}
}

func TestCopyRequired(t *testing.T) {
	orig := &utils.Required{Name: "node", Install: []string{"brew install node"}}
	got := copyRequired(orig)
	if got == orig {
		t.Error("expected new pointer")
	}
	if got.Name != "node" {
		t.Errorf("got %+v", got)
	}
}

func TestLowercaseFirstLetter(t *testing.T) {
	tests := map[string]string{
		"":            "",
		"Init":        "init",
		"BeforeStart": "beforeStart",
	}
	for in, want := range tests {
		if got := lowercaseFirstLetter(in); got != want {
			t.Errorf("lowercaseFirstLetter(%q) = %q want %q", in, got, want)
		}
	}
}

func TestAddDbServicesToMapNil(t *testing.T) {
	m := map[string]interface{}{}
	addDbServicesToMap(&utils.CorgiCompose{}, m)
	if _, ok := m[utils.DbServicesInConfig]; ok {
		t.Error("expected no entry for nil DatabaseServices")
	}
}

func TestAddDbServicesToMap(t *testing.T) {
	m := map[string]interface{}{}
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db1", Driver: "postgres", Port: 5432},
		},
	}
	addDbServicesToMap(corgi, m)
	dbs := m[utils.DbServicesInConfig].(map[string]*utils.DatabaseService)
	if len(dbs) != 1 || dbs["db1"] == nil {
		t.Errorf("got %v", dbs)
	}
	if dbs["db1"].ServiceName != "" {
		t.Errorf("ServiceName should be cleared, got %q", dbs["db1"].ServiceName)
	}
}

func TestAddServicesToMap(t *testing.T) {
	m := map[string]interface{}{}
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{{ServiceName: "api", Port: 3000}},
	}
	addServicesToMap(corgi, m)
	svcs := m[utils.ServicesInConfig].(map[string]*utils.Service)
	if len(svcs) != 1 || svcs["api"] == nil {
		t.Errorf("got %v", svcs)
	}
	if svcs["api"].ServiceName != "" {
		t.Error("ServiceName should be cleared")
	}
}

func TestAddRequiredToMap(t *testing.T) {
	m := map[string]interface{}{}
	corgi := &utils.CorgiCompose{
		Required: []utils.Required{{Name: "node"}},
	}
	addRequiredToMap(corgi, m)
	r := m[utils.RequiredInConfig].(map[string]*utils.Required)
	if len(r) != 1 || r["node"].Name != "" {
		t.Errorf("got %v", r)
	}
}

func TestAddLifecycleToMapNoneSet(t *testing.T) {
	m := map[string]interface{}{}
	addLifecycleToMap(&utils.CorgiCompose{}, m)
	if len(m) != 0 {
		t.Errorf("expected empty, got %v", m)
	}
}

func TestAddLifecycleToMapSetsAll(t *testing.T) {
	m := map[string]interface{}{}
	addLifecycleToMap(&utils.CorgiCompose{
		Init:        []string{"x"},
		Start:       []string{"y"},
		BeforeStart: []string{"a"},
		AfterStart:  []string{"b"},
	}, m)
	for _, k := range []string{utils.InitInConfig, utils.StartInConfig, utils.BeforeStartInConfig, utils.AfterStartInConfig} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
}

func TestAddFlagsToMap(t *testing.T) {
	m := map[string]interface{}{}
	addFlagsToMap(&utils.CorgiCompose{}, m)
	if len(m) != 0 {
		t.Errorf("expected empty, got %v", m)
	}
	addFlagsToMap(&utils.CorgiCompose{UseDocker: true, UseAwsVpn: true}, m)
	if !m[utils.UseDockerInConfig].(bool) || !m[utils.UseAwsVpnInConfig].(bool) {
		t.Errorf("flags not set: %v", m)
	}
}

func TestGetCorgiServicesMapFull(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Name:             "proj",
		Description:      "demo",
		UseDocker:        true,
		Init:             []string{"setup"},
		DatabaseServices: []utils.DatabaseService{{ServiceName: "db", Driver: "postgres"}},
		Services:         []utils.Service{{ServiceName: "api"}},
		Required:         []utils.Required{{Name: "node"}},
	}
	m := GetCorgiServicesMap(corgi)
	if m[utils.NameInConfig] != "proj" {
		t.Errorf("name = %v", m[utils.NameInConfig])
	}
	if m[utils.DescriptionInConfig] != "demo" {
		t.Errorf("desc = %v", m[utils.DescriptionInConfig])
	}
	if !m[utils.UseDockerInConfig].(bool) {
		t.Error("UseDocker missing")
	}
}

func TestFormatPrompt(t *testing.T) {
	tests := []struct {
		yamlTag, name, want string
	}{
		{"foo,omitempty", "Foo", "Enter foo:"},
		{"bar", "Bar", "Enter bar:"},
		{"", "BazQux", "Enter bazqux:"},
	}
	for _, tt := range tests {
		if got := formatPrompt(tt.yamlTag, tt.name); got != tt.want {
			t.Errorf("got %q want %q", got, tt.want)
		}
	}
}

func TestParseMapEntryString(t *testing.T) {
	k, v, ok := parseMapEntry("foo=bar", reflect.String)
	if !ok || k.String() != "foo" || v.String() != "bar" {
		t.Errorf("got %v %v %v", k, v, ok)
	}
}

func TestParseMapEntryInterface(t *testing.T) {
	_, _, ok := parseMapEntry("foo=bar", reflect.Interface)
	if !ok {
		t.Error("expected ok")
	}
}

func TestParseMapEntryNoEquals(t *testing.T) {
	_, _, ok := parseMapEntry("nokey", reflect.String)
	if ok {
		t.Error("expected false")
	}
}

func TestParseMapEntryUnsupportedKind(t *testing.T) {
	_, _, ok := parseMapEntry("foo=bar", reflect.Int)
	if ok {
		t.Error("expected false")
	}
}

func TestParseMapEntryEmptyKey(t *testing.T) {
	_, _, ok := parseMapEntry("=value", reflect.String)
	if ok {
		t.Error("expected false")
	}
}

func TestAddServicesToMapNil(t *testing.T) {
	m := map[string]interface{}{}
	addServicesToMap(&utils.CorgiCompose{}, m)
	if _, ok := m[utils.ServicesInConfig]; ok {
		t.Error("expected no entry for nil Services")
	}
}

func TestAddRequiredToMapNil(t *testing.T) {
	m := map[string]interface{}{}
	addRequiredToMap(&utils.CorgiCompose{}, m)
	if _, ok := m[utils.RequiredInConfig]; ok {
		t.Error("expected no entry for nil Required")
	}
}

func TestPromptMapFieldParsesValidEntries(t *testing.T) {
	type holder struct {
		Labels map[string]string `yaml:"labels"`
	}

	withStdin(t, "foo=bar\ninvalid\n baz = qux \n\n")

	target := &holder{}
	v := reflect.ValueOf(target).Elem()
	field, _ := v.Type().FieldByName("Labels")
	promptMapField(v.FieldByName("Labels"), field, "Enter labels:")

	if len(target.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %+v", target.Labels)
	}
	if target.Labels["foo"] != "bar" || target.Labels["baz"] != "qux" {
		t.Fatalf("unexpected labels: %+v", target.Labels)
	}
}

func TestPromptMapFieldLeavesEmptyTargetUnset(t *testing.T) {
	type holder struct {
		Labels map[string]string `yaml:"labels"`
	}

	withStdin(t, "\n")

	target := &holder{}
	v := reflect.ValueOf(target).Elem()
	field, _ := v.Type().FieldByName("Labels")
	promptMapField(v.FieldByName("Labels"), field, "Enter labels:")

	if target.Labels != nil {
		t.Fatalf("expected nil map, got %+v", target.Labels)
	}
}

func TestReadStringSliceStopsOnBlankLine(t *testing.T) {
	withStdin(t, "first\nsecond\n\n")

	got := readStringSlice("Enter commands")
	if !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("unexpected slice: %+v", got)
	}
}

func TestPromptSliceFieldSetsStringSlice(t *testing.T) {
	type holder struct {
		Start []string `yaml:"start"`
	}

	withStdin(t, "make build\nmake test\n\n")

	target := &holder{}
	v := reflect.ValueOf(target).Elem()
	field, _ := v.Type().FieldByName("Start")
	promptSliceField(v.FieldByName("Start"), field, "Enter start")

	if !slices.Equal(target.Start, []string{"make build", "make test"}) {
		t.Fatalf("unexpected start slice: %+v", target.Start)
	}
}

func TestSetUserInputToFieldRequiredRetries(t *testing.T) {
	withStdin(t, "\nservice-api\n")

	var got string
	setUserInputToField(reflect.ValueOf(&got).Elem(), "Enter name:", true)

	if got != "service-api" {
		t.Fatalf("got %q", got)
	}
}

func TestSetUserInputToFieldNormalizesOptionalString(t *testing.T) {
	withStdin(t, "a b c\n")

	var got string
	setUserInputToField(reflect.ValueOf(&got).Elem(), "Enter name:", false)

	if got != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestSetUserInputToFieldParsesInt(t *testing.T) {
	withStdin(t, "42\n")

	var got int
	setUserInputToField(reflect.ValueOf(&got).Elem(), "Enter port:", false)

	if got != 42 {
		t.Fatalf("got %d", got)
	}
}

func TestHandleCommandCreationAppendsExistingCommands(t *testing.T) {
	withStdin(t, "make lint\nmake test\n\n")

	corgiMap := map[string]interface{}{
		utils.StartInConfig: []string{"make build"},
	}
	handleCommandCreation(corgiMap, utils.StartInConfig)

	got, _ := corgiMap[utils.StartInConfig].([]string)
	if !slices.Equal(got, []string{"make build", "make lint", "make test"}) {
		t.Fatalf("unexpected commands: %+v", got)
	}
}

func TestEncodeSection(t *testing.T) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	encodeSection(enc, map[string]interface{}{"key": "value"}, "key", "label")
	enc.Close()
	if !strings.Contains(buf.String(), "value") {
		t.Errorf("got %q", buf.String())
	}
}

func TestEncodeSectionMissing(t *testing.T) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	encodeSection(enc, map[string]interface{}{}, "missing", "label")
	enc.Close()
	if buf.Len() != 0 {
		t.Errorf("expected empty, got %q", buf.String())
	}
}

func TestEncodeScalarSections(t *testing.T) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	encodeScalarSections(enc, map[string]interface{}{
		utils.NameInConfig:      "myproj",
		utils.UseDockerInConfig: true,
	})
	enc.Close()
	got := buf.String()
	if !strings.Contains(got, "myproj") {
		t.Errorf("missing name: %q", got)
	}
	if !strings.Contains(got, "useDocker") {
		t.Errorf("missing useDocker: %q", got)
	}
}

func TestEncodeScalarSectionsEmptySliceSkipped(t *testing.T) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	encodeScalarSections(enc, map[string]interface{}{
		utils.InitInConfig: []string{},
	})
	enc.Close()
	if strings.Contains(buf.String(), "init") {
		t.Errorf("expected empty slice skipped: %q", buf.String())
	}
}

func TestRemoveSeparators(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.yml")
	body := "key: value\n---\nfoo: bar\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeSeparators(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if strings.Contains(string(got), "---") {
		t.Errorf("--- not removed: %q", got)
	}
}

func TestRemoveSeparatorsMissing(t *testing.T) {
	if err := removeSeparators("/nonexistent/zzz.yml"); err == nil {
		t.Error("expected err")
	}
}

func TestUpdateCorgiComposeFileWithMap(t *testing.T) {
	prev := utils.CorgiComposePath
	dir := t.TempDir()
	utils.CorgiComposePath = filepath.Join(dir, "out.yml")
	t.Cleanup(func() { utils.CorgiComposePath = prev })

	UpdateCorgiComposeFileWithMap(map[string]interface{}{
		utils.NameInConfig: "myproj",
	})
	body, err := os.ReadFile(utils.CorgiComposePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "myproj") {
		t.Errorf("got %q", body)
	}
}

func TestUpdateCorgiComposeFileWithMapDefaultName(t *testing.T) {
	prevPath := utils.CorgiComposePath
	utils.CorgiComposePath = ""
	t.Cleanup(func() { utils.CorgiComposePath = prevPath })

	cwd, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	UpdateCorgiComposeFileWithMap(map[string]interface{}{
		utils.NameInConfig: "default-test",
	})
	body, err := os.ReadFile(utils.CorgiComposeDefaultName)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "default-test") {
		t.Errorf("got %q", body)
	}
}

func TestHandleServiceCreationDb(t *testing.T) {
	corgiMap := map[string]interface{}{}
	svc := &utils.DatabaseService{ServiceName: "newdb"}
	handleServiceCreation(corgiMap, utils.DbServicesInConfig, svc, "ServiceName")
	got := corgiMap[utils.DbServicesInConfig].(map[string]*utils.DatabaseService)
	if got["newdb"] == nil {
		t.Errorf("got %+v", got)
	}
}

func TestHandleServiceCreationService(t *testing.T) {
	corgiMap := map[string]interface{}{}
	svc := &utils.Service{ServiceName: "api"}
	handleServiceCreation(corgiMap, utils.ServicesInConfig, svc, "ServiceName")
	got := corgiMap[utils.ServicesInConfig].(map[string]*utils.Service)
	if got["api"] == nil {
		t.Errorf("got %+v", got)
	}
}

func TestHandleServiceCreationRequired(t *testing.T) {
	corgiMap := map[string]interface{}{}
	r := &utils.Required{Name: "node"}
	handleServiceCreation(corgiMap, utils.RequiredInConfig, r, "Name")
	got := corgiMap[utils.RequiredInConfig].(map[string]*utils.Required)
	if got["node"] == nil {
		t.Errorf("got %+v", got)
	}
}
