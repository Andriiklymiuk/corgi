package utils

import "strings"

// EnvVar is one resolved environment entry with the part of the compose that
// produced it.
type EnvVar struct {
	Key    string `json:"-"`
	Value  string `json:"value"`
	Source string `json:"source"` // db:<name> | service:<name> | self:port | literal | file:<path>
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

	// service dependencies
	for _, dep := range svc.DependsOnServices {
		chunk := appendDependentServiceEnv("", dep, *corgi)
		entries = append(entries, parseChunkInOrder(chunk, "service:"+dep.Name)...)
	}

	// db dependencies
	for _, dep := range svc.DependsOnDb {
		db := findDbByName(corgi.DatabaseServices, dep.Name)
		if db == nil || (db.ManualRun && !dep.ForceUseEnv) {
			continue
		}
		chunk := generateEnvForDbDependentService(svc, dep, *db)
		entries = append(entries, parseChunkInOrder(chunk, "db:"+dep.Name)...)
	}

	return dedupeLastWins(entries), nil
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
