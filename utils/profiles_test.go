package utils

import (
	"reflect"
	"sort"
	"testing"
)

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sample stack: api(backend,full)+worker(backend) on the backend side,
// web(frontend,full) on the frontend side, db has no profiles but api
// depends_on_db it, and cache has no profiles and nobody depends on it.
func sampleCompose() *CorgiCompose {
	return &CorgiCompose{
		Services: []Service{
			{
				ServiceName: "api",
				Profiles:    []string{"backend", "full"},
				DependsOnDb: []DependsOnDb{{Name: "db"}},
			},
			{
				ServiceName:       "worker",
				Profiles:          []string{"backend"},
				DependsOnServices: []DependsOnService{{Name: "api"}},
			},
			{
				ServiceName: "web",
				Profiles:    []string{"frontend", "full"},
				DependsOnDb: []DependsOnDb{{Name: "db"}},
			},
		},
		DatabaseServices: []DatabaseService{
			{ServiceName: "db"},    // no profiles, pulled in via depends_on_db
			{ServiceName: "cache"}, // no profiles, never referenced
		},
	}
}

func TestSelectByProfileEmptySelectsAll(t *testing.T) {
	corgi := sampleCompose()
	services, dbs := SelectByProfile(corgi, "")

	wantSvc := []string{"api", "web", "worker"}
	if got := keys(services); !reflect.DeepEqual(got, wantSvc) {
		t.Errorf("services = %v, want %v", got, wantSvc)
	}
	wantDb := []string{"cache", "db"}
	if got := keys(dbs); !reflect.DeepEqual(got, wantDb) {
		t.Errorf("dbs = %v, want %v", got, wantDb)
	}
}

func TestSelectByProfileMembersAndTransitiveDeps(t *testing.T) {
	corgi := sampleCompose()
	services, dbs := SelectByProfile(corgi, "backend")

	// api + worker are members; api's depends_on_db pulls in db (no profile).
	wantSvc := []string{"api", "worker"}
	if got := keys(services); !reflect.DeepEqual(got, wantSvc) {
		t.Errorf("services = %v, want %v", got, wantSvc)
	}
	wantDb := []string{"db"}
	if got := keys(dbs); !reflect.DeepEqual(got, wantDb) {
		t.Errorf("dbs = %v, want %v (db must be pulled in via depends_on_db; cache excluded)", got, wantDb)
	}
}

func TestSelectByProfileServiceDepPulledInWithoutProfileTag(t *testing.T) {
	// frontend selects web; web depends_on_db db (no profile) -> db included,
	// api/worker excluded.
	corgi := sampleCompose()
	services, dbs := SelectByProfile(corgi, "frontend")

	wantSvc := []string{"web"}
	if got := keys(services); !reflect.DeepEqual(got, wantSvc) {
		t.Errorf("services = %v, want %v", got, wantSvc)
	}
	wantDb := []string{"db"}
	if got := keys(dbs); !reflect.DeepEqual(got, wantDb) {
		t.Errorf("dbs = %v, want %v", got, wantDb)
	}
}

func TestSelectByProfileTransitiveServiceClosure(t *testing.T) {
	// A profile member whose service dep has no profile tag still pulls that
	// dep in (transitive over depends_on_services).
	corgi := &CorgiCompose{
		Services: []Service{
			{ServiceName: "front", Profiles: []string{"web"}, DependsOnServices: []DependsOnService{{Name: "gateway"}}},
			{ServiceName: "gateway", DependsOnServices: []DependsOnService{{Name: "core"}}},
			{ServiceName: "core"},
			{ServiceName: "unrelated"},
		},
	}
	services, _ := SelectByProfile(corgi, "web")
	want := []string{"core", "front", "gateway"}
	if got := keys(services); !reflect.DeepEqual(got, want) {
		t.Errorf("services = %v, want %v", got, want)
	}
}

func TestSelectByProfileUnknownIsEmpty(t *testing.T) {
	corgi := sampleCompose()
	services, dbs := SelectByProfile(corgi, "does-not-exist")
	if len(services) != 0 || len(dbs) != 0 {
		t.Errorf("unknown profile selected %v / %v, want empty (caller warns)", keys(services), keys(dbs))
	}
}

func TestSelectByProfileDbDeclaredDirectly(t *testing.T) {
	// A db_service may itself declare a profile and gets selected on its own.
	corgi := &CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "metrics", Profiles: []string{"observability"}},
			{ServiceName: "primary"},
		},
	}
	services, dbs := SelectByProfile(corgi, "observability")
	if len(services) != 0 {
		t.Errorf("services = %v, want none", keys(services))
	}
	if got := keys(dbs); !reflect.DeepEqual(got, []string{"metrics"}) {
		t.Errorf("dbs = %v, want [metrics]", got)
	}
}
