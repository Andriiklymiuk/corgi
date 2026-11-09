package utils

import "strings"

// CodeInfo describes one stable error or warning code: what it means and what
// to do next. The catalog is the single source for the `fix` field of every
// JSON error, `corgi vocab <code>`, and the table in docs/agents.md.
type CodeInfo struct {
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
	Fix     string `json:"fix"`
	Warning bool   `json:"warning,omitempty"`
}

var ErrorCatalog = []CodeInfo{
	{Code: ErrComposeNotFound, Meaning: "no corgi-compose.yml resolved", Fix: "run from a directory with corgi-compose.yml, or pass -f <path>"},
	{Code: ErrConfig, Meaning: "could not load or resolve the compose file", Fix: "run `corgi validate` to see what is wrong, or pass -f <path>"},
	{Code: ErrComposeParse, Meaning: "the YAML failed to parse", Fix: "fix the syntax at the line the message names, then `corgi validate`"},
	{Code: ErrInteractiveReq, Meaning: "a prompt was needed but no one can answer it", Fix: "pass the flag the message names (`corgi vocab <command>` lists them)"},
	{Code: ErrServiceNotFound, Meaning: "the named service or db_service is not in the compose file", Fix: "check the name against `corgi ps --json`"},
	{Code: ErrMissingField, Meaning: "a required field is absent", Fix: "add the field the message names (`corgi vocab <field>` shows its shape)"},
	{Code: ErrDanglingDep, Meaning: "depends_on_services or depends_on_db names a service that does not exist", Fix: "correct the name to one under services: or db_services:"},
	{Code: ErrDependencyCycle, Meaning: "depends_on_services forms a cycle", Fix: "remove one edge of the cycle the message prints"},
	{Code: ErrUnknownDriver, Meaning: "db_services.driver is not a known driver", Fix: "use a driver `corgi vocab driver` lists"},
	{Code: ErrMissingStart, Meaning: "a service has a port but no start command and no runner", Fix: "add start: or runner: {name: docker}"},
	{Code: ErrPortConflict, Meaning: "two services or db_services bind the same host port", Fix: "give each a distinct port:"},
	{Code: ErrUnhealthy, Meaning: "a readiness or health probe failed", Fix: "run `corgi why <service> --json`, then `corgi logs <service>`"},
	{Code: ErrReadinessTimeout, Meaning: "a dependency was not ready before the deadline", Fix: "run `corgi why <service> --json`; raise --ready-timeout only if it is slow, not broken"},
	{Code: ErrDockerDown, Meaning: "the Docker daemon is unreachable", Fix: "start Docker (`corgi doctor --fix` tries), then retry"},
	{Code: ErrUsage, Meaning: "invalid command usage or arguments", Fix: "run `corgi <command> --help`; the message names the accepted flags"},
	{Code: ErrExecFailed, Meaning: "the command could not be spawned or exited non-zero", Fix: "check the binary is on PATH in the service's directory; `corgi exec <service> -- which <bin>`"},
	{Code: ErrUnknownProfile, Meaning: "run --profile matched no services or db_services", Fix: "use a name from a profiles: list in corgi-compose.yml"},
	{Code: ErrInvalidCondition, Meaning: "a depends_on condition is not ready or started", Fix: "set condition: ready or condition: started"},
	{Code: ErrAlreadyRunning, Meaning: "a detached run is already active for this project", Fix: "`corgi restart`, `corgi stop`, or `corgi run --detach --force`"},
	{Code: ErrUnsupported, Meaning: "the operation is not supported here yet", Fix: "use the alternative the message names"},
	{Code: ErrNotRunning, Meaning: "no matching detached service to act on", Fix: "start it with `corgi run --detach` first"},
	{Code: ErrConfigPath, Meaning: "the user config directory cannot be resolved", Fix: "check that $HOME is set and ~/.corgi is writable"},
	{Code: ErrConfigRead, Meaning: "the user config file cannot be read", Fix: "check ~/.corgi/config.yml parses, or move it aside"},
	{Code: ErrDuplicateName, Meaning: "a name is used by more than one service or db_service, or a key is repeated", Fix: "rename so every service and db_service name is unique"},
	{Code: ErrPortRange, Meaning: "a configured port is outside 1-65535", Fix: "use a port between 1 and 65535"},
	{Code: WarnNoHealthcheck, Meaning: "a depended-on service has no healthCheck, so a TCP probe is used", Fix: "add healthCheck: with a URL or command that means ready", Warning: true},
	{Code: WarnNoBranch, Meaning: "cloneFrom is set without a branch", Fix: "add branch: so every clone checks out the same code", Warning: true},
	{Code: WarnUnknownField, Meaning: "an unknown key in corgi-compose.yml was ignored", Fix: "rename it to the field the warning suggests (`corgi vocab <field>`)", Warning: true},
}

func LookupCode(code string) (CodeInfo, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, c := range ErrorCatalog {
		if c.Code == code {
			return c, true
		}
	}
	return CodeInfo{}, false
}

func FixFor(code string) string {
	c, _ := LookupCode(code)
	return c.Fix
}
