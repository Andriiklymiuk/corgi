package utils

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type EnvVar struct {
	Key    string `json:"-"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

func (e EnvVar) IsGenerated() bool {
	for _, prefix := range []string{"db:", "service:", "self:", "literal"} {
		if strings.HasPrefix(e.Source, prefix) {
			return true
		}
	}
	return false
}

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

func ResolveServiceEnv(svc Service, corgi *CorgiCompose) ([]EnvVar, error) {
	chain, err := ResolveServiceEnvChain(svc, corgi)
	if err != nil {
		return nil, err
	}
	return dedupeLastWins(chain), nil
}

func ResolveServiceEnvChain(svc Service, corgi *CorgiCompose) ([]EnvVar, error) {
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

	rewriteLocalhostInEntries(entries, svc)
	return entries, nil
}

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
				continue
			}
			return nil, err
		}
		expanded = substituteEnvVarReferences(expanded, existing)
		out = append(out, parseChunkInOrder(expanded, "literal")...)
	}
	return out, nil
}

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

func ResolveAllEnv(corgi *CorgiCompose) (map[string][]EnvVar, error) {
	return resolveEveryService(corgi, ResolveServiceEnv)
}

func ResolveAllEnvChains(corgi *CorgiCompose) (map[string][]EnvVar, error) {
	return resolveEveryService(corgi, ResolveServiceEnvChain)
}

func resolveEveryService(
	corgi *CorgiCompose,
	resolve func(Service, *CorgiCompose) ([]EnvVar, error),
) (map[string][]EnvVar, error) {
	if corgi == nil {
		return map[string][]EnvVar{}, nil
	}
	resolved, err := resolveExportsFixedPoint(corgi)
	if err != nil {
		return nil, err
	}
	currentExportsMap = resolved
	defer func() { currentExportsMap = nil }()

	out := make(map[string][]EnvVar, len(corgi.Services))
	for _, svc := range corgi.Services {
		entries, err := resolve(svc, corgi)
		if err != nil {
			return nil, err
		}
		out[svc.ServiceName] = entries
	}
	return out, nil
}

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
