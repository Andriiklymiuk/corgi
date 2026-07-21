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

var RootDbServicesFolder = "corgi_services/db_services"
var RootServicesFolder = "corgi_services/services"
var ServicesItemsFromFlag []string
var DbServicesItemsFromFlag []string

// Names declared in corgi-compose.yml but filtered out by --services /
// --dbServices. Env gen uses these to drop cross-service refs to producers
// that aren't running, instead of erroring.
var SkippedServices = map[string]bool{}
var SkippedDbServices = map[string]bool{}

// UnknownComposeFields holds keys the strict YAML decoder did not recognize on
// the most recent load (likely typos). Surfaced warn-first via ValidateCompose;
// reset on every load. Warn-now/error-later: a future release may upgrade these
// to hard errors once configs are clean.
var UnknownComposeFields []string

// DuplicateComposeKeys lists keys that appeared more than once within a single
// services/db_services/required map on the most recent load. YAML silently
// keeps only the last; ValidateCompose reports these. Reset per load.
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
	// localstack driver:
	Services      []string          `yaml:"services,omitempty"`      // e.g. [sqs, s3, sns, secretsmanager, ssm, kinesis]
	Queues        []string          `yaml:"queues,omitempty"`        // SQS queues to auto-create
	Buckets       []string          `yaml:"buckets,omitempty"`       // S3 buckets to auto-create
	Topics        []string          `yaml:"topics,omitempty"`        // SNS topics to auto-create
	Subscriptions []SnsSubscription `yaml:"subscriptions,omitempty"` // SNS topic -> SQS queue wiring
	Secrets       []AwsSecret       `yaml:"secrets,omitempty"`       // Secrets Manager entries
	Parameters    []SsmParameter    `yaml:"parameters,omitempty"`    // SSM Parameter Store entries
	Streams       []string          `yaml:"streams,omitempty"`       // Kinesis streams (1 shard each)
	// supabase driver:
	JWTSecret      string             `yaml:"jwtSecret,omitempty"`      // Override stock JWT secret. If set, driver re-signs ANON_KEY / SERVICE_ROLE_KEY with this secret to match what `supabase status` will report.
	AuthUsers      []SupabaseAuthUser `yaml:"authUsers,omitempty"`      // Auth users to seed via supabase admin API on `up`.
	ConfigTomlPath string             `yaml:"configTomlPath,omitempty"` // Optional path (relative to corgi-compose.yml) to a config.toml that corgi copies to corgi_services/db_services/<svc>/supabase/config.toml on each `corgi init`. If unset, supabase init runs at first `corgi up` and config.toml lives at <projectRoot>/supabase/config.toml.
	StudioPort     int                `yaml:"studioPort,omitempty"`     // supabase only. Patches [studio].port in config.toml on each up. Compose wins over file.
	InbucketPort   int                `yaml:"inbucketPort,omitempty"`   // supabase only. Patches [inbucket].port in config.toml on each up. Compose wins over file.
	DbPort         int                `yaml:"dbPort,omitempty"`         // supabase only. Patches [db].port in config.toml on each up. Compose wins over file.
	// image driver:
	Image         string   `yaml:"image,omitempty"`         // image driver only. Docker image reference (e.g. "gotenberg/gotenberg:8").
	ContainerPort int      `yaml:"containerPort,omitempty"` // image driver only. Container's internal port. Defaults to `port:` if unset. Used in docker-compose `<port>:<containerPort>` mapping.
	Environment   []string `yaml:"environment,omitempty"`   // image driver only. Docker-compose environment entries (e.g. ["MEILI_MASTER_KEY=secret"]).
	Volumes       []string `yaml:"volumes,omitempty"`       // image driver only. Docker-compose volume mappings (e.g. ["./data:/app/data"]).
	Command       []string `yaml:"command,omitempty"`       // image driver only. Override container entrypoint args (e.g. ["--collector.zipkin.host-port=9411"]).
	// Optional HTTP path for `corgi status`. If set, status check does GET
	// http://localhost:<port><HealthCheck> and accepts any non-5xx as healthy.
	// If unset, status falls back to a TCP connect on the port.
	HealthCheck string `yaml:"healthCheck,omitempty"`

	// Run profiles this db_service belongs to; empty runs only when no --profile.
	Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// KnownDrivers is the set of valid db_services.driver values, derived from the
