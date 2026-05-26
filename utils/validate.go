package utils

import (
	"fmt"
	"sort"
	"strings"
)

// Warning codes for static validation (non-fatal, advisory).
const (
	WarnNoHealthcheck = "W_NO_HEALTHCHECK"
	WarnNoBranch      = "W_NO_BRANCH"
)

// ValidationIssue is one problem found by ValidateCompose.
type ValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// ValidateCompose runs static semantic checks over an already-parsed compose (no I/O).
func ValidateCompose(c *CorgiCompose) (errs, warns []ValidationIssue) {
	if c == nil {
		return nil, nil
	}

	errs = append(errs, checkUnknownDrivers(c)...)
	errs = append(errs, checkDanglingDeps(c)...)
	errs = append(errs, checkDependencyCycles(c)...)
	errs = append(errs, checkMissingStart(c)...)
	errs = append(errs, checkPortConflicts(c)...)
	errs = append(errs, checkInvalidConditions(c)...)
	errs = append(errs, checkRestartPolicies(c)...)

	warns = append(warns, checkDependedWithoutHealthcheck(c)...)
	warns = append(warns, checkCloneWithoutBranch(c)...)

	return errs, warns
}

func checkRestartPolicies(c *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	for _, s := range c.Services {
		if err := ValidateRestartPolicy(s.RestartPolicy); err != nil {
			out = append(out, ValidationIssue{
				Code:    ErrUsage,
				Message: fmt.Sprintf("service %q: %v", s.ServiceName, err),
				Field:   fmt.Sprintf("services.%s.restartPolicy", s.ServiceName),
			})
		}
	}
	return out
}

// composeNames returns the known service / db names in the compose.
func composeNames(c *CorgiCompose) (services, dbs map[string]bool) {
	services = make(map[string]bool, len(c.Services))
	dbs = make(map[string]bool, len(c.DatabaseServices))
	for _, s := range c.Services {
		services[s.ServiceName] = true
	}
	for _, db := range c.DatabaseServices {
		dbs[db.ServiceName] = true
	}
	return services, dbs
}

func checkUnknownDrivers(c *CorgiCompose) []ValidationIssue {
	known := make(map[string]bool, len(KnownDrivers))
	for _, d := range KnownDrivers {
		known[d] = true
	}
	var out []ValidationIssue
	for _, db := range c.DatabaseServices {
		if db.Driver == "" || known[db.Driver] {
			continue
		}
		out = append(out, ValidationIssue{
			Code:    ErrUnknownDriver,
			Message: fmt.Sprintf("db_service %q uses unknown driver %q (known: %s)", db.ServiceName, db.Driver, strings.Join(KnownDrivers, ", ")),
			Field:   fmt.Sprintf("db_services.%s.driver", db.ServiceName),
		})
	}
	return out
}

func checkDanglingDeps(c *CorgiCompose) []ValidationIssue {
	services, dbs := composeNames(c)
	var out []ValidationIssue
	for _, s := range c.Services {
		for _, dep := range s.DependsOnServices {
			if dep.Name != "" && !services[dep.Name] {
				out = append(out, ValidationIssue{
					Code:    ErrDanglingDep,
					Message: fmt.Sprintf("service %q depends on unknown service %q", s.ServiceName, dep.Name),
					Field:   fmt.Sprintf("services.%s.depends_on_services", s.ServiceName),
				})
			}
		}
		for _, dep := range s.DependsOnDb {
			if dep.Name != "" && !dbs[dep.Name] {
				out = append(out, ValidationIssue{
					Code:    ErrDanglingDep,
					Message: fmt.Sprintf("service %q depends on unknown db_service %q", s.ServiceName, dep.Name),
					Field:   fmt.Sprintf("services.%s.depends_on_db", s.ServiceName),
				})
			}
		}
	}
	return out
}

// checkDependencyCycles reports one issue per service that participates in a
// cycle in the depends_on_services graph (edges to unknown services are
// ignored — those surface as dangling deps).
func checkDependencyCycles(c *CorgiCompose) []ValidationIssue {
	adj := buildServiceDepAdjacency(c)
	cyclic := findCyclicServices(c, adj)

	var out []ValidationIssue
	for _, n := range cyclic {
		out = append(out, ValidationIssue{
			Code:    ErrDependencyCycle,
			Message: fmt.Sprintf("service %q is part of a depends_on_services cycle", n),
			Field:   fmt.Sprintf("services.%s.depends_on_services", n),
		})
	}
	return out
}

// buildServiceDepAdjacency maps each service to the known services it depends
// on. Edges to unknown services are dropped (those surface as dangling deps).
func buildServiceDepAdjacency(c *CorgiCompose) map[string][]string {
	services, _ := composeNames(c)
	adj := make(map[string][]string, len(c.Services))
	for _, s := range c.Services {
		for _, dep := range s.DependsOnServices {
			if dep.Name != "" && services[dep.Name] {
				adj[s.ServiceName] = append(adj[s.ServiceName], dep.Name)
			}
		}
	}
	return adj
}

// findCyclicServices returns the sorted names of services that participate in a
// cycle within the dependency adjacency.
func findCyclicServices(c *CorgiCompose, adj map[string][]string) []string {
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(c.Services))
	inCycle := make(map[string]bool)

	var dfs func(node string, stack []string)
	dfs = func(node string, stack []string) {
		state[node] = visiting
		stack = append(stack, node)
		for _, next := range adj[node] {
			switch state[next] {
			case unvisited:
				dfs(next, stack)
			case visiting:
				markCycleFrom(stack, next, inCycle)
			}
		}
		state[node] = visited
	}

	// Deterministic start order.
	names := make([]string, 0, len(c.Services))
	for _, s := range c.Services {
		names = append(names, s.ServiceName)
	}
	sort.Strings(names)
	for _, n := range names {
		if state[n] == unvisited {
			dfs(n, nil)
		}
	}

	cyclic := make([]string, 0, len(inCycle))
	for n := range inCycle {
		cyclic = append(cyclic, n)
	}
	sort.Strings(cyclic)
	return cyclic
}

