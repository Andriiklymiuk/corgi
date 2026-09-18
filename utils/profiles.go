package utils

import (
	"sort"
	"strings"
)

var WithDepsFromFlag bool

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

func ParseProfiles(value string) []string {
	var out []string
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func SelectByProfile(corgi *CorgiCompose, profile string) (services, dbs map[string]bool) {
	return SelectByProfiles(corgi, []string{profile})
}

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

func seedProfileSelection(corgi *CorgiCompose, profiles []string) (services, dbs map[string]bool, queue []string) {
	services = map[string]bool{}
	dbs = map[string]bool{}
	for _, s := range corgi.Services {
		if intersects(s.Profiles, profiles) {
			services[s.ServiceName] = true
			queue = append(queue, s.ServiceName)
		}
	}
	for _, db := range corgi.DatabaseServices {
		if intersects(db.Profiles, profiles) {
			dbs[db.ServiceName] = true
		}
	}
	return services, dbs, queue
}

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
