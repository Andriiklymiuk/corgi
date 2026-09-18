package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var CorgiComposeDefaultName = "corgi-compose.yml"
var DbServicesInConfig = "db_services"
var ServicesInConfig = "services"
var RequiredInConfig = "required"
var InitInConfig = "init"
var StartInConfig = "start"
var BeforeStartInConfig = "beforeStart"
var AfterStartInConfig = "afterStart"
var UseDockerInConfig = "useDocker"
var UseAwsVpnInConfig = "useAwsVpn"
var NameInConfig = "name"
var DescriptionInConfig = "description"

var ServicesItemsFromFlag []string
var DbServicesItemsFromFlag []string

var SkippedServices = map[string]bool{}
var SkippedDbServices = map[string]bool{}

var UnknownComposeFields []string

var DuplicateComposeKeys []string

type DatabaseService struct {
	ServiceName       string                   `yaml:"service_name,omitempty"`
	Driver            string                   `yaml:"driver,omitempty" options:"postgres,mongodb,mysql,mariadb,redis,redis-server,rabbitmq,sqs,s3,dynamodb,kafka,mssql,cassandra,cockroach,clickhouse,scylla,keydb,influxdb,surrealdb,neo4j,dgraph,arangodb,elasticsearch,timescaledb,couchdb,meilisearch,faunadb,yugabytedb,skytable,dragonfly,redict,valkey,postgis,pgvector,localstack,supabase,mailpit,image❌skip"`
	Version           string                   `yaml:"version,omitempty"`
	Host              string                   `yaml:"host,omitempty"`
	User              string                   `yaml:"user,omitempty"`
	Password          string                   `yaml:"password,omitempty"`
	DatabaseName      string                   `yaml:"databaseName,omitempty"`
	Port              int                      `yaml:"port,omitempty"`
	Port2             int                      `yaml:"port2,omitempty"`
	ManualRun         bool                     `yaml:"manualRun,omitempty"`
	SeedFromDbEnvPath string                   `yaml:"seedFromDbEnvPath,omitempty"`
	SeedFromFilePath  string                   `yaml:"seedFromFilePath,omitempty"`
	SeedFromDb        SeedFromDb               `yaml:"seedFromDb,omitempty"`
	Additional        AdditionalDatabaseConfig `yaml:"additional,omitempty"`
	Services          []string                 `yaml:"services,omitempty"`
	Queues            []string                 `yaml:"queues,omitempty"`
	Buckets           []string                 `yaml:"buckets,omitempty"`
	Topics            []string                 `yaml:"topics,omitempty"`
	Subscriptions     []SnsSubscription        `yaml:"subscriptions,omitempty"`
	Secrets           []AwsSecret              `yaml:"secrets,omitempty"`
	Parameters        []SsmParameter           `yaml:"parameters,omitempty"`
	Streams           []string                 `yaml:"streams,omitempty"`
	JWTSecret         string                   `yaml:"jwtSecret,omitempty"`
	AuthUsers         []SupabaseAuthUser       `yaml:"authUsers,omitempty"`
	ConfigTomlPath    string                   `yaml:"configTomlPath,omitempty"`
	StudioPort        int                      `yaml:"studioPort,omitempty"`
	InbucketPort      int                      `yaml:"inbucketPort,omitempty"`
	DbPort            int                      `yaml:"dbPort,omitempty"`
	Image             string                   `yaml:"image,omitempty"`
	ContainerPort     int                      `yaml:"containerPort,omitempty"`
	Environment       []string                 `yaml:"environment,omitempty"`
	Volumes           []string                 `yaml:"volumes,omitempty"`
	Command           []string                 `yaml:"command,omitempty"`
	HealthCheck       string                   `yaml:"healthCheck,omitempty"`

	Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

var KnownDrivers = knownDriversFromTag()

func knownDriversFromTag() []string {
	t := reflect.TypeOf(DatabaseService{})
	f, ok := t.FieldByName("Driver")
	if !ok {
		return nil
	}
	opts := f.Tag.Get("options")
	if opts == "" {
		return nil
	}
	var drivers []string
	for _, d := range strings.Split(opts, ",") {
		d = strings.TrimSuffix(d, "❌skip")
		if d != "" {
			drivers = append(drivers, d)
		}
	}
	return drivers
}

type SnsSubscription struct {
	Topic string `yaml:"topic,omitempty"`
	Queue string `yaml:"queue,omitempty"`
}

type AwsSecret struct {
	Name  string `yaml:"name,omitempty"`
	Value string `yaml:"value,omitempty"`
}

type SupabaseAuthUser struct {
	Email    string                 `yaml:"email,omitempty"`
	Password string                 `yaml:"password,omitempty"`
	Metadata map[string]interface{} `yaml:"metadata,omitempty"`
}

func (u SupabaseAuthUser) MetadataJSON() string {
	if u.Metadata == nil {
		return "{}"
	}
	b, err := json.Marshal(u.Metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

type SsmParameter struct {
	Name  string `yaml:"name,omitempty"`
	Value string `yaml:"value,omitempty"`
	Type  string `yaml:"type,omitempty"`
}

type SeedFromDb struct {
	Host         string `yaml:"host,omitempty"`
	DatabaseName string `yaml:"databaseName,omitempty"`
	User         string `yaml:"user,omitempty"`
	Password     string `yaml:"password,omitempty"`
	Port         int    `yaml:"port,omitempty"`
}

type AdditionalDatabaseConfig struct {
	DefinitionPath string `yaml:"definitionPath,omitempty"`
}

type DependsOnService struct {
	Name        string `yaml:"name,omitempty"`
	EnvAlias    string `yaml:"envAlias,omitempty"`
	Suffix      string `yaml:"suffix,omitempty"`
	Scheme      string `yaml:"scheme,omitempty"`
	ForceUseEnv bool   `yaml:"forceUseEnv,omitempty"`
	Condition   string `yaml:"condition,omitempty" json:"condition,omitempty"`
}

type DependsOnDb struct {
	Name        string `yaml:"name,omitempty"`
	EnvAlias    string `yaml:"envAlias,omitempty"`
	ForceUseEnv bool   `yaml:"forceUseEnv,omitempty"`
	Condition   string `yaml:"condition,omitempty" json:"condition,omitempty"`
}

type Script struct {
	Name                string   `yaml:"name,omitempty"`
	ManualRun           bool     `yaml:"manualRun,omitempty"`
	Commands            []string `yaml:"commands,omitempty"`
	CopyEnvFromFilePath string   `yaml:"copyEnvFromFilePath,omitempty"`
}

type Runner struct {
	Name          string    `yaml:"name,omitempty" options:"docker,"`
	Dockerfile    string    `yaml:"dockerfile,omitempty"`
	Context       string    `yaml:"context,omitempty"`
	Target        string    `yaml:"target,omitempty"`
	Args          BuildArgs `yaml:"args,omitempty"`
	Volumes       []string  `yaml:"volumes,omitempty"`
	ContainerPort int       `yaml:"containerPort,omitempty"`
	Command       string    `yaml:"command,omitempty"`
	ComposeFile   string    `yaml:"composeFile,omitempty"`
	Image         string    `yaml:"image,omitempty"`
	Watch         bool      `yaml:"watch,omitempty"`
}

type Service struct {
	ServiceName            string             `yaml:"service_name,omitempty"`
	Path                   string             `yaml:"path,omitempty"`
	IgnoreEnv              bool               `yaml:"ignore_env,omitempty"`
	ManualRun              bool               `yaml:"manualRun,omitempty"`
	CloneFrom              string             `yaml:"cloneFrom,omitempty"`
	Branch                 string             `yaml:"branch,omitempty"`
	Environment            []string           `yaml:"environment,omitempty"`
	EnvPath                string             `yaml:"envPath,omitempty"`
	CopyEnvFromFilePath    string             `yaml:"copyEnvFromFilePath,omitempty"`
	EnvPlaceholdersToCheck []string           `yaml:"envPlaceholdersToCheck,omitempty"`
	LocalhostNameInEnv     string             `yaml:"localhostNameInEnv,omitempty"`
	Port                   int                `yaml:"port,omitempty"`
	PortAlias              string             `yaml:"portAlias,omitempty"`
	DependsOnServices      []DependsOnService `yaml:"depends_on_services,omitempty"`
	DependsOnDb            []DependsOnDb      `yaml:"depends_on_db,omitempty"`
	WaitForDatabases       *bool              `yaml:"waitForDatabases,omitempty"`
	Exports                []string           `yaml:"exports,omitempty"`
	BeforeStart            BeforeStartSteps   `yaml:"beforeStart,omitempty"`
	Start                  []string           `yaml:"start,omitempty"`
	AfterStart             []string           `yaml:"afterStart,omitempty"`
	RestartPolicy          *RestartPolicy     `yaml:"restartPolicy,omitempty"`
	OpenOnReady            *OpenOnReady       `yaml:"openOnReady,omitempty"`
	Scripts                []Script           `yaml:"scripts,omitempty"`
	InteractiveInput       bool               `yaml:"interactiveInput,omitempty"`
	AutoSourceEnv          *bool              `yaml:"autoSourceEnv,omitempty"`

	Runner Runner `yaml:"runner,omitempty"`

	Tunnel *TunnelConfig `yaml:"tunnel,omitempty"`

	HealthCheck string `yaml:"healthCheck,omitempty"`

	Warmup *WarmupCheck `yaml:"warmup,omitempty"`

	Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"`

	AbsolutePath string

	CacheScope string `json:"-"`

	ResolvedDockerSource DockerSource `yaml:"-" json:"-"`
}

type TunnelConfig struct {
	Provider string `yaml:"provider,omitempty"`
	Hostname string `yaml:"hostname,omitempty"`
	Name     string `yaml:"name,omitempty"`
}

type Required struct {
	Name     string   `yaml:"name,omitempty"`
	Why      []string `yaml:"why,omitempty"`
	Install  []string `yaml:"install,omitempty"`
	Optional bool     `yaml:"optional,omitempty"`
	CheckCmd string   `yaml:"checkCmd,omitempty"`
	SkipInCi bool     `yaml:"skipInCi,omitempty"`
}

func ActiveRequired(required []Required) []Required {
	if !CIMode {
		return required
	}
	active := make([]Required, 0, len(required))
	for _, r := range required {
		if r.SkipInCi {
			Info("required:", r.Name, "skipped (skipInCi, CI detected)")
			continue
		}
		active = append(active, r)
	}
	return active
}

type EnvTier struct {
	Dir        string `yaml:"dir,omitempty"`
	DbServices string `yaml:"dbServices,omitempty"`
	Confirm    bool   `yaml:"confirm,omitempty"`
}

type CorgiCompose struct {
	DatabaseServices []DatabaseService
	Services         []Service
	Required         []Required
	EnvTiers         map[string]EnvTier `yaml:"envTiers,omitempty"`
	Init             []string           `yaml:"init,omitempty"`
	BeforeStart      []string           `yaml:"beforeStart,omitempty"`
	Start            []string           `yaml:"start,omitempty"`
	AfterStart       []string           `yaml:"afterStart,omitempty"`

	UseDocker       bool `yaml:"useDocker,omitempty"`
	UseAwsVpn       bool `yaml:"useAwsVpn,omitempty"`
	ScopeContainers bool `yaml:"scopeContainers,omitempty"`

	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`

	E2E *E2ESuite `yaml:"e2e,omitempty"`
}

type E2ESuite struct {
	Workdir   string   `yaml:"workdir,omitempty"`
	Install   string   `yaml:"install,omitempty"`
	Run       string   `yaml:"run,omitempty"`
	Artifacts []string `yaml:"artifacts,omitempty"`
}

type CorgiComposeYaml struct {
	DatabaseServices map[string]DatabaseService `yaml:"db_services"`
	Services         map[string]Service         `yaml:"services"`
	Required         map[string]Required        `yaml:"required"`
	EnvTiers         map[string]EnvTier         `yaml:"envTiers,omitempty"`
	Init             []string                   `yaml:"init,omitempty"`
	BeforeStart      []string                   `yaml:"beforeStart,omitempty"`
	Start            []string                   `yaml:"start,omitempty"`
	AfterStart       []string                   `yaml:"afterStart,omitempty"`

	UseDocker bool `yaml:"useDocker,omitempty"`
	UseAwsVpn bool `yaml:"useAwsVpn,omitempty"`

	ScopeContainers bool `yaml:"scopeContainers,omitempty"`

	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`

	E2E *E2ESuite `yaml:"e2e,omitempty"`
}

var CorgiComposePath string
var CorgiComposePathDir string
var CorgiComposeFileContent *CorgiCompose

func GetCorgiServices(cobra *cobra.Command) (*CorgiCompose, error) {
	pathToCorgiComposeFile, corgiYaml, err := loadCorgiComposeFile(cobra)
	if err != nil {
		return nil, err
	}

	describeFlag, err := cobra.Root().Flags().GetBool("describe")
	if err != nil {
		return nil, err
	}

	corgi := buildBaseCorgi(corgiYaml)

	if err := applyEnvTier(&corgi); err != nil {
		return nil, err
	}
	SetContainerScope(&corgi)

	applyWithDeps(corgiYaml.Services)

	if err := SaveExecPath(corgi.Name, corgi.Description, pathToCorgiComposeFile); err != nil {
		Info("failed to save corgi-compose file path: ", err)
	}

	dbServices, err := parseDatabaseServices(corgiYaml.DatabaseServices, describeFlag)
	if err != nil {
		return nil, err
	}
	corgi.DatabaseServices = dbServices

	corgi.Services = parseServices(corgiYaml.Services, describeFlag)
	corgi.Required = parseRequired(corgiYaml.Required, describeFlag)
	corgi.E2E = corgiYaml.E2E

	if err := applyServiceDirOverrides(cobra, &corgi); err != nil {
		return nil, err
	}

	if err := ApplyIsolationLease(&corgi, IsolateLease); err != nil {
		return nil, err
	}

	CorgiComposeFileContent = &corgi
	return &corgi, nil
}

func loadCorgiComposeFile(cobra *cobra.Command) (string, CorgiComposeYaml, error) {
	pathToCorgiComposeFile, err := determineCorgiComposePath(cobra)
	if err != nil {
		return "", CorgiComposeYaml{}, err
	}

	pathToCorgiComposeFile, err = filepath.Abs(pathToCorgiComposeFile)
	if err != nil {
		return "", CorgiComposeYaml{}, fmt.Errorf("couldn't get absolute path for %s: %v", pathToCorgiComposeFile, err)
	}

	Info("Using corgi-compose file:", pathToCorgiComposeFile)
	CorgiComposePath = pathToCorgiComposeFile
	CorgiComposePathDir = filepath.Dir(pathToCorgiComposeFile)

	if !SkipCorgiServicesMigration {
		if moved, err := MigrateCorgiServices(CorgiComposePathDir); moved {
			Info("moved corgi_services into .corgi/corgi_services")
		} else if err != nil {
			Info("corgi_services move skipped: ", err)
		}
	}

	file, err := os.ReadFile(pathToCorgiComposeFile)
	if err != nil {
		return "", CorgiComposeYaml{}, fmt.Errorf("couldn't read %s", pathToCorgiComposeFile)
	}

	dotenv, err := LoadDotEnv(filepath.Join(CorgiComposePathDir, ".env"))
	if err != nil {
		return "", CorgiComposeYaml{}, fmt.Errorf("couldn't read .env next to %s: %v", pathToCorgiComposeFile, err)
	}
	file, _ = InterpolateTolerant(file, EnvThenDotEnv(dotenv))

	var corgiYaml CorgiComposeYaml
	UnknownComposeFields = nil
	DuplicateComposeKeys = nil
	dec := yaml.NewDecoder(bytes.NewReader(file))
	dec.KnownFields(true)
	if err := dec.Decode(&corgiYaml); err != nil {
		if fields := unknownFieldsFromYAMLError(err); len(fields) > 0 {
			UnknownComposeFields = fields
			corgiYaml = CorgiComposeYaml{}
			if err2 := yaml.Unmarshal(file, &corgiYaml); err2 != nil {
				return "", CorgiComposeYaml{}, fmt.Errorf("couldn't unmarshal file %s: %v", pathToCorgiComposeFile, err2)
			}
		} else {
			return "", CorgiComposeYaml{}, fmt.Errorf("couldn't unmarshal file %s: %v", pathToCorgiComposeFile, err)
		}
	}
	DuplicateComposeKeys = detectDuplicateComposeKeys(file)
	return pathToCorgiComposeFile, corgiYaml, nil
}

func detectDuplicateComposeKeys(file []byte) []string {
	var root yaml.Node
	if err := yaml.Unmarshal(file, &root); err != nil || len(root.Content) == 0 {
		return nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	var dups []string
	for i := 0; i+1 < len(doc.Content); i += 2 {
		section := doc.Content[i].Value
		if section != "services" && section != "db_services" && section != "required" {
			continue
		}
		dups = append(dups, duplicateKeysInSection(section, doc.Content[i+1])...)
	}
	return dups
}

func duplicateKeysInSection(section string, m *yaml.Node) []string {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	var dups []string
	seen := map[string]bool{}
	for j := 0; j+1 < len(m.Content); j += 2 {
		key := m.Content[j].Value
		if seen[key] {
			dups = append(dups, fmt.Sprintf("%s.%s", section, key))
		}
		seen[key] = true
	}
	return dups
}

func unknownFieldsFromYAMLError(err error) []string {
	if err == nil {
		return nil
	}
	var fields []string
	for _, line := range strings.Split(err.Error(), "\n") {
		const marker = "field "
		i := strings.Index(line, marker)
		if i < 0 || !strings.Contains(line, "not found in type") {
			continue
		}
		rest := line[i+len(marker):]
		name := strings.TrimSpace(strings.SplitN(rest, " ", 2)[0])
		if name != "" {
			fields = append(fields, name)
		}
	}
	return fields
}

func buildBaseCorgi(y CorgiComposeYaml) CorgiCompose {
	return CorgiCompose{
		Init:            y.Init,
		BeforeStart:     y.BeforeStart,
		Start:           y.Start,
		AfterStart:      y.AfterStart,
		UseDocker:       y.UseDocker,
		UseAwsVpn:       y.UseAwsVpn,
		ScopeContainers: y.ScopeContainers,
		Name:            y.Name,
		Description:     y.Description,
		EnvTiers:        y.EnvTiers,
	}
}

func parseDatabaseServices(dbServicesData map[string]DatabaseService, describeFlag bool) ([]DatabaseService, error) {
	SkippedDbServices = map[string]bool{}
	if len(dbServicesData) == 0 || !servicesCanBeAdded(DbServicesItemsFromFlag) {
		for indexName := range dbServicesData {
			SkippedDbServices[indexName] = true
		}
		Info("no db_services provided")
		return nil, nil
	}
	var dbServices []DatabaseService
	for indexName, db := range dbServicesData {
		if !IsServiceIncludedInFlag(DbServicesItemsFromFlag, indexName) {
			SkippedDbServices[indexName] = true
			continue
		}
		dbToAdd, err := buildDatabaseService(indexName, db)
		if err != nil {
			return nil, err
		}
		dbServices = append(dbServices, dbToAdd)
		if describeFlag {
			describeServiceInfo(dbToAdd)
		}
	}
	return dbServices, nil
}

func buildDatabaseService(indexName string, db DatabaseService) (DatabaseService, error) {
	seedFromDb := mergeSeedFromDb(db)
	driver := db.Driver
	if driver == "" {
		driver = "postgres"
	}
	host := db.Host
	if host == "" {
		host = "localhost"
	}

	additional, finalUser, finalPassword := ProcessAdditionalDatabaseConfig(db, indexName)

	services := db.Services
	if driver == "localstack" {
		services = autoInjectLocalstackServices(services, db)
		if err := validateLocalstackConfig(indexName, db); err != nil {
			return DatabaseService{}, err
		}
	}

	built := db
	built.ServiceName = indexName
	built.Driver = driver
	built.Host = host
	built.User = finalUser
	built.Password = finalPassword
	built.SeedFromDb = seedFromDb
	built.Additional = additional
	built.Services = services
	return built, nil
}

func mergeSeedFromDb(db DatabaseService) SeedFromDb {
	var seedFromDb SeedFromDb
	if db.SeedFromDbEnvPath != "" {
		seedFromDb = getDbSourceFromPath(db.SeedFromDbEnvPath)
	}
	if (seedFromDb == SeedFromDb{}) {
		return db.SeedFromDb
	}
	if db.SeedFromDb.Host != "" {
		seedFromDb.Host = db.SeedFromDb.Host
	}
	if db.SeedFromDb.DatabaseName != "" {
		seedFromDb.DatabaseName = db.SeedFromDb.DatabaseName
	}
	if db.SeedFromDb.User != "" {
		seedFromDb.User = db.SeedFromDb.User
	}
	if db.SeedFromDb.Password != "" {
		seedFromDb.Password = db.SeedFromDb.Password
	}
	if db.SeedFromDb.Port != 0 {
		seedFromDb.Port = db.SeedFromDb.Port
	}
	return seedFromDb
}

func parseServices(servicesData map[string]Service, describeFlag bool) []Service {
	SkippedServices = map[string]bool{}
	if len(servicesData) == 0 || !servicesCanBeAdded(ServicesItemsFromFlag) {
		for indexName := range servicesData {
			SkippedServices[indexName] = true
		}
		Info("no services provided")
		return nil
	}
	var services []Service
	for indexName, service := range servicesData {
		if !IsServiceIncludedInFlag(ServicesItemsFromFlag, indexName) {
			SkippedServices[indexName] = true
			continue
		}
		serviceToAdd := buildService(indexName, service)
		services = append(services, serviceToAdd)
		if describeFlag {
			describeServiceInfo(serviceToAdd)
		}
	}
	return services
}

func buildService(indexName string, service Service) Service {
	resolveServicePathFromCloneFrom(&service)
	normalizeServicePath(&service)

	built := service
	built.ServiceName = indexName
	built.AbsolutePath = computeAbsolutePath(service.Path)
	resolveDockerExposedPort(&built)
	return built
}

func resolveDockerExposedPort(service *Service) {
	if service.Runner.Name != "docker" || service.Port != 0 {
		return
	}
	exposedPort, _ := GetExposedPortFromDockerfile(*service)
	if exposedPort == "" {
		return
	}
	port, err := strconv.Atoi(exposedPort)
	if err != nil {
		return
	}
	service.Port = port
}

func resolveServicePathFromCloneFrom(service *Service) {
	if service.Path != "" || service.CloneFrom == "" {
		return
	}
	if !strings.HasSuffix(service.CloneFrom, ".git") {
		return
	}
	splitURL := strings.Split(service.CloneFrom, "/")
	repoName := strings.TrimSuffix(splitURL[len(splitURL)-1], ".git")
	service.Path = "./" + repoName
}

func normalizeServicePath(service *Service) {
	if !strings.HasPrefix(service.Path, "./") && service.Path != "" {
		service.Path = "./" + service.Path
	}
	if service.Path == "." {
		service.Path = ""
	}
}

func computeAbsolutePath(path string) string {
	if strings.HasPrefix(path, "./") {
		return strings.Replace(path, "./", CorgiComposePathDir+"/", 1)
	}
	return CorgiComposePathDir + "/" + path
}

func ServiceRepoDir(path string) string { return computeAbsolutePath(path) }

func JoinUnderComposeDir(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes the compose directory", rel)
	}
	joined := filepath.Clean(filepath.Join(CorgiComposePathDir, rel))
	base := filepath.Clean(CorgiComposePathDir)
	if joined != base && !strings.HasPrefix(joined, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the compose directory", rel)
	}
	return joined, nil
}

func overrideServiceDirs(corgi *CorgiCompose, pairs []string) error {
	if len(pairs) == 0 {
		return nil
	}
	byName := map[string]*Service{}
	for i := range corgi.Services {
		byName[corgi.Services[i].ServiceName] = &corgi.Services[i]
	}
	for _, pair := range pairs {
		name, dir, err := cutServicePair(pair)
		if err != nil {
			return fmt.Errorf("--service-dir %v", err)
		}
		svc, found := byName[name]
		if !found {
			return fmt.Errorf("--service-dir: no service named %q in corgi-compose.yml", name)
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("--service-dir %s: %v", name, err)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return fmt.Errorf("--service-dir %s: %q is not an existing directory", name, abs)
		}
		Info("service-dir override:", name, "→", abs)
		pointServiceAt(svc, abs)
	}
	return nil
}

func applyServiceDirOverrides(cmd *cobra.Command, corgi *CorgiCompose) error {
	pairs, err := cmd.Flags().GetStringArray("service-dir")
	if err != nil {
		return nil
	}
	return overrideServiceDirs(corgi, pairs)
}

func parseRequired(requiredData map[string]Required, describeFlag bool) []Required {
	if len(requiredData) == 0 {
		Info("no required instructions provided in file.")
		Info("Tip: It is useful to provide required to showcase what is used and how to install it")
		Info()
		return nil
	}
	var requiredInstructions []Required
	for indexName, required := range requiredData {
		requiredToAdd := required
		requiredToAdd.Name = indexName
		requiredInstructions = append(requiredInstructions, requiredToAdd)
		if describeFlag {
			describeServiceInfo(requiredToAdd)
		}
	}
	return requiredInstructions
}

func GetDbServiceByName(databaseServiceName string, databaseServices []DatabaseService) (DatabaseService, error) {
	for _, db := range databaseServices {
		if db.ServiceName == databaseServiceName {
			return db, nil
		}
	}
	return DatabaseService{}, fmt.Errorf("db_service %s is not found", databaseServiceName)
}

func CleanFromScratch(cmd *cobra.Command, corgi CorgiCompose) {
	isFromScratch, err := cmd.Root().Flags().GetBool("fromScratch")
	if err != nil {
		fmt.Println(err)
		return
	}
	if !isFromScratch {
		return
	}
	if len(corgi.DatabaseServices) != 0 {
		ExecuteForEachService("remove")
	}
	CleanCorgiServicesFolder()
}

func CleanCorgiServicesFolder() {
	if skipped, _ := CleanCorgiWorktrees(false); len(skipped) > 0 {
		Infof("kept %d worktree(s) with uncommitted changes (corgi worktree prune --force to drop):\n", len(skipped))
		for _, d := range skipped {
			Infof("  %s\n", d)
		}
	}
	root := CorgiServicesDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fmt.Println("couldn't read corgi_services folder: ", err)
		return
	}
	for _, e := range entries {
		if err := removeExceptSnapshots(filepath.Join(root, e.Name())); err != nil {
			fmt.Println("couldn't clean", e.Name(), ":", err)
		}
	}
	if remaining, err := os.ReadDir(root); err == nil && len(remaining) == 0 {
		_ = os.Remove(root)
	}
	fmt.Println("🗑️ Cleaned up corgi_services (snapshots preserved)")
}

// Lstat, not Stat: a symlink out of corgi_services is removed as a link, never followed.
func removeExceptSnapshots(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return os.Remove(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	keptAny, err := removeChildrenExceptSnapshots(path, entries)
	if err != nil {
		return err
	}
	if keptAny {
		return nil
	}
	return os.Remove(path)
}

func removeChildrenExceptSnapshots(path string, entries []os.DirEntry) (bool, error) {
	keptAny := false
	for _, e := range entries {
		if e.IsDir() && e.Name() == "snapshots" {
			keptAny = true
			continue
		}
		if err := removeExceptSnapshots(filepath.Join(path, e.Name())); err != nil {
			return false, err
		}
		if e.IsDir() {
			if _, statErr := os.Lstat(filepath.Join(path, e.Name())); statErr == nil {
				keptAny = true
			}
		}
	}
	return keptAny, nil
}

func CleanSnapshots() {
	root := DbServicesIn(CorgiComposePathDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			_ = os.RemoveAll(filepath.Join(root, e.Name(), "snapshots"))
		}
	}
	fmt.Println("🗑️ Cleaned up db snapshots")
}

func getDbSourceFromPath(path string) SeedFromDb {
	var seedFromDb SeedFromDb
	for _, envLine := range GetFileContent(path) {
		line := strings.TrimSpace(envLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.ToUpper(strings.TrimSpace(parts[0])), parts[1]
		switch key {
		case "DB_HOST":
			seedFromDb.Host = value
		case "DB_NAME":
			seedFromDb.DatabaseName = value
		case "DB_PASSWORD":
			seedFromDb.Password = value
		case "DB_USER":
			seedFromDb.User = value
		case "DB_PORT":
			intVar, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				Info(err)
				continue
			}
			seedFromDb.Port = intVar
		}
	}
	return seedFromDb
}

func describeServiceInfo(service any) {
	data, err := json.MarshalIndent(service, "", "\t")
	if err != nil {
		Info(err)
	} else {
		Info(string(data))
	}
}

func servicesCanBeAdded(services []string) bool {
	for _, service := range services {
		if service == "none" {
			return false
		}
	}
	return true
}

func IsServiceIncludedInFlag(services []string, serviceName string) bool {
	if len(services) == 0 {
		return true
	}
	var isIncluded bool
	for _, service := range services {
		if service == serviceName {
			isIncluded = true
		}
	}
	return isIncluded
}

func getCorgiConfigFilePath() (string, error) {
	defaultCorgiConfigName := CorgiComposeDefaultName
	corgiComposeExists, err := CheckIfFileExistsInDirectory(
		".",
		defaultCorgiConfigName,
	)
	if err != nil {
		return "", err
	}
	if corgiComposeExists {
		return defaultCorgiConfigName, nil
	}

	parentConfig := filepath.Join("..", defaultCorgiConfigName)
	parentExists, err := CheckIfFileExistsInDirectory("..", defaultCorgiConfigName)
	if err == nil && parentExists {
		Info("No corgi-compose.yml here; using the one one level up:", parentConfig)
		return parentConfig, nil
	}

	chosenCorgiPath, err := getCorgiConfigFromAlert()
	if err != nil || chosenCorgiPath == "" {
		return "", err
	}
	return chosenCorgiPath, nil
}

func getCorgiConfigFromAlert() (string, error) {
	files, err := findCorgiYamlFiles()
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no corgi-compose.yml found in this directory or one level up; run from a corgi workspace or pass -f <path>")
	}

	if NonInteractive {
		return "", fmt.Errorf("no corgi-compose.yml found and no terminal to pick one; pass -f <path> or run from a directory containing corgi-compose.yml")
	}

	file, err := PickItemFromListPrompt(
		"Select corgi config file to use",
		files,
		"none",
	)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	return file, nil
}

func findCorgiYamlFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(".", func(path string, directory fs.DirEntry, err error) error {
		if err != nil {
			fmt.Println(err)
			return nil
		}
		if directory.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml" {
			return nil
		}
		if !strings.Contains(directory.Name(), "corgi") {
			return nil
		}

		files = append(files, path)

		return nil
	})
	return files, err
}

