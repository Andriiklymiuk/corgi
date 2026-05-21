package cmd

import (
	"os"
	"sort"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
)

// dryRunPlan is the JSON shape printed by `corgi run --dry-run --json`.
// Slices are always non-nil so the contract never emits null arrays.
type dryRunPlan struct {
	Valid     bool                    `json:"valid"`
	Order     []string                `json:"order"`
	Databases []dryRunDB              `json:"databases"`
	Services  []dryRunService         `json:"services"`
	Warnings  []utils.ValidationIssue `json:"warnings"`
	Errors    []utils.ValidationIssue `json:"errors,omitempty"`
}

type dryRunDB struct {
	Name      string `json:"name"`
	Driver    string `json:"driver"`
	Port      int    `json:"port"`
	WillStart bool   `json:"willStart"`
}

type dryRunService struct {
	Name      string   `json:"name"`
	Port      int      `json:"port"`
	WillClone bool     `json:"willClone"`
	DependsOn []string `json:"dependsOn"`
	EnvKeys   []string `json:"envKeys"`
}

// computeDryRunPlan builds the plan without any side effects. It runs static
// validation, resolves a topological start order, and reports per-item details.
func computeDryRunPlan(corgi *utils.CorgiCompose) dryRunPlan {
	errs, warns := utils.ValidateCompose(corgi)
	if errs == nil {
		errs = []utils.ValidationIssue{}
	}
	if warns == nil {
		warns = []utils.ValidationIssue{}
	}

	plan := dryRunPlan{
		Valid:     len(errs) == 0,
		Order:     computeStartOrder(corgi),
		Databases: []dryRunDB{},
		Services:  []dryRunService{},
		Warnings:  warns,
	}
	if len(errs) > 0 {
		plan.Errors = errs
	}

	for _, db := range corgi.DatabaseServices {
		plan.Databases = append(plan.Databases, dryRunDB{
			Name:      db.ServiceName,
			Driver:    db.Driver,
			Port:      db.Port,
			WillStart: !db.ManualRun,
		})
	}

	for _, svc := range corgi.Services {
		deps := serviceDeps(svc)
		envKeys := utils.ComputeEnvKeysForService(svc, corgi)
		plan.Services = append(plan.Services, dryRunService{
			Name:      svc.ServiceName,
			Port:      svc.Port,
			WillClone: willClone(svc),
			DependsOn: deps,
			EnvKeys:   envKeys,
		})
	}

	return plan
}

// willClone reports whether corgi would clone this service: cloneFrom is set
// and the target path does not yet exist on disk. os.Stat is read-only.
func willClone(svc utils.Service) bool {
	if svc.CloneFrom == "" {
		return false
	}
	if svc.AbsolutePath == "" {
		return true
	}
	if _, err := os.Stat(svc.AbsolutePath); err == nil {
		return false
	}
	return true
}

// serviceDeps returns the node ids this service depends on: db deps as
// db:<name> and service deps as svc:<name>, sorted for deterministic output.
func serviceDeps(svc utils.Service) []string {
	deps := []string{}
	for _, d := range svc.DependsOnDb {
		if d.Name != "" {
			deps = append(deps, "db:"+d.Name)
		}
	}
	for _, d := range svc.DependsOnServices {
		if d.Name != "" {
			deps = append(deps, "svc:"+d.Name)
		}
	}
	sort.Strings(deps)
	return deps
}

// computeStartOrder topologically sorts the dependency graph. Databases must
// precede any service depending on them; services follow their
// depends_on_services. Node ids are db:<name> and svc:<name>. On a cycle
// (already flagged as E_DEPENDENCY_CYCLE), it emits a best-effort order by
// appending the remaining nodes deterministically. Ties break by name.
func computeStartOrder(corgi *utils.CorgiCompose) []string {
	nodes := []string{}
	indeg := map[string]int{}
	graph := map[string][]string{}
	exists := map[string]bool{}

	addNode := func(id string) {
		if !exists[id] {
			exists[id] = true
			nodes = append(nodes, id)
			indeg[id] = 0
		}
	}

	for _, db := range corgi.DatabaseServices {
		addNode("db:" + db.ServiceName)
	}
	for _, svc := range corgi.Services {
		addNode("svc:" + svc.ServiceName)
	}

	addEdge := func(from, to string) {
		// from must precede to; only count edges between known nodes.
		if !exists[from] || !exists[to] {
			return
		}
		graph[from] = append(graph[from], to)
		indeg[to]++
	}

	for _, svc := range corgi.Services {
		to := "svc:" + svc.ServiceName
		for _, d := range svc.DependsOnDb {
			if d.Name != "" {
				addEdge("db:"+d.Name, to)
			}
		}
		for _, d := range svc.DependsOnServices {
			if d.Name != "" {
				addEdge("svc:"+d.Name, to)
			}
		}
	}

	return kahnSort(nodes, indeg, graph)
}

// kahnSort runs Kahn's algorithm with deterministic tie-breaking (lowest name
// first). Any nodes left after a cycle are appended in sorted order so the
// output stays best-effort and stable.
func kahnSort(nodes []string, indeg map[string]int, graph map[string][]string) []string {
	sorted := append([]string{}, nodes...)
	sort.Strings(sorted)

	ready := []string{}
	for _, n := range sorted {
		if indeg[n] == 0 {
			ready = append(ready, n)
		}
	}

	order := []string{}
	done := map[string]bool{}
	for len(ready) > 0 {
		sort.Strings(ready)
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		done[n] = true
		next := append([]string{}, graph[n]...)
		sort.Strings(next)
		for _, m := range next {
			indeg[m]--
			if indeg[m] == 0 {
				ready = append(ready, m)
			}
		}
	}

	// Cycle remnants: append deterministically so order is never empty.
	for _, n := range sorted {
		if !done[n] {
			order = append(order, n)
		}
	}
	return order
}

// emitDryRunPlan prints the plan and returns the process exit code.
func emitDryRunPlan(plan dryRunPlan) int {
	if utils.JSONOutput {
		utils.PrintJSON(plan)
		if !plan.Valid {
			return 1
		}
		return 0
	}

	printDryRunHuman(plan)
	if !plan.Valid {
		return 1
	}
	return 0
}

func printDryRunHuman(plan dryRunPlan) {
	utils.Info(art.BlueColor, "🐶 corgi run --dry-run (no side effects)", art.WhiteColor)
	if !plan.Valid {
		utils.Info(art.RedColor, "validation failed:", art.WhiteColor)
		for _, e := range plan.Errors {
			utils.Infof("  ✗ [%s] %s\n", e.Code, e.Message)
		}
	}
	for _, w := range plan.Warnings {
		utils.Infof("  ⚠ [%s] %s\n", w.Code, w.Message)
	}

	utils.Info("\nStart order:")
	for i, id := range plan.Order {
		utils.Infof("  %d. %s\n", i+1, id)
	}

	if len(plan.Databases) > 0 {
		utils.Info("\nDatabases:")
		for _, db := range plan.Databases {
			utils.Infof("  • %s (driver=%s, port=%d, willStart=%t)\n", db.Name, db.Driver, db.Port, db.WillStart)
		}
	}

	if len(plan.Services) > 0 {
		utils.Info("\nServices:")
		for _, s := range plan.Services {
			utils.Infof("  • %s (port=%d, willClone=%t)\n", s.Name, s.Port, s.WillClone)
			if len(s.DependsOn) > 0 {
				utils.Infof("      depends on: %v\n", s.DependsOn)
			}
			if len(s.EnvKeys) > 0 {
				utils.Infof("      env keys: %v\n", s.EnvKeys)
			}
		}
	}
}
