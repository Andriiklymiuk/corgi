package rooms

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Shape struct {
	Name       string            `json:"name,omitempty"`
	About      string            `json:"description,omitempty"`
	Type       string            `json:"type,omitempty"`
	Required   []string          `json:"required,omitempty"`
	Properties map[string]*Shape `json:"properties,omitempty"`
	Items      *Shape            `json:"items,omitempty"`
	Enum       []any             `json:"enum,omitempty"`
	MaxLength  int               `json:"maxLength,omitempty"`
	OnlyListed bool              `json:"onlyListed,omitempty"`
	Minimum    *float64          `json:"minimum,omitempty"`
	Maximum    *float64          `json:"maximum,omitempty"`
}

var shapeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}@[0-9]{1,4}$`)

func shapePath(agentDir, name string) string {
	return filepath.Join(Dir(agentDir), "shapes", name+".json")
}

func SaveShape(agentDir, name string, raw []byte, about string) (Shape, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !shapeName.MatchString(name) {
		return Shape{}, fmt.Errorf("a shape is name@version, like finding@1, not %q", name)
	}
	var s Shape
	if err := json.Unmarshal(raw, &s); err != nil {
		return Shape{}, fmt.Errorf("the shape is not JSON: %w", err)
	}
	if s.Type != "object" || len(s.Properties) == 0 {
		return Shape{}, errors.New(`a shape is an object with properties: {"type":"object","required":[...],"properties":{...}}`)
	}
	if about = strings.TrimSpace(about); about != "" {
		s.About = about
	}
	s.Name = name
	if _, err := os.Stat(shapePath(agentDir, name)); err == nil {
		return Shape{}, fmt.Errorf("%s exists and messages were checked against it: save the change as the next version", name)
	}
	out, _ := json.MarshalIndent(s, "", "  ")
	if err := os.MkdirAll(filepath.Dir(shapePath(agentDir, name)), 0o700); err != nil {
		return Shape{}, err
	}
	return s, os.WriteFile(shapePath(agentDir, name), out, 0o600)
}

func LoadShape(agentDir, name string) (Shape, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !shapeName.MatchString(name) {
		return Shape{}, fmt.Errorf("a shape is name@version, like finding@1, not %q", name)
	}
	raw, err := os.ReadFile(shapePath(agentDir, name))
	if err != nil {
		return Shape{}, fmt.Errorf("no shape %s: corgi agent room shape add %s --file shape.json", name, name)
	}
	var s Shape
	return s, json.Unmarshal(raw, &s)
}

func Shapes(agentDir string) []Shape {
	entries, _ := os.ReadDir(filepath.Join(Dir(agentDir), "shapes"))
	out := []Shape{}
	for _, e := range entries {
		if s, err := LoadShape(agentDir, strings.TrimSuffix(e.Name(), ".json")); err == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s Shape) Check(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("there is no data")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("it is not JSON")
	}
	return s.check(v, "")
}

func (s Shape) check(v any, at string) error {
	where := func() string {
		if at == "" {
			return "the data"
		}
		return at
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, want := range s.Enum {
			ok = ok || want == v
		}
		if !ok {
			return fmt.Errorf("%s is %v, not one of %v", where(), v, s.Enum)
		}
	}
	switch s.Type {
	case "", "any":
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is not an object", where())
		}
		for _, name := range s.Required {
			if _, has := obj[name]; !has {
				return fmt.Errorf("%s is missing", join(at, name))
			}
		}
		for name, value := range obj {
			child, known := s.Properties[name]
			if !known {
				if s.OnlyListed {
					return fmt.Errorf("%s is not in the shape", join(at, name))
				}
				continue
			}
			if err := child.check(value, join(at, name)); err != nil {
				return err
			}
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s is not a list", where())
		}
		if s.Items != nil {
			for i, item := range list {
				if err := s.Items.check(item, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s is not text", where())
		}
		if s.MaxLength > 0 && len(text) > s.MaxLength {
			return fmt.Errorf("%s is longer than %d", where(), s.MaxLength)
		}
	case "number", "integer":
		n, ok := v.(float64)
		if !ok || (s.Type == "integer" && n != float64(int64(n))) {
			return fmt.Errorf("%s is not a %s", where(), map[string]string{"number": "number", "integer": "whole number"}[s.Type])
		}
		if (s.Minimum != nil && n < *s.Minimum) || (s.Maximum != nil && n > *s.Maximum) {
			return fmt.Errorf("%s is out of range", where())
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s is not true or false", where())
		}
	default:
		return fmt.Errorf("the shape names a type corgi does not check: %s", s.Type)
	}
	return nil
}

func join(at, name string) string {
	if at == "" {
		return name
	}
	return at + "." + name
}
