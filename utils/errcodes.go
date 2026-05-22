package utils

// Error codes emitted via JSONError. Stable contract for agents — codes never
// change meaning; new codes may be added. Documented in docs/agents.md.
const (
	ErrPortConflict     = "E_PORT_CONFLICT"
	ErrDanglingDep      = "E_DANGLING_DEP"
	ErrDependencyCycle  = "E_DEPENDENCY_CYCLE"
	ErrUnknownDriver    = "E_UNKNOWN_DRIVER"
	ErrMissingStart     = "E_MISSING_START"
	ErrMissingField     = "E_MISSING_FIELD"
	ErrComposeNotFound  = "E_COMPOSE_NOT_FOUND"
	ErrComposeParse     = "E_COMPOSE_PARSE"
	ErrServiceNotFound  = "E_SERVICE_NOT_FOUND"
	ErrInteractiveReq   = "E_INTERACTIVE_REQUIRED"
	ErrUnhealthy        = "E_UNHEALTHY"
	ErrReadinessTimeout = "E_READINESS_TIMEOUT"
	ErrDockerDown       = "E_DOCKER_DOWN"
	ErrUsage            = "E_USAGE"           // invalid command usage / args
	ErrExecFailed       = "E_EXEC_FAILED"     // command failed to spawn
	ErrUnknownProfile   = "E_UNKNOWN_PROFILE" // --profile matched no services/db_services
	ErrInvalidCondition = "E_INVALID_CONDITION"
	ErrConfig           = "E_CONFIG"          // could not load/resolve the compose file
	ErrAlreadyRunning   = "E_ALREADY_RUNNING" // a detached run is already active
	ErrUnsupported      = "E_UNSUPPORTED"     // operation not supported yet
	ErrConfigPath       = "E_CONFIG_PATH"     // cannot resolve user-config dir
	ErrConfigRead       = "E_CONFIG_READ"     // cannot read user-config file
)