// Driver field's `options:` tag (its trailing "❌skip" sentinel stripped).
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

// SupabaseAuthUser is one entry in db_services.<name>.authUsers for the
// supabase driver. `metadata` is a yaml map serialized to JSON for
// user_metadata. Nil omits user_metadata.
type SupabaseAuthUser struct {
	Email    string                 `yaml:"email,omitempty"`
	Password string                 `yaml:"password,omitempty"`
	Metadata map[string]interface{} `yaml:"metadata,omitempty"`
}

// MetadataJSON serializes Metadata to a compact JSON object string for the
// admin API. Returns "{}" if Metadata is nil. Errors marshaling are
// extremely unlikely (yaml decoder produces JSON-friendly types) but on
// failure we fall back to "{}" to keep the bootstrap script idempotent.
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
	Type  string `yaml:"type,omitempty"` // String | StringList | SecureString
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
	// Condition gates startup: "ready" waits for the readiness probe, "started"
	// waits only until corgi launched it. Empty = no gating unless --gate-deps.
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
}

type DependsOnDb struct {
	Name        string `yaml:"name,omitempty"`
	EnvAlias    string `yaml:"envAlias,omitempty"`
	ForceUseEnv bool   `yaml:"forceUseEnv,omitempty"`
	// Condition opts this edge into startup gating. See DependsOnService.
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
}

type Script struct {
	Name                string   `yaml:"name,omitempty"`
	ManualRun           bool     `yaml:"manualRun,omitempty"`
	Commands            []string `yaml:"commands,omitempty"`
	CopyEnvFromFilePath string   `yaml:"copyEnvFromFilePath,omitempty"`
}

type Runner struct {
	Name string `yaml:"name,omitempty" options:"docker,"`
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
	Exports                []string           `yaml:"exports,omitempty"`
	BeforeStart            BeforeStartSteps   `yaml:"beforeStart,omitempty"`
	Start                  []string           `yaml:"start,omitempty"`
	AfterStart             []string           `yaml:"afterStart,omitempty"`
	RestartPolicy          *RestartPolicy     `yaml:"restartPolicy,omitempty"`
	OpenOnReady            *OpenOnReady       `yaml:"openOnReady,omitempty"`
	Scripts                []Script           `yaml:"scripts,omitempty"`
	InteractiveInput       bool               `yaml:"interactiveInput,omitempty"`
	// AutoSourceEnv toggles the `set -a; . <envFile>; set +a` prefix corgi
	// adds to start/beforeStart/afterStart commands. nil/true = on (default),
	// false = off. Off avoids exporting every var to subprocesses (e.g. when
	// a beforeStart `npm install` would otherwise leak secrets to postinstall
	// scripts).
	AutoSourceEnv *bool `yaml:"autoSourceEnv,omitempty"`

	Runner Runner `yaml:"runner,omitempty"`

	// Tunnel declares an optional public HTTPS tunnel managed by `corgi
	// tunnel`. When set + hostname resolves non-empty, corgi runs the
	// provider in named/static mode (stable URL across restarts).
	// Otherwise (block missing or hostname empty) corgi falls back to the
	// provider's default behavior (cloudflared Quick Tunnels, etc.).
	Tunnel *TunnelConfig `yaml:"tunnel,omitempty"`

	// Optional HTTP path for `corgi status`. If set, status check does GET
	// http://localhost:<port><HealthCheck> and accepts any non-5xx as healthy.
	// If unset, status falls back to a TCP connect on the port.
	HealthCheck string `yaml:"healthCheck,omitempty"`

	// Warmup is a single expensive request made once the service is live,
	// before it counts as ready. A polled healthCheck has to be cheap: a dev
	// server that compiles on demand does the work again for every probe, so
	// polling one starves the machine and the stack never settles. Put the
	// expensive check here instead — it runs once and waits.
	Warmup *WarmupCheck `yaml:"warmup,omitempty"`

	// Run profiles this service belongs to; empty runs only when no --profile.
	Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"`

	AbsolutePath string

	// CacheScope isolates beforeStart step-cache markers when the service runs
	// from a relocated dir. Empty for the declared checkout.
	CacheScope string `json:"-"`
}

