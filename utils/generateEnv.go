package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const corgiGeneratedMessage = "# 🐶 Auto generated vars by corgi"

func getEnvFromFile(filePath, corgiGeneratedMessage string) string {
	envFileContent := GetFileContent(filePath)
	var envFileNormalizedContent []string
	for _, content := range envFileContent {
		if content != corgiGeneratedMessage {
			envFileNormalizedContent = append(envFileNormalizedContent, content)
		}
	}

	return strings.Join(envFileNormalizedContent, "\n") + "\n"
}

func createEnvString(envForService, envName, host, port, suffix string) string {
	return fmt.Sprintf("%s%s=http://%s:%s%s\n", envForService, envName, host, port, suffix)
}

func findServiceByName(services []Service, serviceName string) *Service {
	for _, s := range services {
		if s.ServiceName == serviceName {
			return &s
		}
	}
	return nil
}

func handleDependentServices(service Service, corgiCompose CorgiCompose) string {
	if service.DependsOnServices == nil {
		return ""
	}
	envForService := ""
	for _, dep := range service.DependsOnServices {
		envForService = appendDependentServiceEnv(envForService, dep, corgiCompose)
	}
	return envForService
}

func appendDependentServiceEnv(envForService string, dep DependsOnService, corgiCompose CorgiCompose) string {
	s := findServiceByName(corgiCompose.Services, dep.Name)
	if s == nil {
		return envForService
	}
	if s.ManualRun && !dep.ForceUseEnv {
		return envForService
	}

	envNameToUse := dep.EnvAlias
	if envNameToUse == "" {
		envNameToUse = splitStringForEnv(s.ServiceName) + "_URL"
	}

	if s.Port != 0 {
		return createEnvString(envForService, envNameToUse, ServiceHost(), fmt.Sprint(s.Port), dep.Suffix)
	}
	for _, envLine := range s.Environment {
		parts := strings.SplitN(envLine, "=", 2)
		if len(parts) == 2 && parts[0] == "PORT" {
			envForService = createEnvString(envForService, envNameToUse, ServiceHost(), parts[1], dep.Suffix)
		}
	}
	return envForService
}

func handleDependsOnDb(service Service, corgiCompose CorgiCompose) string {
	if service.DependsOnDb == nil {
		return ""
	}
	var envForService string
	for _, dep := range service.DependsOnDb {
		db := findDbByName(corgiCompose.DatabaseServices, dep.Name)
		if db == nil || (db.ManualRun && !dep.ForceUseEnv) {
			continue
		}
		envForService += generateEnvForDbDependentService(service, dep, *db)
	}
	return envForService
}

func findDbByName(dbs []DatabaseService, name string) *DatabaseService {
	for i := range dbs {
		if dbs[i].ServiceName == name {
			return &dbs[i]
		}
	}
	return nil
}

func generateEnvForDbDependentService(service Service, dependingDb DependsOnDb, db DatabaseService) string {
	var serviceNameInEnv string

	if len(service.DependsOnDb) > 1 {
		serviceNameInEnv = splitStringForEnv(db.ServiceName) + "_"
	}
	if dependingDb.EnvAlias != "" {
		if dependingDb.EnvAlias == "none" {
			serviceNameInEnv = ""
		} else {
			serviceNameInEnv = dependingDb.EnvAlias + "_"
		}
	}

	driverConfig, ok := DriverConfigs[db.Driver]
	if !ok {
		driverConfig = DriverConfigs["default"]
	}

	serviceNameInEnv += driverConfig.Prefix
	envForService := driverConfig.EnvGenerator(serviceNameInEnv, db)

	return envForService
}

func EnsurePathExists(dirName string) error {
	_, err := os.Stat(dirName)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dirName, 0755)
}

// ExportsMap holds resolved exports per service: serviceName -> varName -> value.
type ExportsMap map[string]map[string]string

var currentExportsMap ExportsMap

// Returned when a ${producer.VAR} ref points at a service excluded by
// --services. Callers drop the env line instead of erroring.
type producerSkippedError struct {
	producer string
	varName  string
}

func (e *producerSkippedError) Error() string {
	return fmt.Sprintf("producer %q is not in --services run; skipping ${%s.%s}", e.producer, e.producer, e.varName)
}