func determineCorgiComposePath(cobraCmd *cobra.Command) (string, error) {
	globalFlag, err := cobraCmd.Flags().GetBool("global")
	if err != nil {
		return "", fmt.Errorf("error checking global flag: %v", err)
	}
	if globalFlag {
		return resolveGlobalPath()
	}

	filenameFlag, err := cobraCmd.Root().Flags().GetString("filename")
	if err != nil {
		return "", err
	}

	if path, handled, err := resolveTemplatePath(cobraCmd, filenameFlag); handled {
		return path, err
	}

	if filenameFlag != "" {
		return filenameFlag, nil
	}
	return getCorgiConfigFilePath()
}

func resolveGlobalPath() (string, error) {
	globalPath, err := selectGlobalExecPath()
	if err != nil || globalPath == "" {
		return "", fmt.Errorf("no global corgi path selected")
	}
	return globalPath, nil
}

func resolveTemplatePath(cobraCmd *cobra.Command, filenameFlag string) (string, bool, error) {
	fromTemplateFlag, err := cobraCmd.Root().Flags().GetString("fromTemplate")
	if err != nil {
		return "", true, err
	}
	if fromTemplateFlag != "" {
		privateTokenFlag, err := cobraCmd.Root().Flags().GetString("privateToken")
		if err != nil {
			return "", true, err
		}
		downloaded, err := DownloadFileFromURL(fromTemplateFlag, filenameFlag, privateTokenFlag)
		if err != nil {
			return "", true, fmt.Errorf("error downloading template: %v", err)
		}
		return downloaded, true, nil
	}

	templateNameFlag, err := cobraCmd.Root().Flags().GetString("fromTemplateName")
	if err != nil {
		return "", true, err
	}
	if templateNameFlag != "" {
		path, err := DownloadExample(cobraCmd, templateNameFlag, filenameFlag)
		return path, true, err
	}

	showExampleList, err := cobraCmd.Root().Flags().GetBool("exampleList")
	if err != nil {
		return "", true, err
	}
	if showExampleList {
		selectedPath, err := PickItemFromListPrompt(
			"Select corgi template to use",
			ExtractExamplePaths(ExampleProjects),
			"none",
		)
		if err != nil {
			return "", true, fmt.Errorf("error selecting path: %v", err)
		}
		path, err := DownloadExample(cobraCmd, selectedPath, filenameFlag)
		return path, true, err
	}

	return "", false, nil
}