// TunnelConfig describes a stable public HTTPS tunnel for one service.
// Hostname / Name support `${VAR}` substitution from shell env first, then
// from the service's env file (copyEnvFromFilePath). Missing required vars
// produce a strict error at `corgi tunnel` time — no silent fallback.
type TunnelConfig struct {
	Provider string `yaml:"provider,omitempty"` // cloudflared (default) | ngrok | localtunnel.
	Hostname string `yaml:"hostname,omitempty"` // public URL (e.g. api-andrii.dev.example.com). Required when block present.
	Name     string `yaml:"name,omitempty"`     // cloudflared tunnel name (must exist via `cloudflared tunnel create`). Ignored for ngrok.
}

type Required struct {
	Name     string   `yaml:"name,omitempty"`
	Why      []string `yaml:"why,omitempty"`
	Install  []string `yaml:"install,omitempty"`
	Optional bool     `yaml:"optional,omitempty"`
	CheckCmd string   `yaml:"checkCmd,omitempty"`
	// SkipInCi drops this tool from preflight when corgi detects CI.
	SkipInCi bool `yaml:"skipInCi,omitempty"`
}

// ActiveRequired filters out tools declared skipInCi when running in CI.
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

// Named run-settings bundle selected by --tier.
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
	// cannot combine from one common struct (yaml serialization), so have to repeat
	Init        []string `yaml:"init,omitempty"`
	BeforeStart []string `yaml:"beforeStart,omitempty"`
	Start       []string `yaml:"start,omitempty"`
	AfterStart  []string `yaml:"afterStart,omitempty"`

	UseDocker bool `yaml:"useDocker,omitempty"`
	UseAwsVpn bool `yaml:"useAwsVpn,omitempty"`

	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type CorgiComposeYaml struct {
	DatabaseServices map[string]DatabaseService `yaml:"db_services"`
	Services         map[string]Service         `yaml:"services"`
	Required         map[string]Required        `yaml:"required"`
	EnvTiers         map[string]EnvTier         `yaml:"envTiers,omitempty"`
	// cannot combine from one common struct (yaml serialization), so have to repeat
	Init        []string `yaml:"init,omitempty"`
	BeforeStart []string `yaml:"beforeStart,omitempty"`
	Start       []string `yaml:"start,omitempty"`
	AfterStart  []string `yaml:"afterStart,omitempty"`

	UseDocker bool `yaml:"useDocker,omitempty"`
	UseAwsVpn bool `yaml:"useAwsVpn,omitempty"`

	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
}

var CorgiComposePath string
var CorgiComposePathDir string
var CorgiComposeFileContent *CorgiCompose

