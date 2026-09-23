package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	WarnNoHealthcheck = "W_NO_HEALTHCHECK"
	WarnNoBranch      = "W_NO_BRANCH"
	WarnUnknownField  = "W_UNKNOWN_FIELD"
)

type ValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

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
	errs = append(errs, checkDuplicateNames(c)...)
	errs = append(errs, checkPortRanges(c)...)

	warns = append(warns, checkDependedWithoutHealthcheck(c)...)
	warns = append(warns, checkCloneWithoutBranch(c)...)
	warns = append(warns, checkUnknownFields(c)...)

	return errs, warns
}

func checkUnknownFields(_ *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	for _, f := range UnknownComposeFields {
		out = append(out, ValidationIssue{
			Code:    WarnUnknownField,
			Message: fmt.Sprintf("unknown field %q in corgi-compose.yml - possible typo; it was ignored", f),
			Field:   f,
		})
	}
	return out
}

func CollectValidationErrors(c *CorgiCompose) []ValidationIssue {
	errs, _ := ValidateCompose(c)
	return errs
}

func AbortOnValidationErrors(c *CorgiCompose) bool {
	errs := CollectValidationErrors(c)
	if len(errs) == 0 {
		return true
	}
	if JSONOutput {
		for _, e := range errs {
			JSONError(e.Code, e.Message)
		}
		return false
	}
	Infof("%s✗ corgi-compose.yml has %d validation error(s):%s\n", art.RedColor, len(errs), art.WhiteColor)
	for _, e := range errs {
		field := ""
		if e.Field != "" {
			field = fmt.Sprintf(" (%s)", e.Field)
		}
		Infof("  %s✗ [%s] %s%s%s\n", art.RedColor, e.Code, e.Message, field, art.WhiteColor)
	}
	return false
}

func checkDuplicateNames(c *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	services, dbs := composeNames(c)
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if dbs[n] {
			out = append(out, ValidationIssue{
				Code:    ErrDuplicateName,
				Message: fmt.Sprintf("name %q is used by both a service and a db_service - names must be unique", n),
				Field:   fmt.Sprintf("services.%s", n),
			})
		}
	}
	for _, dup := range DuplicateComposeKeys {
		out = append(out, ValidationIssue{
			Code:    ErrDuplicateName,
			Message: fmt.Sprintf("duplicate key %q - YAML keeps only the last; remove the duplicate", dup),
			Field:   dup,
		})
	}
	return out
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

func checkDependencyCycles(c *CorgiCompose) []ValidationIssue {
	adj := buildGatedServiceDepAdjacency(c)
	cyclic := findCyclicServices(c, adj)

	var out []ValidationIssue
	for _, n := range cyclic {
		out = append(out, ValidationIssue{
			Code:    ErrDependencyCycle,
			Message: fmt.Sprintf("service %q is part of a condition:-gated depends_on_services cycle - gated deps in a cycle wait on each other and time out", n),
			Field:   fmt.Sprintf("services.%s.depends_on_services", n),
		})
	}
	return out
}

func buildGatedServiceDepAdjacency(c *CorgiCompose) map[string][]string {
	services, _ := composeNames(c)
	adj := make(map[string][]string, len(c.Services))
	for _, s := range c.Services {
		for _, dep := range s.DependsOnServices {
			if dep.Name == "" || !services[dep.Name] || dep.Condition == "" {
				continue
			}
			adj[s.ServiceName] = append(adj[s.ServiceName], dep.Name)
		}
	}
	return adj
}

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

func markCycleFrom(stack []string, target string, inCycle map[string]bool) {
	for i := len(stack) - 1; i >= 0; i-- {
		inCycle[stack[i]] = true
		if stack[i] == target {
			return
		}
	}
}

const (
	condReady   = "ready"
	condStarted = "started"
)