func selectGlobalExecPath() (string, error) {
	executionPaths, err := ListExecPaths()
	if err != nil {
		return "", fmt.Errorf("error retrieving executed paths: %v", err)
	}
	if len(executionPaths) == 0 {
		return "", fmt.Errorf("no global corgi paths found")
	}

	displayPaths := make([]string, len(executionPaths))
	for i, executionPath := range executionPaths {
		displayString := ""
		if executionPath.Name != "" {
			displayString = fmt.Sprintf("%s%s%s, ", art.BlueColor, executionPath.Name, art.WhiteColor)
		}
		displayString += executionPath.Path
		displayPaths[i] = displayString
	}

	selectedDisplay, err := PickItemFromListPrompt(
		"Select a path from global corgi paths",
		displayPaths,
		"none",
	)
	if err != nil {
		return "", fmt.Errorf("error selecting path: %v", err)
	}
	fmt.Printf("Selected path: %s\n", selectedDisplay)

	for _, executionPath := range executionPaths {
		formattedDisplay := executionPath.Path
		if executionPath.Name != "" {
			formattedDisplay = fmt.Sprintf("%s%s%s, %s", art.BlueColor, executionPath.Name, art.WhiteColor, executionPath.Path)
		}
		if selectedDisplay == formattedDisplay {
			return executionPath.Path, nil
		}
	}

	return "", fmt.Errorf("selected path not found in the list")
}