// Get corgi-compose info from path to corgi-compose.yml file
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

	if err := applyServiceDirOverrides(cobra, &corgi); err != nil {
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

	file, err := os.ReadFile(pathToCorgiComposeFile)
	if err != nil {
		return "", CorgiComposeYaml{}, fmt.Errorf("couldn't read %s", pathToCorgiComposeFile)
	}

	// Expand ${VAR} / ${VAR:-default} before parsing, against process env plus an
	// optional sibling .env (env wins).
	dotenv, err := LoadDotEnv(filepath.Join(CorgiComposePathDir, ".env"))
	if err != nil {
		return "", CorgiComposeYaml{}, fmt.Errorf("couldn't read .env next to %s: %v", pathToCorgiComposeFile, err)
	}
	// Tolerant on purpose, and silent: an unset ${VAR} is left untouched so
	// runtime/per-service env, tunnel hostnames, and cross-service
	// ${producer.VAR} refs that resolve later keep working without noise.
	file, _ = InterpolateTolerant(file, EnvThenDotEnv(dotenv))

	var corgiYaml CorgiComposeYaml
	UnknownComposeFields = nil
	DuplicateComposeKeys = nil
	dec := yaml.NewDecoder(bytes.NewReader(file))
	dec.KnownFields(true)
	if err := dec.Decode(&corgiYaml); err != nil {
		// KnownFields surfaces typo'd keys as an error. To stay non-breaking we
		// record them as warnings and re-decode tolerantly so the load succeeds.
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

// detectDuplicateComposeKeys parses the document as raw nodes and reports any
// duplicated key under the top-level services / db_services / required maps,
// which a normal decode would silently collapse.
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
		m := doc.Content[i+1]
		if m.Kind != yaml.MappingNode {
			continue
		}
		seen := map[string]bool{}
		for j := 0; j+1 < len(m.Content); j += 2 {
			key := m.Content[j].Value
			if seen[key] {
				dups = append(dups, fmt.Sprintf("%s.%s", section, key))
			}
			seen[key] = true
		}
	}
	return dups
}

// unknownFieldsFromYAMLError pulls the offending key names out of a yaml.v3
// KnownFields(true) error. Returns nil if the error is not about unknown
// fields (so genuine parse errors still propagate as hard failures).
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
		Init:        y.Init,
		BeforeStart: y.BeforeStart,
		Start:       y.Start,
		AfterStart:  y.AfterStart,
		UseDocker:   y.UseDocker,
		UseAwsVpn:   y.UseAwsVpn,
		Name:        y.Name,
		Description: y.Description,
		EnvTiers:    y.EnvTiers,
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

	return DatabaseService{
		ServiceName:       indexName,
		Driver:            driver,
		Version:           db.Version,
		Host:              host,
		DatabaseName:      db.DatabaseName,
		User:              finalUser,
		Password:          finalPassword,
		Port:              db.Port,
		Port2:             db.Port2,
		ManualRun:         db.ManualRun,
		SeedFromDb:        seedFromDb,
		SeedFromDbEnvPath: db.SeedFromDbEnvPath,
		SeedFromFilePath:  db.SeedFromFilePath,
		Additional:        additional,
		Services:          services,
		Queues:            db.Queues,
		Buckets:           db.Buckets,
		Topics:            db.Topics,
		Subscriptions:     db.Subscriptions,
		Secrets:           db.Secrets,
		Parameters:        db.Parameters,
		Streams:           db.Streams,
		JWTSecret:         db.JWTSecret,
		AuthUsers:         db.AuthUsers,
		ConfigTomlPath:    db.ConfigTomlPath,
		StudioPort:        db.StudioPort,
		InbucketPort:      db.InbucketPort,
		DbPort:            db.DbPort,
		Image:             db.Image,
		ContainerPort:     db.ContainerPort,
		Environment:       db.Environment,
		Volumes:           db.Volumes,
		Command:           db.Command,
		HealthCheck:       db.HealthCheck,
		Profiles:          db.Profiles,
	}, nil
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
	resolveDockerExposedPort(&service)
	resolveServicePathFromCloneFrom(&service)
	normalizeServicePath(&service)
	absolutePath := computeAbsolutePath(service.Path)

	return Service{
		ServiceName:            indexName,
		Path:                   service.Path,
		AbsolutePath:           absolutePath,
		IgnoreEnv:              service.IgnoreEnv,
		ManualRun:              service.ManualRun,
		CloneFrom:              service.CloneFrom,
		Branch:                 service.Branch,
		DependsOnServices:      service.DependsOnServices,
		DependsOnDb:            service.DependsOnDb,
		Exports:                service.Exports,
		Environment:            service.Environment,
		EnvPath:                service.EnvPath,
		CopyEnvFromFilePath:    service.CopyEnvFromFilePath,
		EnvPlaceholdersToCheck: service.EnvPlaceholdersToCheck,
		LocalhostNameInEnv:     service.LocalhostNameInEnv,
		Port:                   service.Port,
		PortAlias:              service.PortAlias,
		BeforeStart:            service.BeforeStart,
		AfterStart:             service.AfterStart,
		RestartPolicy:          service.RestartPolicy,
		OpenOnReady:            service.OpenOnReady,
		Start:                  service.Start,
		Scripts:                service.Scripts,
		InteractiveInput:       service.InteractiveInput,
		AutoSourceEnv:          service.AutoSourceEnv,
		Runner:                 service.Runner,
		Tunnel:                 service.Tunnel,
		HealthCheck:            service.HealthCheck,
		Profiles:               service.Profiles,
	}
}