func checkInvalidConditions(c *CorgiCompose) []ValidationIssue {
	valid := func(cond string) bool {
		return cond == "" || cond == condReady || cond == condStarted
	}
	invalidCondition := func(svc, cond, field string) ValidationIssue {
		return ValidationIssue{
			Code:    ErrInvalidCondition,
			Message: fmt.Sprintf("service %q has invalid depends_on condition %q (use %q or %q)", svc, cond, condReady, condStarted),
			Field:   field,
		}
	}
	var out []ValidationIssue
	for _, s := range c.Services {
		for i, dep := range s.DependsOnServices {
			if !valid(dep.Condition) {
				out = append(out, invalidCondition(s.ServiceName, dep.Condition,
					fmt.Sprintf("services.%s.depends_on_services[%d].condition", s.ServiceName, i)))
			}
		}
		for i, dep := range s.DependsOnDb {
			if !valid(dep.Condition) {
				out = append(out, invalidCondition(s.ServiceName, dep.Condition,
					fmt.Sprintf("services.%s.depends_on_db[%d].condition", s.ServiceName, i)))
			}
		}
	}
	return out
}

func checkMissingStart(c *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	for _, s := range c.Services {
		if s.Port == 0 || s.ManualRun {
			continue
		}
		if len(s.Start) > 0 || s.Runner.IsDocker() {
			continue
		}
		if DetectDockerSource(s) != SourceNone {
			continue
		}
		if s.CloneFrom != "" {
			if _, err := os.Stat(s.AbsolutePath); err != nil {
				continue
			}
		}
		out = append(out, ValidationIssue{
			Code:    ErrMissingStart,
			Message: fmt.Sprintf("service %q sets port %d but has no start command, no Dockerfile and no compose file", s.ServiceName, s.Port),
			Field:   fmt.Sprintf("services.%s.start", s.ServiceName),
		})
	}
	return out
}

func checkPortConflicts(c *CorgiCompose) []ValidationIssue {
	type owner struct {
		label string
		field string
	}
	byPort := make(map[int][]owner)
	for _, db := range c.DatabaseServices {
		if db.Port == 0 || db.ManualRun {
			continue
		}
		byPort[db.Port] = append(byPort[db.Port], owner{
			label: fmt.Sprintf("db_service %q", db.ServiceName),
			field: fmt.Sprintf("db_services.%s.port", db.ServiceName),
		})
	}
	for _, s := range c.Services {
		if s.Port == 0 || s.ManualRun {
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

func checkPortRanges(c *CorgiCompose) []ValidationIssue {
	var out []ValidationIssue
	flag := func(port int, label, field string) {
		if port == 0 || (port >= 1 && port <= 65535) {
			return
		}
		out = append(out, ValidationIssue{
			Code:    ErrPortRange,
			Message: fmt.Sprintf("%s port %d is out of range (must be 1-65535)", label, port),
			Field:   field,
		})
	}
	for _, db := range c.DatabaseServices {
		flag(db.Port, fmt.Sprintf("db_service %q", db.ServiceName), fmt.Sprintf("db_services.%s.port", db.ServiceName))
		flag(db.Port2, fmt.Sprintf("db_service %q port2", db.ServiceName), fmt.Sprintf("db_services.%s.port2", db.ServiceName))
		flag(db.ContainerPort, fmt.Sprintf("db_service %q containerPort", db.ServiceName), fmt.Sprintf("db_services.%s.containerPort", db.ServiceName))
		flag(db.StudioPort, fmt.Sprintf("db_service %q studioPort", db.ServiceName), fmt.Sprintf("db_services.%s.studioPort", db.ServiceName))
	}
	for _, s := range c.Services {
		flag(s.Port, fmt.Sprintf("service %q", s.ServiceName), fmt.Sprintf("services.%s.port", s.ServiceName))
	}
	return out
}

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
				Message: fmt.Sprintf("service %q is depended on but has no healthCheck - a TCP probe will be used", s.ServiceName),
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
				Message: fmt.Sprintf("service %q sets cloneFrom but no branch - the default branch will be used", s.ServiceName),
				Field:   fmt.Sprintf("services.%s.branch", s.ServiceName),
			})
		}
	}
	return out
}
