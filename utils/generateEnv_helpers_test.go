package utils

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveCopyEnvPath(t *testing.T) {
	t.Run("explicit path wins", func(t *testing.T) {
		got := resolveCopyEnvPath(Service{CopyEnvFromFilePath: "service.env"}, "override.env")
		if got != "override.env" {
			t.Errorf("got %q, want override.env", got)
		}
	})
	t.Run("falls back to service config", func(t *testing.T) {
		got := resolveCopyEnvPath(Service{CopyEnvFromFilePath: "service.env"}, "")
		if got != "service.env" {
			t.Errorf("got %q, want service.env", got)
		}
	})
	t.Run("both empty", func(t *testing.T) {
		got := resolveCopyEnvPath(Service{}, "")
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestBuildServiceExports(t *testing.T) {
	t.Run("inline KEY=value", func(t *testing.T) {
		got := buildServiceExports(
			Service{Exports: []string{"FOO=bar"}},
			map[string]string{},
		)
		if got["FOO"] != "bar" {
			t.Errorf("got %v, want FOO=bar", got)
		}
	})
	t.Run("bare export pulls from env", func(t *testing.T) {
		got := buildServiceExports(
			Service{Exports: []string{"PORT"}},
			map[string]string{"PORT": "8080"},
		)
		if got["PORT"] != "8080" {
			t.Errorf("got %v, want PORT=8080", got)
		}
	})
	t.Run("bare export missing var dropped", func(t *testing.T) {
		got := buildServiceExports(
			Service{Exports: []string{"GHOST"}},
			map[string]string{"OTHER": "x"},
		)
		if _, ok := got["GHOST"]; ok {
			t.Errorf("missing var should be dropped, got %v", got)
		}
	})
	t.Run("inline expands ${VAR}", func(t *testing.T) {
		got := buildServiceExports(
			Service{Exports: []string{"URL=http://${HOST}:${PORT}"}},
			map[string]string{"HOST": "localhost", "PORT": "3000"},
		)
		if got["URL"] != "http://localhost:3000" {
			t.Errorf("got %q, want http://localhost:3000", got["URL"])
		}
	})
}

func TestFindStuckExports(t *testing.T) {
	t.Run("no stuck", func(t *testing.T) {
		out := ExportsMap{
			"a": {"X": "ready"},
			"b": {"Y": "fine"},
		}
		if got := findStuckExports(out); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
	t.Run("detects unresolved cross ref", func(t *testing.T) {
		out := ExportsMap{
			"a": {"X": "${b.Y}"},
		}
		got := findStuckExports(out)
		if len(got) != 1 || !strings.Contains(got[0], "a.X") {
			t.Errorf("got %v, want one entry mentioning a.X", got)
		}
	})
}

func TestCollectProducers(t *testing.T) {
	s := Service{
		Environment: []string{
			"DB_URL=${db.URL}",
			"API=${api.HOST}:${api.PORT}",
			"PLAIN=value",
		},
	}
	got := collectProducers(s)
	want := map[string]bool{"db": true, "api": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestValidateSubscriptions(t *testing.T) {
	topics := map[string]bool{"orders": true}
	queues := map[string]bool{"q1": true}

	t.Run("valid sub", func(t *testing.T) {
		err := validateSubscriptions("svc",
			[]SnsSubscription{{Topic: "orders", Queue: "q1"}},
			topics, queues,
		)
		if err != nil {
			t.Errorf("unexpected err: %v", err)
		}
	})
	t.Run("missing topic field", func(t *testing.T) {
		err := validateSubscriptions("svc",
			[]SnsSubscription{{Topic: "", Queue: "q1"}},
			topics, queues,
		)
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("want required err, got %v", err)
		}
	})
	t.Run("undeclared topic", func(t *testing.T) {
		err := validateSubscriptions("svc",
			[]SnsSubscription{{Topic: "ghost", Queue: "q1"}},
			topics, queues,
		)
		if err == nil || !strings.Contains(err.Error(), "not declared in topics") {
			t.Errorf("want topic err, got %v", err)
		}
	})
	t.Run("undeclared queue", func(t *testing.T) {
		err := validateSubscriptions("svc",
			[]SnsSubscription{{Topic: "orders", Queue: "ghost"}},
			topics, queues,
		)
		if err == nil || !strings.Contains(err.Error(), "not declared in queues") {
			t.Errorf("want queue err, got %v", err)
		}
	})
}

func TestValidateLocalstackParameters(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		err := validateLocalstackParameters("svc", []SsmParameter{{Type: "String"}})
		if err == nil || !strings.Contains(err.Error(), "name required") {
			t.Errorf("want name err, got %v", err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		err := validateLocalstackParameters("svc", []SsmParameter{{Name: "p", Type: "Bogus"}})
		if err == nil || !strings.Contains(err.Error(), "Bogus") {
			t.Errorf("want type err, got %v", err)
		}
	})
	t.Run("valid types accepted", func(t *testing.T) {
		for _, typ := range []string{"", "String", "StringList", "SecureString"} {
			err := validateLocalstackParameters("svc", []SsmParameter{{Name: "p", Type: typ}})
			if err != nil {
				t.Errorf("type %q rejected: %v", typ, err)
			}
		}
	})
}