func resolveDockerExposedPort(service *Service) {
	if service.Runner.Name != "docker" || service.Port != 0 {
		return
	}
	exposedPort, err := GetExposedPortFromDockerfile(*service)
	if err != nil {
		fmt.Println("couldn't get exposed port from Dockerfile: ", err)
	}
	if exposedPort == "" {
		return
	}
	port, err := strconv.Atoi(exposedPort)
	if err != nil {
		fmt.Println("error converting exposed port to integer:", err)
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

// ServiceRepoDir resolves a service's compose `path:` to an absolute repo dir,
// reusing the same logic env generation uses. Used by mission-control's probe.
func ServiceRepoDir(path string) string { return computeAbsolutePath(path) }

// JoinUnderComposeDir resolves a compose-relative path against CorgiComposePathDir
// and rejects anything that escapes it (via `..` or an absolute path), so a
// crafted definitionPath / seedFromFilePath can't read files outside the project.
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

// overrideServiceDirs repoints named services (name=path) at an external working
// dir, e.g. a git worktree. AbsolutePath is the only source of cwd, so this is
// enough. Unknown name / missing dir is a hard error — a typo running the wrong
// tree is worse than a stop.
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

// applyServiceDirOverrides applies --service-dir if the command defines it
// (run/exec/test); others (e.g. clean) are unaffected.
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
		requiredToAdd := Required{
			Name:     indexName,
			Why:      required.Why,
			Install:  required.Install,
			Optional: required.Optional,
			CheckCmd: required.CheckCmd,
			SkipInCi: required.SkipInCi,
		}
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
	// git worktree remove before rm so source repos don't keep dangling entries.
	_ = CleanCorgiWorktrees()
	root := "./corgi_services"
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fmt.Println("couldn't read corgi_services folder: ", err)
		return
	}
	for _, e := range entries {
		// snapshots are expensive to rebuild — preserved here, dropped only by `clean -i snapshots`
		if err := removeExceptSnapshots(filepath.Join(root, e.Name())); err != nil {
			fmt.Println("couldn't clean", e.Name(), ":", err)
		}
	}
	if remaining, err := os.ReadDir(root); err == nil && len(remaining) == 0 {
		_ = os.Remove(root)
	}
	fmt.Println("🗑️ Cleaned up corgi_services (snapshots preserved)")
}

// Recursively removes path but keeps any "snapshots" dir. Lstat (not Stat) so a
// symlink out of corgi_services is removed as a link, never followed and emptied.
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
	keptAny := false
	for _, e := range entries {
		if e.IsDir() && e.Name() == "snapshots" {
			keptAny = true
			continue
		}
		if err := removeExceptSnapshots(filepath.Join(path, e.Name())); err != nil {
			return err
		}
		if e.IsDir() {
			if _, statErr := os.Lstat(filepath.Join(path, e.Name())); statErr == nil {
				keptAny = true
			}
		}
	}
	if keptAny {
		return nil
	}
	return os.Remove(path)
}

func CleanSnapshots() {
	root := "./corgi_services/db_services"
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
			continue // no '=', not a key=value line
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

	// No config here — try one level up (e.g. run from a service folder while the
	// corgi-compose.yml lives in the onboarding/workspace dir above it).
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
