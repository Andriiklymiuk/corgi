package utils

import (
	"testing"
)

// codesOf collects the Code field from a slice of issues for easy assertion.
func codesOf(issues []ValidationIssue) []string {
	out := make([]string, len(issues))
	for i, x := range issues {
		out[i] = x.Code
	}
	return out
}

func countCode(issues []ValidationIssue, code string) int {
	n := 0
	for _, x := range issues {
		if x.Code == code {
			n++
		}
	}
	return n
}

func TestValidateCompose(t *testing.T) {
	tests := []struct {
		name      string
		compose   *CorgiCompose
		wantErr   map[string]int // code -> expected count (0 = must be absent)
		wantWarn  map[string]int
		wantClean bool // no errors and no warnings at all
	}{
		{
			name:      "nil compose",
			compose:   nil,
			wantClean: true,
		},
		{
			name: "clean minimal",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres", Port: 5432}},
				Services: []Service{
					{ServiceName: "api", Port: 3000, Start: []string{"npm start"}},
				},
			},
			wantClean: true,
		},
		{
			name: "dangling service dep",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", DependsOnServices: []DependsOnService{{Name: "ghost"}}},
				},
			},
			wantErr: map[string]int{ErrDanglingDep: 1},
		},
		{
			name: "dangling db dep",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", DependsOnDb: []DependsOnDb{{Name: "ghostdb"}}},
				},
			},
			wantErr: map[string]int{ErrDanglingDep: 1},
		},
		{
			name: "two-node cycle",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "a", DependsOnServices: []DependsOnService{{Name: "b"}}},
					{ServiceName: "b", DependsOnServices: []DependsOnService{{Name: "a"}}},
				},
			},
			wantErr: map[string]int{ErrDependencyCycle: 2},
		},
		{
			name: "self cycle",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "a", DependsOnServices: []DependsOnService{{Name: "a"}}},
				},
			},
			wantErr: map[string]int{ErrDependencyCycle: 1},
		},
		{
			name: "unknown driver",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "nosuchdb", Port: 1234}},
			},
			wantErr: map[string]int{ErrUnknownDriver: 1},
		},
		{
			name: "known image driver is valid",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "image", Port: 1234}},
			},
			wantErr: map[string]int{ErrUnknownDriver: 0},
		},
		{
			name: "port with no start and no docker runner",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", Port: 3000},
				},
			},
			wantErr: map[string]int{ErrMissingStart: 1},
		},
		{
			name: "port with docker runner is fine",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", Port: 3000, Runner: Runner{Name: "docker"}},
				},
			},
			wantErr: map[string]int{ErrMissingStart: 0},
		},
		{
			name: "port conflict service vs db",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres", Port: 8080}},
				Services: []Service{
					{ServiceName: "api", Port: 8080, Start: []string{"go run ."}},
				},
			},
			wantErr: map[string]int{ErrPortConflict: 1},
		},
		{
			name: "zero ports never conflict",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres"}},
				Services: []Service{
					{ServiceName: "api", Start: []string{"go run ."}},
					{ServiceName: "worker", Start: []string{"go run ."}},
				},
			},
			wantErr: map[string]int{ErrPortConflict: 0},
		},
		{
			name: "depended service without healthcheck warns",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", Port: 3000, Start: []string{"x"}, DependsOnServices: []DependsOnService{{Name: "core"}}},
					{ServiceName: "core", Port: 4000, Start: []string{"x"}},
				},
			},
			wantWarn: map[string]int{WarnNoHealthcheck: 1},
		},
		{
			name: "depended service with healthcheck does not warn",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", Port: 3000, Start: []string{"x"}, DependsOnServices: []DependsOnService{{Name: "core"}}},
					{ServiceName: "core", Port: 4000, Start: []string{"x"}, HealthCheck: "/health"},
				},
			},
			wantWarn: map[string]int{WarnNoHealthcheck: 0},
		},
		{
			name: "bogus depends_on condition errors",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", Start: []string{"x"}},
					{ServiceName: "web", Start: []string{"x"}, DependsOnServices: []DependsOnService{{Name: "api", Condition: "healthy"}}},
				},
			},
			wantErr: map[string]int{ErrInvalidCondition: 1},
		},
		{
			name: "bogus db depends_on condition errors",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres"}},
				Services: []Service{
					{ServiceName: "api", Start: []string{"x"}, DependsOnDb: []DependsOnDb{{Name: "db", Condition: "bogus"}}},
				},
			},
			wantErr: map[string]int{ErrInvalidCondition: 1},
		},
		{
			name: "valid conditions and empty do not error",
			compose: &CorgiCompose{
				DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres"}},
				Services: []Service{
					{ServiceName: "api", Start: []string{"x"}, HealthCheck: "/h"},
					{ServiceName: "web", Start: []string{"x"}, DependsOnServices: []DependsOnService{
						{Name: "api", Condition: "ready"},
						{Name: "api", Condition: "started"},
						{Name: "api", Condition: ""},
					}, DependsOnDb: []DependsOnDb{{Name: "db", Condition: "ready"}}},
				},
			},
			wantErr: map[string]int{ErrInvalidCondition: 0},
		},
		{
			name: "cloneFrom without branch warns",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", CloneFrom: "git@github.com:x/y.git", Start: []string{"x"}},
				},
			},
			wantWarn: map[string]int{WarnNoBranch: 1},
		},
		{
			name: "cloneFrom with branch does not warn",
			compose: &CorgiCompose{
				Services: []Service{
					{ServiceName: "api", CloneFrom: "git@github.com:x/y.git", Branch: "main", Start: []string{"x"}},
				},
			},
			wantWarn: map[string]int{WarnNoBranch: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, warns := ValidateCompose(tt.compose)

			if tt.wantClean {
				if len(errs) != 0 || len(warns) != 0 {
					t.Fatalf("want clean, got errs=%v warns=%v", codesOf(errs), codesOf(warns))
				}
				return
			}

			for code, want := range tt.wantErr {
				if got := countCode(errs, code); got != want {
					t.Errorf("error code %s: got %d, want %d (all=%v)", code, got, want, codesOf(errs))
				}
			}
			for code, want := range tt.wantWarn {
				if got := countCode(warns, code); got != want {
					t.Errorf("warn code %s: got %d, want %d (all=%v)", code, got, want, codesOf(warns))
				}
			}
		})
	}
}

func TestKnownDriversNonEmpty(t *testing.T) {
	if len(KnownDrivers) == 0 {
		t.Fatal("KnownDrivers is empty — driver options tag failed to parse")
	}
	want := map[string]bool{"postgres": false, "image": false, "supabase": false, "localstack": false}
	for _, d := range KnownDrivers {
		if d == "❌skip" {
			t.Errorf("KnownDrivers should not contain the ❌skip sentinel")
		}
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for d, found := range want {
		if !found {
			t.Errorf("expected driver %q in KnownDrivers", d)
		}
	}
}
