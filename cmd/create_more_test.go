package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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
		utils.NameInConfig:        "myproj",
		utils.UseDockerInConfig:   true,
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
