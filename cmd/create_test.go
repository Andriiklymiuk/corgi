package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

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
		Name:        "proj",
		Description: "demo",
		UseDocker:   true,
		Init:        []string{"setup"},
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