var crossServiceRefRe = regexp.MustCompile(`\$\{([A-Za-z0-9_\-/]+)\.([A-Za-z_][A-Za-z0-9_]*)\}`)

// topoSortServices returns services in dependency order (deps first).
//
// Only "hard" edges add ordering: a hard edge is created when consumer's
// environment block contains ${producer.VAR}. depends_on_services entries
// that are alias-only (e.g. envAlias: BASE_URL) are "soft" — their emitted
// value is a static localhost:port and needs no ordering. Self-deps are
// always ignored.
//
// Cycles only matter when both sides reference each other's exports. If a
// cycle is detected, the error names the services involved.
func collectProducers(s Service) map[string]bool {
	producers := map[string]bool{}
	for _, env := range s.Environment {
		for _, m := range crossServiceRefRe.FindAllStringSubmatch(env, -1) {
			producers[m[1]] = true
		}
	}
	return producers
}

func addServiceEdges(s Service, byName map[string]Service, graph map[string][]string, indeg map[string]int) {
	producers := collectProducers(s)
	seen := map[string]bool{}
	for _, d := range s.DependsOnServices {
		if d.Name == s.ServiceName || !producers[d.Name] {
			continue
		}
		if _, ok := byName[d.Name]; !ok {
			continue
		}
		if seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		graph[d.Name] = append(graph[d.Name], s.ServiceName)
		indeg[s.ServiceName]++
	}
}

func topoSortServices(services []Service) ([]Service, error) {
	indeg := map[string]int{}
	graph := map[string][]string{}
	byName := map[string]Service{}

	for _, s := range services {
		byName[s.ServiceName] = s
		if _, ok := indeg[s.ServiceName]; !ok {
			indeg[s.ServiceName] = 0
		}
	}
	for _, s := range services {
		addServiceEdges(s, byName, graph, indeg)
	}

	queue := initialZeroInDegree(services, indeg)
	ordered := drainTopoQueue(queue, byName, graph, indeg)

	if len(ordered) != len(services) {
		return nil, cycleError(services, indeg)
	}
	return ordered, nil
}

func initialZeroInDegree(services []Service, indeg map[string]int) []string {
	var queue []string
	for _, s := range services {
		if indeg[s.ServiceName] == 0 {
			queue = append(queue, s.ServiceName)
		}
	}
	return queue
}

