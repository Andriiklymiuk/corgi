package utils

import "testing"

// Catches accidental typos or duplicate values in the error-code catalog,
// which is a stable contract for agents.
func TestErrCodeValues(t *testing.T) {
	want := map[string]string{
		"ErrPortConflict":     "E_PORT_CONFLICT",
		"ErrDanglingDep":      "E_DANGLING_DEP",
		"ErrDependencyCycle":  "E_DEPENDENCY_CYCLE",
		"ErrUnknownDriver":    "E_UNKNOWN_DRIVER",
		"ErrMissingStart":     "E_MISSING_START",
		"ErrMissingField":     "E_MISSING_FIELD",
		"ErrComposeNotFound":  "E_COMPOSE_NOT_FOUND",
		"ErrComposeParse":     "E_COMPOSE_PARSE",
		"ErrServiceNotFound":  "E_SERVICE_NOT_FOUND",
		"ErrInteractiveReq":   "E_INTERACTIVE_REQUIRED",
		"ErrUnhealthy":        "E_UNHEALTHY",
		"ErrReadinessTimeout": "E_READINESS_TIMEOUT",
		"ErrDockerDown":       "E_DOCKER_DOWN",
		"ErrUsage":            "E_USAGE",
		"ErrExecFailed":       "E_EXEC_FAILED",
		"ErrUnknownProfile":   "E_UNKNOWN_PROFILE",
		"ErrInvalidCondition": "E_INVALID_CONDITION",
	}

	got := map[string]string{
		"ErrPortConflict":     ErrPortConflict,
		"ErrDanglingDep":      ErrDanglingDep,
		"ErrDependencyCycle":  ErrDependencyCycle,
		"ErrUnknownDriver":    ErrUnknownDriver,
		"ErrMissingStart":     ErrMissingStart,
		"ErrMissingField":     ErrMissingField,
		"ErrComposeNotFound":  ErrComposeNotFound,
		"ErrComposeParse":     ErrComposeParse,
		"ErrServiceNotFound":  ErrServiceNotFound,
		"ErrInteractiveReq":   ErrInteractiveReq,
		"ErrUnhealthy":        ErrUnhealthy,
		"ErrReadinessTimeout": ErrReadinessTimeout,
		"ErrDockerDown":       ErrDockerDown,
		"ErrUsage":            ErrUsage,
		"ErrExecFailed":       ErrExecFailed,
		"ErrUnknownProfile":   ErrUnknownProfile,
		"ErrInvalidCondition": ErrInvalidCondition,
	}

	for name, wantVal := range want {
		if got[name] != wantVal {
			t.Errorf("%s = %q, want %q", name, got[name], wantVal)
		}
	}

	seen := make(map[string]string)
	for name, val := range got {
		if prev, dup := seen[val]; dup {
			t.Errorf("duplicate error code %q used by %s and %s", val, prev, name)
		}
		seen[val] = name
	}
}
