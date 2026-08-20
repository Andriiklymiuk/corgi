package utils

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// EnvVar is one resolved environment entry with the part of the compose that
// produced it.
type EnvVar struct {
	Key    string `json:"-"`
	Value  string `json:"value"`
	Source string `json:"source"` // db:<name> | service:<name> | self:port | literal | file:<path>
}

// IsGenerated reports whether corgi itself produces this variable at run time,
// as opposed to reading it from a copied env file. A whitelist on purpose: a
// future source label lands as not-generated until classified here, so
// `corgi env check` fails closed instead of silently passing missing keys.
func (e EnvVar) IsGenerated() bool {
	for _, prefix := range []string{"db:", "service:", "self:", "literal"} {
		if strings.HasPrefix(e.Source, prefix) {
			return true
		}
	}
	return false
}

// parseChunkInOrder splits a corgi env chunk into ordered KEY=VALUE pairs,
// tagging each with source. Blank/comment lines are skipped.
func parseChunkInOrder(chunk, source string) []EnvVar {
	var out []EnvVar
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		out = append(out, EnvVar{
			Key:    strings.TrimSpace(line[:idx]),
			Value:  strings.TrimSpace(line[idx+1:]),
			Source: source,
		})
	}
	return out
}

// ResolveServiceEnv returns svc's fully-resolved env entries with source
// attribution. Read-only: calls the same builders as corgi run, writes nothing.
func ResolveServiceEnv(svc Service, corgi *CorgiCompose) ([]EnvVar, error) {
	if corgi == nil || svc.IgnoreEnv {
		return []EnvVar{}, nil
	}
	var entries []EnvVar
	entries = append(entries, copiedEnvFileEntries(svc)...)
	entries = append(entries, dependentServiceEntries(svc, corgi)...)
	entries = append(entries, databaseDependencyEntries(svc, corgi)...)
	entries = append(entries, selfPortEntry(svc)...)

	literal, err := literalEnvironmentEntries(svc, entries)
	if err != nil {
		return nil, err
	}
	entries = append(entries, literal...)

	resolved := dedupeLastWins(entries)
	rewriteLocalhostInEntries(resolved, svc)
	return resolved, nil
}

// copiedEnvFileEntries is the copied env file (lowest precedence); honors
// tier + .env-example fallback.
func copiedEnvFileEntries(svc Service) []EnvVar {
	src := resolveEnvSourceFile(CorgiComposePathDir, svc, "", ActiveTierName, ActiveTierDir)
	if src == "" {
		return nil
	}
	chunk := getEnvFromFile(src, corgiGeneratedMessage)
	return parseChunkInOrder(chunk, "file:"+filepath.Base(src))
}

func dependentServiceEntries(svc Service, corgi *CorgiCompose) []EnvVar {
	var entries []EnvVar
	for _, dep := range svc.DependsOnServices {
		chunk := appendDependentServiceEnv("", dep, *corgi)
		entries = append(entries, parseChunkInOrder(chunk, "service:"+dep.Name)...)
	}
	return entries
}

func databaseDependencyEntries(svc Service, corgi *CorgiCompose) []EnvVar {
	var entries []EnvVar
	for _, dep := range svc.DependsOnDb {
		db := findDbByName(corgi.DatabaseServices, dep.Name)
		if db == nil || (db.ManualRun && !dep.ForceUseEnv) {
			continue
		}
		chunk := generateEnvForDbDependentService(svc, dep, *db)
		entries = append(entries, parseChunkInOrder(chunk, "db:"+dep.Name)...)
	}
	return entries
}

func selfPortEntry(svc Service) []EnvVar {
	if svc.Port == 0 {
		return nil
	}
	alias := "PORT"
	if svc.PortAlias != "" {
		alias = svc.PortAlias
	}
	return []EnvVar{{Key: alias, Value: fmt.Sprint(svc.Port), Source: "self:port"}}
}

// literalEnvironmentEntries covers literal environment: lines (own ${VAR} +
// cross-service ${producer.VAR}).
func literalEnvironmentEntries(svc Service, entries []EnvVar) ([]EnvVar, error) {
	if len(svc.Environment) == 0 {
		return nil, nil
	}
	existing := map[string]string{}
	for _, e := range entries {
		existing[e.Key] = e.Value
	}
	var out []EnvVar
	for _, raw := range svc.Environment {
		expanded, err := substituteCrossServiceRefs(raw, svc, currentExportsMap)
		if err != nil {
			var skipped *producerSkippedError
			if errors.As(err, &skipped) {
				continue // producer not in selection; generator drops it too
			}
			return nil, err
		}
		expanded = substituteEnvVarReferences(expanded, existing)
		out = append(out, parseChunkInOrder(expanded, "literal")...)
	}
	return out, nil
}

// rewriteLocalhostInEntries mirrors renderEnvFileContent's final host rewrite
// (generateEnv.go) so reported values match the written .env.
// LocalhostNameInEnv wins if set; otherwise --host (HostOverride) catches
// user-written URLs too.
func rewriteLocalhostInEntries(resolved []EnvVar, svc Service) {
	switch {
	case svc.LocalhostNameInEnv != "":
		for i := range resolved {
			resolved[i].Value = strings.ReplaceAll(resolved[i].Value, "localhost", svc.LocalhostNameInEnv)
		}
	case HostOverride != "":
		for i := range resolved {
			resolved[i].Value = strings.ReplaceAll(resolved[i].Value, "localhost", HostOverride)
		}
	}
}

// ResolveAllEnv resolves every service's env, keyed by service name. It primes
// the cross-service exports fixed point first so ${producer.VAR} references in
// any service resolve to real values.
func ResolveAllEnv(corgi *CorgiCompose) (map[string][]EnvVar, error) {
	if corgi == nil {
		return map[string][]EnvVar{}, nil
	}
	// resolveExportsFixedPoint only returns the map; it does not assign the
	// package global that ResolveServiceEnv's literal block reads (unlike the
	// run path, where GenerateEnvForServices assigns it). Assign it here.
	resolved, err := resolveExportsFixedPoint(corgi)
	if err != nil {
		return nil, err
	}
	currentExportsMap = resolved
	defer func() { currentExportsMap = nil }()

	out := make(map[string][]EnvVar, len(corgi.Services))
	for _, svc := range corgi.Services {
		entries, err := ResolveServiceEnv(svc, corgi)
		if err != nil {
			return nil, err
		}
		out[svc.ServiceName] = entries
	}
	return out, nil
}

// dedupeLastWins keeps the last value/source per key (matching the concat+parse
// behaviour of the real generator), ordered by each key's final position.
func dedupeLastWins(in []EnvVar) []EnvVar {
	last := map[string]int{}
	for i, e := range in {
		last[e.Key] = i
	}
	out := make([]EnvVar, 0, len(last))
	for i, e := range in {
		if last[e.Key] == i {
			out = append(out, e)
		}
	}
	return out
}
