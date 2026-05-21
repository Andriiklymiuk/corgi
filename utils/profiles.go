package utils

// SelectByProfile returns the names of services and db_services to run for the
// given profile, including the transitive depends_on closure. profile=="" means
// select all. Returns sets (map[string]bool) keyed by service/db name.
//
// Selection starts from members whose Profiles contains profile, then walks
// each selected service's depends_on_services and depends_on_db to pull in the
// services/dbs they need — even if those have no profiles tag (docker-compose
// behavior: a frontend profile still brings up the DB it depends on). An
// unknown profile (no member matches) yields empty sets, so the caller can warn
// and start nothing rather than falling through to "select all".
func SelectByProfile(corgi *CorgiCompose, profile string) (services, dbs map[string]bool) {
	services = map[string]bool{}
	dbs = map[string]bool{}

	if profile == "" {
		for _, s := range corgi.Services {
			services[s.ServiceName] = true
		}
		for _, db := range corgi.DatabaseServices {
			dbs[db.ServiceName] = true
		}
		return services, dbs
	}

	svcByName := map[string]Service{}
	for _, s := range corgi.Services {
		svcByName[s.ServiceName] = s
	}

	// Seed the BFS with services that declare the profile.
	var queue []string
	for _, s := range corgi.Services {
		if containsString(s.Profiles, profile) {
			services[s.ServiceName] = true
			queue = append(queue, s.ServiceName)
		}
	}
	// db_services may also declare the profile directly.
	for _, db := range corgi.DatabaseServices {
		if containsString(db.Profiles, profile) {
			dbs[db.ServiceName] = true
		}
	}

	// BFS over service deps. dbs are leaves (no further service deps to walk).
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

	return services, dbs
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}