// markCycleFrom flags every node from the top of the DFS stack down to (and
// including) the back-edge target as being part of a cycle.
func markCycleFrom(stack []string, target string, inCycle map[string]bool) {
	for i := len(stack) - 1; i >= 0; i-- {
		inCycle[stack[i]] = true
		if stack[i] == target {
			return
		}
	}
}

// checkInvalidConditions flags a depends_on entry whose condition is set to
// something other than the supported "ready"/"started" values. The run path
// silently treats unknown values as "ready", so surface them as errors here.
func checkInvalidConditions(c *CorgiCompose) []ValidationIssue {
	valid := func(cond string) bool {
		return cond == "" || cond == "ready" || cond == "started"
	}
	var out []ValidationIssue
	for _, s := range c.Services {
		for i, dep := range s.DependsOnServices {
			if !valid(dep.Condition) {
				out = append(out, ValidationIssue{
					Code:    ErrInvalidCondition,
					Message: fmt.Sprintf("service %q has invalid depends_on condition %q (use %q or %q)", s.ServiceName, dep.Condition, "ready", "started"),
					Field:   fmt.Sprintf("services.%s.depends_on_services[%d].condition", s.ServiceName, i),
				})
			}
		}
		for i, dep := range s.DependsOnDb {
			if !valid(dep.Condition) {
				out = append(out, ValidationIssue{
					Code:    ErrInvalidCondition,
					Message: fmt.Sprintf("service %q has invalid depends_on condition %q (use %q or %q)", s.ServiceName, dep.Condition, "ready", "started"),
					Field:   fmt.Sprintf("services.%s.depends_on_db[%d].condition", s.ServiceName, i),
				})
			}
		}
	}
	return out
}

// checkMissingStart flags a service that exposes a port but has neither a
// start command nor a docker runner (which provides its own entrypoint).
func checkMissingStart(c *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	for _, s := range c.Services {
		if s.Port == 0 {
			continue
		}
		if len(s.Start) > 0 || s.Runner.Name == "docker" {
			continue
		}
		out = append(out, ValidationIssue{
			Code:    ErrMissingStart,
			Message: fmt.Sprintf("service %q sets port %d but has no start command and is not a docker runner", s.ServiceName, s.Port),
			Field:   fmt.Sprintf("services.%s.start", s.ServiceName),
		})
	}
	return out
}

// checkPortConflicts reports any host port claimed by more than one
// service / db_service. Port 0 (unset) is ignored.
func checkPortConflicts(c *CorgiCompose) []ValidationIssue {
	type owner struct {
		label string
		field string
	}
	byPort := make(map[int][]owner)
	for _, db := range c.DatabaseServices {
		if db.Port == 0 {
			continue
		}
		byPort[db.Port] = append(byPort[db.Port], owner{
			label: fmt.Sprintf("db_service %q", db.ServiceName),
			field: fmt.Sprintf("db_services.%s.port", db.ServiceName),
		})
	}
	for _, s := range c.Services {
		if s.Port == 0 {
			continue
		}
		byPort[s.Port] = append(byPort[s.Port], owner{
			label: fmt.Sprintf("service %q", s.ServiceName),
			field: fmt.Sprintf("services.%s.port", s.ServiceName),
		})
	}

	ports := make([]int, 0, len(byPort))
	for p := range byPort {
		ports = append(ports, p)
	}
	sort.Ints(ports)

	var out []ValidationIssue
	for _, p := range ports {
		owners := byPort[p]
		if len(owners) < 2 {
			continue
		}
		labels := make([]string, len(owners))
		for i, o := range owners {
			labels[i] = o.label
		}
		out = append(out, ValidationIssue{
			Code:    ErrPortConflict,
			Message: fmt.Sprintf("port %d is bound by %s", p, strings.Join(labels, ", ")),
			Field:   owners[0].field,
		})
	}
	return out
}

// checkDependedWithoutHealthcheck warns when a service that others depend on
// has no healthCheck — readiness then falls back to a plain TCP probe.
func checkDependedWithoutHealthcheck(c *CorgiCompose) []ValidationIssue {
	depended := make(map[string]bool)
	for _, s := range c.Services {
		for _, dep := range s.DependsOnServices {
			if dep.Name != "" {
				depended[dep.Name] = true
			}
		}
	}
	var out []ValidationIssue
	for _, s := range c.Services {
		if depended[s.ServiceName] && s.HealthCheck == "" {
			out = append(out, ValidationIssue{
				Code:    WarnNoHealthcheck,
				Message: fmt.Sprintf("service %q is depended on but has no healthCheck — a TCP probe will be used", s.ServiceName),
				Field:   fmt.Sprintf("services.%s.healthCheck", s.ServiceName),
			})
		}
	}
	return out
}

func checkCloneWithoutBranch(c *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	for _, s := range c.Services {
		if s.CloneFrom != "" && s.Branch == "" {
			out = append(out, ValidationIssue{
				Code:    WarnNoBranch,
				Message: fmt.Sprintf("service %q sets cloneFrom but no branch — the default branch will be used", s.ServiceName),
				Field:   fmt.Sprintf("services.%s.branch", s.ServiceName),
			})
		}
	}
	return out
}
