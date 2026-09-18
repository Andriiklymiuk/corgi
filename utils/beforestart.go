package utils

import "gopkg.in/yaml.v3"

type BeforeStartStep struct {
	Run      string
	CacheKey []string
}

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

func (s BeforeStartSteps) HasCacheKeys() bool {
	for _, st := range s {
		if len(st.CacheKey) > 0 {
			return true
		}
	}
	return false
}

func (s BeforeStartSteps) Commands() []string {
	out := make([]string, 0, len(s))
	for _, st := range s {
		out = append(out, st.Run)
	}
	return out
}