func toMap(slice interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	val := reflect.ValueOf(slice)
	for i := 0; i < val.Len(); i++ {
		item := val.Index(i).Interface()
		key := reflect.ValueOf(item).FieldByName("ServiceName").String()
		result[key] = item
	}
	return result
}

func CompareCorgiFiles(c1, c2 *CorgiCompose) bool {
	if c1.Name != c2.Name ||
		c1.Description != c2.Description ||
		c1.UseDocker != c2.UseDocker ||
		c1.UseAwsVpn != c2.UseAwsVpn {
		return false
	}

	if !reflect.DeepEqual(toMap(c1.Services), toMap(c2.Services)) {
		return false
	}

	if !reflect.DeepEqual(toMap(c1.DatabaseServices), toMap(c2.DatabaseServices)) {
		return false
	}

	if !reflect.DeepEqual(c1.Required, c2.Required) {
		return false
	}

	if !reflect.DeepEqual(c1.Init, c2.Init) ||
		!reflect.DeepEqual(c1.BeforeStart, c2.BeforeStart) ||
		!reflect.DeepEqual(c1.Start, c2.Start) ||
		!reflect.DeepEqual(c1.AfterStart, c2.AfterStart) {
		return false
	}

	return true
}

func (s Service) WaitsForDatabases() bool {
	return s.WaitForDatabases == nil || *s.WaitForDatabases
}

func AnyServiceStartsWithDatabases(corgi *CorgiCompose) bool {
	for _, s := range corgi.Services {
		if !s.WaitsForDatabases() {
			return true
		}
	}
	return false
}
