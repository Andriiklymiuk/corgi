package utils

import "gopkg.in/yaml.v3"

// One beforeStart entry: a command plus optional cacheKey files whose unchanged
// hash lets corgi skip the step.
type BeforeStartStep struct {
	Run      string
	CacheKey []string
}

// BeforeStartSteps parses entries that are either a plain string (today) or an
// object {run, cacheKey}.
type BeforeStartSteps []BeforeStartStep

func (s *BeforeStartSteps) UnmarshalYAML(value *yaml.Node) error {
	var raw []yaml.Node
	if err := value.Decode(&raw); err != nil {
		return err
	}
	steps := make(BeforeStartSteps, 0, len(raw))
	for i := range raw {
		n := &raw[i]
		if n.Kind == yaml.ScalarNode {
			steps = append(steps, BeforeStartStep{Run: n.Value})
			continue
		}
		var obj struct {
			Run      string   `yaml:"run"`
			CacheKey []string `yaml:"cacheKey"`
		}
		if err := n.Decode(&obj); err != nil {
			return err
		}
		steps = append(steps, BeforeStartStep{Run: obj.Run, CacheKey: obj.CacheKey})
	}
	*s = steps
	return nil
}

// HasCacheKeys reports whether any step opts into caching.
func (s BeforeStartSteps) HasCacheKeys() bool {
	for _, st := range s {
		if len(st.CacheKey) > 0 {
			return true
		}
	}
	return false
}

// Commands returns just the command strings, for the existing runners.
func (s BeforeStartSteps) Commands() []string {
	out := make([]string, 0, len(s))
	for _, st := range s {
		out = append(out, st.Run)
	}
	return out
}