func drainTopoQueue(queue []string, byName map[string]Service, graph map[string][]string, indeg map[string]int) []Service {
	var ordered []Service
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		ordered = append(ordered, byName[n])
		for _, m := range graph[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	return ordered
}

func cycleError(services []Service, indeg map[string]int) error {
	var stuck []string
	for _, s := range services {
		if indeg[s.ServiceName] > 0 {
			stuck = append(stuck, s.ServiceName)
		}
	}
	return fmt.Errorf(
		"cycle detected in cross-service ${producer.VAR} references involving: %s",
		strings.Join(stuck, ", "),
	)
}

// resolveExports computes the exports map for a producer service from its
// resolved env. Entries with `=` are inline literals (with ${OWN_VAR} expansion);
// entries without `=` are re-exports of an existing env var.
func resolveExports(service Service, producerEnv map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, entry := range service.Exports {
		if idx := strings.Index(entry, "="); idx >= 0 {
			name := strings.TrimSpace(entry[:idx])
			rawVal := entry[idx+1:]
			out[name] = substituteEnvVarReferences(rawVal, producerEnv)
			continue
		}
		name := strings.TrimSpace(entry)
		val, ok := producerEnv[name]
		if !ok {
			return nil, fmt.Errorf(
				"service %q exports %q but it is not present in the service's resolved env",
				service.ServiceName, name,
			)
		}
		out[name] = val
	}
	return out, nil
}

// substituteCrossServiceRefs expands ${producer.VAR} references against the
// cross-service exports map. Validates that the producer is listed in the
// consumer's depends_on_services and that VAR is exported.
func resolveCrossServiceRef(
	consumer Service,
	allowed map[string]bool,
	exports ExportsMap,
	producer, varName, match string,
) (string, error) {
	if !allowed[producer] {
		return match, fmt.Errorf(
			"service %q references ${%s.%s} but %q is not in depends_on_services",
			consumer.ServiceName, producer, varName, producer,
		)
	}
	if SkippedServices[producer] {
		return match, &producerSkippedError{producer: producer, varName: varName}
	}
	producerExports, ok := exports[producer]
	if !ok {
		return match, fmt.Errorf(
			"service %q references ${%s.%s} but %q has no exports",
			consumer.ServiceName, producer, varName, producer,
		)
	}
	val, ok := producerExports[varName]
	if !ok {
		return match, fmt.Errorf(
			"service %q references ${%s.%s} but %q is not exported by %q",
			consumer.ServiceName, producer, varName, varName, producer,
		)
	}
	return val, nil
}

func substituteCrossServiceRefs(
	envLine string,
	consumer Service,
	exports ExportsMap,
) (string, error) {
	if exports == nil {
		return envLine, nil
	}
	allowed := map[string]bool{}
	for _, d := range consumer.DependsOnServices {
		allowed[d.Name] = true
	}

	var firstErr error
	out := crossServiceRefRe.ReplaceAllStringFunc(envLine, func(match string) string {
		m := crossServiceRefRe.FindStringSubmatch(match)
		val, err := resolveCrossServiceRef(consumer, allowed, exports, m[1], m[2], match)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return val
	})
	return out, firstErr
}

// Adds env variables to each service, including dependent db_services and services.
//
// Resolution is two-phase so that bidirectional ${producer.VAR} references
// (codependencies) work as long as the actual VAR values don't form a true
// cycle:
//
//  1. Try topo-sort. If it succeeds, process services in dependency order —
//     each service's exports are fully resolved before any consumer runs.
//     This is the fast path and matches pre-1.12 behavior.
//
//  2. If topo-sort detects a cycle (both sides reference each other's
//     exports), fall back to fixed-point resolution: build each service's
//     exports from its local env without cross-ref substitution, then
//     iteratively expand ${producer.VAR} within the exports map until
//     stable. Finally, write each service's env file using the resolved
//     map. A genuine VAR-level cycle (e.g. A.X="${B.Y}" and B.Y="${A.X}")
//     remains unresolvable and surfaces as an error naming the stuck vars.
func GenerateEnvForServices(corgiCompose *CorgiCompose) error {
	ordered, err := topoSortServices(corgiCompose.Services)
	if err == nil {
		currentExportsMap = ExportsMap{}
		defer func() { currentExportsMap = nil }()

		for _, service := range ordered {
			if err := GenerateEnvForService(
				corgiCompose,
				service,
				"",
				false,
			); err != nil {
				return err
			}
		}
		return nil
	}

	// Fallback: fixed-point resolution for codependencies.
	resolved, fpErr := resolveExportsFixedPoint(corgiCompose)
	if fpErr != nil {
		// Both topo and fixed-point failed — true cycle. Surface the
		// fixed-point error since it names the actual stuck variables.
		fmt.Println(art.RedColor, "service env generation:", fpErr, art.WhiteColor)
		return fpErr
	}
	currentExportsMap = resolved
	defer func() { currentExportsMap = nil }()

	for _, service := range corgiCompose.Services {
		if err := GenerateEnvForService(
			corgiCompose,
			service,
			"",
			false,
		); err != nil {
			return err
		}
	}
	return nil
}

// resolveExportsFixedPoint is the codependency fallback. It builds each
// service's exports from its local env (no cross-ref substitution), then
// iteratively expands ${producer.VAR} within the exports map until either
// stable or all references are resolved. Returns an error naming any
// vars whose values still contain unresolved cross-refs after the
// iteration limit (true VAR-level cycle).
func buildServiceExports(s Service, envMap map[string]string) map[string]string {
	exports := map[string]string{}
	for _, entry := range s.Exports {
		if idx := strings.Index(entry, "="); idx >= 0 {
			name := strings.TrimSpace(entry[:idx])
			rawVal := entry[idx+1:]
			exports[name] = substituteEnvVarReferences(rawVal, envMap)
			continue
		}
		name := strings.TrimSpace(entry)
		if val, ok := envMap[name]; ok {
			exports[name] = val
		}
	}
	return exports
}

func iterateExportsFixedPoint(out ExportsMap, consumers map[string]Service, maxIter int) {
	for iter := 0; iter < maxIter; iter++ {
		if !applyOnePass(out, consumers) {
			return
		}
	}
}

func applyOnePass(out ExportsMap, consumers map[string]Service) bool {
	changed := false
	for serviceName, vars := range out {
		if substituteServiceVars(vars, consumers[serviceName], out) {
			changed = true
		}
	}
	return changed
}

func substituteServiceVars(vars map[string]string, consumer Service, out ExportsMap) bool {
	changed := false
	for varName, val := range vars {
		if !crossServiceRefRe.MatchString(val) {
			continue
		}
		newVal, subErr := substituteCrossServiceRefs(val, consumer, out)
		if subErr != nil {
			var skipped *producerSkippedError
			if errors.As(subErr, &skipped) {
				// Strip skipped refs so findStuckExports doesn't flag a fake cycle.
				stripped := crossServiceRefRe.ReplaceAllStringFunc(val, func(match string) string {
					m := crossServiceRefRe.FindStringSubmatch(match)
					if SkippedServices[m[1]] {
						return ""
					}
					return match
				})
				if stripped != val {
					vars[varName] = stripped
					changed = true
				}
			}
			continue
		}
		if newVal != val {
			vars[varName] = newVal
			changed = true
		}
	}
	return changed
}

func findStuckExports(out ExportsMap) []string {
	var stuck []string
	for serviceName, vars := range out {
		for varName, val := range vars {
			if crossServiceRefRe.MatchString(val) {
				stuck = append(stuck, fmt.Sprintf("%s.%s=%q", serviceName, varName, val))
			}
		}
	}
	return stuck
}

func resolveExportsFixedPoint(c *CorgiCompose) (ExportsMap, error) {
	out := ExportsMap{}
	for _, s := range c.Services {
		if len(s.Exports) == 0 {
			continue
		}
		localEnv := buildLocalEnv(s, *c)
		envMap := parseEnvVarsIntoMap(localEnv)
		out[s.ServiceName] = buildServiceExports(s, envMap)
	}

	consumers := map[string]Service{}
	for _, s := range c.Services {
		consumers[s.ServiceName] = s
	}

	maxIter := 0
	for _, vars := range out {
		maxIter += len(vars)
	}
	maxIter = maxIter*2 + 1

	iterateExportsFixedPoint(out, consumers, maxIter)

	if stuck := findStuckExports(out); len(stuck) > 0 {
		return nil, fmt.Errorf(
			"cycle in cross-service exports — could not resolve: %s",
			strings.Join(stuck, ", "),
		)
	}
	return out, nil
}

// buildLocalEnv replicates the env-construction phase of GenerateEnvForService
// without writing a file or substituting cross-service refs. Used by the
// codependency fallback to compute each service's exports independently of
// resolution order.
func buildLocalEnv(service Service, corgiCompose CorgiCompose) string {
	var envForService string
	if service.CopyEnvFromFilePath != "" {
		path := fmt.Sprintf("%s/%s", CorgiComposePathDir, service.CopyEnvFromFilePath)
		envForService = getEnvFromFile(path, corgiGeneratedMessage)
	}
	envForService += handleDependentServices(service, corgiCompose)
	envForService += handleDependsOnDb(service, corgiCompose)
	if service.Port != 0 {
		portAlias := "PORT"
		if service.PortAlias != "" {
			portAlias = service.PortAlias
		}
		envForService += fmt.Sprintf("\n%s=%d", portAlias, service.Port)
	}
	if len(service.Environment) > 0 {
		existing := parseEnvVarsIntoMap(envForService)
		var lines []string
		for _, line := range service.Environment {
			// Only own ${VAR} substitution; leave ${producer.VAR} literal
			// for the fixed-point resolver to handle.
			lines = append(lines, substituteEnvVarReferences(line, existing))
		}
		envForService += "\n" + strings.Join(lines, "\n") + "\n"
	}
	return envForService
}

func resolveCopyEnvPath(service Service, copyEnvFilePath string) string {
	if copyEnvFilePath != "" {
		return copyEnvFilePath
	}
	return service.CopyEnvFromFilePath
}

func appendEnvironmentLines(envForService string, service Service) (string, error) {
	if len(service.Environment) == 0 {
		return envForService, nil
	}
	existingEnvVars := parseEnvVarsIntoMap(envForService)
	var updatedEnvironment []string
	for _, envLine := range service.Environment {
		crossExpanded, err := substituteCrossServiceRefs(envLine, service, currentExportsMap)
		if err != nil {
			var skipped *producerSkippedError
			if errors.As(err, &skipped) {
				fmt.Println(
					art.YellowColor,
					"ℹ️  dropping env line", envLine, "for", service.ServiceName,
					"— references ${"+skipped.producer+"."+skipped.varName+"} but",
					skipped.producer, "is not in --services run",
					art.WhiteColor,
				)
				continue
			}
			fmt.Println(art.RedColor, "env generation for", service.ServiceName, ":", err, art.WhiteColor)
			return "", err
		}
		updatedEnvLine := substituteEnvVarReferences(crossExpanded, existingEnvVars)
		updatedEnvironment = append(updatedEnvironment, updatedEnvLine)
	}
	return envForService + "\n" + strings.Join(updatedEnvironment, "\n") + "\n", nil
}

func buildServiceEnvBody(service Service, corgiCompose *CorgiCompose, copyEnvFilePath string, ignoreDependentServicesEnvs bool) (string, error) {
	var envForService string
	pathToCopyEnvFileFrom := resolveCopyEnvPath(service, copyEnvFilePath)
	if pathToCopyEnvFileFrom != "" {
		copyEnvFromFileAbsolutePath := fmt.Sprintf("%s/%s", CorgiComposePathDir, pathToCopyEnvFileFrom)
		envForService = getEnvFromFile(copyEnvFromFileAbsolutePath, corgiGeneratedMessage)
	}

	if ignoreDependentServicesEnvs {
		return envForService, nil
	}

	envForService += handleDependentServices(service, *corgiCompose)
	envForService += handleDependsOnDb(service, *corgiCompose)

	if service.Port != 0 {
		portAlias := "PORT"
		if service.PortAlias != "" {
			portAlias = service.PortAlias
		}
		envForService += fmt.Sprintf("\n%s=%d", portAlias, service.Port)
	}

	return appendEnvironmentLines(envForService, service)
}

func recordExportsForService(service Service, envForService string) error {
	if currentExportsMap == nil || len(service.Exports) == 0 {
		return nil
	}
	if _, already := currentExportsMap[service.ServiceName]; already {
		return nil
	}
	producerEnv := parseEnvVarsIntoMap(envForService)
	resolved, err := resolveExports(service, producerEnv)
	if err != nil {
		fmt.Println(art.RedColor, "exports for", service.ServiceName, ":", err, art.WhiteColor)
		return err
	}
	currentExportsMap[service.ServiceName] = resolved
	return nil
}

func renderEnvFileContent(pathToEnvFile string, envForService string, service Service) string {
	envFileContent := GetFileContent(pathToEnvFile)
	var corgiEnvPosition []int
	for index, line := range envFileContent {
		if line == corgiGeneratedMessage {
			corgiEnvPosition = append(corgiEnvPosition, index)
		}
	}
	if len(corgiEnvPosition) == 2 {
		envFileContent = removeFromToIndexes(envFileContent, corgiEnvPosition[0], corgiEnvPosition[1])
	}

	envFileContentString := strings.Join(envFileContent, "\n")
	if len(envForService) != 0 {
		envFileContentString += fmt.Sprintf(
			"\n%s\n%s\n%s\n",
			corgiGeneratedMessage,
			envForService,
			corgiGeneratedMessage,
		)
	}
	// LocalhostNameInEnv wins over --host for this service: it rewrites every
	// "localhost" in the file (incl. db hosts), while --host only touches
	// service URLs at generation time.
	if service.LocalhostNameInEnv != "" {
		envFileContentString = strings.ReplaceAll(envFileContentString, "localhost", service.LocalhostNameInEnv)
	}
	return envFileContentString
}

func writeEnvFile(pathToEnvFile, content string) error {
	if content == "" {
		return nil
	}
	f, err := os.OpenFile(pathToEnvFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

func GenerateEnvForService(
	corgiCompose *CorgiCompose,
	service Service,
	copyEnvFilePath string,
	ignoreDependentServicesEnvs bool,
) error {
	if err := EnsurePathExists(service.AbsolutePath); err != nil {
		fmt.Println("Error ensuring directory:", err)
		return err
	}

	if service.IgnoreEnv {
		fmt.Println(art.RedColor, "Ignoring env file for", service.ServiceName, art.WhiteColor)
		return nil
	}

	envForService, err := buildServiceEnvBody(service, corgiCompose, copyEnvFilePath, ignoreDependentServicesEnvs)
	if err != nil {
		return err
	}

	if err := recordExportsForService(service, envForService); err != nil {
		return err
	}

	pathToEnvFile := GetPathToEnv(service)
	envFileContentString := renderEnvFileContent(pathToEnvFile, envForService, service)
	return writeEnvFile(pathToEnvFile, envFileContentString)
}

func parseEnvVarsIntoMap(envForService string) map[string]string {
	envMap := make(map[string]string)
	lines := strings.Split(envForService, "\n")
	for _, line := range lines {
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := parts[0]
				value := parts[1]
				envMap[key] = value
			}
		}
	}
	return envMap
}

// substituteEnvVarReferences processes an environment variable line for variable references and substitutes them.
func substituteEnvVarReferences(envLine string, envMap map[string]string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(envLine, func(match string) string {
		// Extract the variable name from the match.
		varName := match[2 : len(match)-1] // Remove ${ and }
		if value, exists := envMap[varName]; exists {
			return value
		}
		// If there's no match, return the original placeholder.
		return match
	})
}

func splitStringForEnv(s string) string {
	if strings.Contains(s, "/") {
		return strings.ToUpper(
			strings.Join(strings.Split(s, "/"), "_"),
		)
	}
	if strings.Contains(s, "-") {
		return strings.ToUpper(
			strings.Join(strings.Split(s, "-"), "_"),
		)
	}
	re := regexp.MustCompile(`[^A-Z][^A-Z]*`)
	stringSlice := re.FindAllString(s, -1)

	for i := range stringSlice {
		if i == 0 {
			continue
		}
		characterIndex := strings.Index(s, stringSlice[i])
		stringSlice[i] = string(s[characterIndex-1]) + stringSlice[i]
	}
	return strings.ToUpper(
		strings.Join(stringSlice, "_"),
	)
}

func GetPathToEnv(service Service) string {
	envName := ".env"
	if service.EnvPath != "" {
		service.EnvPath = strings.Replace(
			service.EnvPath,
			service.AbsolutePath,
			"",
			-1,
		)
		if strings.Contains(service.EnvPath, "/") {
			if service.EnvPath[:1] == "." {
				service.EnvPath = service.EnvPath[1:]
			}
			if service.EnvPath[:1] == "/" {
				service.EnvPath = service.EnvPath[1:]
			}
		}
		envName = service.EnvPath
	}

	if len(service.AbsolutePath) <= 1 {
		return envName
	}
	if service.AbsolutePath[len(service.AbsolutePath)-1:] != "/" {
		return service.AbsolutePath + "/" + envName
	} else {
		return service.AbsolutePath + envName
	}
}

func removeFromToIndexes(s []string, from int, to int) []string {
	return append(s[:from], s[to+1:]...)
}

func CreateFileForPath(path string) {
	if path == "" {
		return
	}
	copyEnvFromFileAbsolutePath := fmt.Sprintf(
		"%s/%s",
		CorgiComposePathDir,
		path,
	)
	dirPath := filepath.Dir(
		copyEnvFromFileAbsolutePath,
	)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {

		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			fmt.Printf(
				"Failed to create directory for env file %s, error: %s\n",
				copyEnvFromFileAbsolutePath,
				err,
			)
			return
		}
	}

	_, err := os.Stat(copyEnvFromFileAbsolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			f, err := os.Create(copyEnvFromFileAbsolutePath)
			if err != nil {
				fmt.Println(err)
			}
			defer f.Close()
		}
	}
}
