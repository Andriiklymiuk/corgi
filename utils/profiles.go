package utils

import (
	"sort"
	"strings"
)

// WithDepsFromFlag is the --with-deps flag: expand --services through its
// depends_on closure.
var WithDepsFromFlag bool

// applyWithDeps rewrites the --services/--dbServices flag globals to the
// depends_on closure of the selected services. No-op unless --with-deps and a
// non-empty --services were given.
func applyWithDeps(servicesMap map[string]Service) {
	if !WithDepsFromFlag || len(ServicesItemsFromFlag) == 0 {
		return
	}
	svcs, dbs := expandWithDeps(servicesMap, ServicesItemsFromFlag)
	if len(svcs) == 0 {
		return
	}
	ServicesItemsFromFlag = sortedSetKeys(svcs)

	set := map[string]bool{}
	for _, d := range DbServicesItemsFromFlag {
		if d != "" && d != "none" {
			set[d] = true
		}
	}
	for d := range dbs {
		set[d] = true
	}
	if len(set) > 0 {
		DbServicesItemsFromFlag = sortedSetKeys(set)
	}
}

func sortedSetKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParseProfiles splits a comma-separated --profile value into trimmed,
// non-empty tokens. Returns nil when nothing meaningful is present.
func ParseProfiles(value string) []string {
	var out []string
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SelectByProfile is the single-profile form of SelectByProfiles.
func SelectByProfile(corgi *CorgiCompose, profile string) (services, dbs map[string]bool) {
	return SelectByProfiles(corgi, []string{profile})
}

// SelectByProfiles returns the services and db_services to run for the union of
// the given profiles, including the transitive depends_on closure (so a profile
// pulls in dependencies even when they carry no profiles tag, matching
// docker-compose). Empty/nil (or a single "") means select all.
func SelectByProfiles(corgi *CorgiCompose, profiles []string) (services, dbs map[string]bool) {
	if len(profiles) == 0 || (len(profiles) == 1 && profiles[0] == "") {
		return selectAll(corgi)
	}

	svcByName := map[string]Service{}
	for _, s := range corgi.Services {
		svcByName[s.ServiceName] = s
	}

	services, dbs, queue := seedProfileSelection(corgi, profiles)
	walkDepClosure(svcByName, services, dbs, queue)
	return services, dbs
}

// selectAll returns every service and db_service (the profile=="" case).
func selectAll(corgi *CorgiCompose) (services, dbs map[string]bool) {
	services = map[string]bool{}
	dbs = map[string]bool{}
	for _, s := range corgi.Services {
		services[s.ServiceName] = true
	}
	for _, db := range corgi.DatabaseServices {
		dbs[db.ServiceName] = true
	}
	return services, dbs
}

// seedProfileSelection collects services and db_services that directly declare
// any of the profiles, plus the BFS queue of seed services to expand.
func seedProfileSelection(corgi *CorgiCompose, profiles []string) (services, dbs map[string]bool, queue []string) {
	services = map[string]bool{}
	dbs = map[string]bool{}
	for _, s := range corgi.Services {
		if intersects(s.Profiles, profiles) {
			services[s.ServiceName] = true
			queue = append(queue, s.ServiceName)
		}
	}
	// db_services may also declare a profile directly.
	for _, db := range corgi.DatabaseServices {
		if intersects(db.Profiles, profiles) {
			dbs[db.ServiceName] = true
		}
	}
	return services, dbs, queue
}

// walkDepClosure expands the BFS queue over depends_on_services, pulling each
// service's transitive service and db dependencies into the selection sets.
func walkDepClosure(svcByName map[string]Service, services, dbs map[string]bool, queue []string) {
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		svc, ok := svcByName[name]
		if !ok {
			continue
		}
		for _, dep := range svc.DependsOnServices {
			if dep.Name != "" && !services[dep.Name] {
				services[dep.Name] = true
				queue = append(queue, dep.Name)
			}
		}
		for _, dep := range svc.DependsOnDb {
			if dep.Name != "" {
				dbs[dep.Name] = true
			}
		}
	}
}

// expandWithDeps returns the seed services plus their transitive depends_on
// closure (services + dbs), for `corgi run --services X --with-deps`.
func expandWithDeps(servicesMap map[string]Service, seeds []string) (services, dbs map[string]bool) {
	services = map[string]bool{}
	dbs = map[string]bool{}
	var queue []string
	for _, s := range seeds {
		if s == "" || s == "none" {
			continue
		}
		if _, ok := servicesMap[s]; ok {
			services[s] = true
			queue = append(queue, s)
		}
	}
	walkDepClosure(servicesMap, services, dbs, queue)
	return services, dbs
}

// intersects reports whether any wanted profile is present in have.
func intersects(have, wanted []string) bool {
	for _, w := range wanted {
		if containsString(have, w) {
			return true
		}
	}
	return false
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}
