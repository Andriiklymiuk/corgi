package utils

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenOnReady opens a service's URL when its healthCheck passes. Parses either
// a bool (openOnReady: true) or an object {path, scheme, browser}.
type OpenOnReady struct {
	Enabled bool
	Path    string
	Scheme  string
	Browser string
}

func (o *OpenOnReady) UnmarshalYAML(value *yaml.Node) error {
	var b bool
	if value.Kind == yaml.ScalarNode && value.Decode(&b) == nil {
		o.Enabled = b
		return nil
	}
	var obj struct {
		Path    string `yaml:"path"`
		Scheme  string `yaml:"scheme"`
		Browser string `yaml:"browser"`
	}
	if err := value.Decode(&obj); err != nil {
		return err
	}
	o.Enabled, o.Path, o.Scheme, o.Browser = true, obj.Path, obj.Scheme, obj.Browser
	return nil
}

// URL builds the address to open: scheme (default http) + localhost + port + path (default /).
func (o OpenOnReady) URL(port int) string {
	scheme := o.Scheme
	if scheme == "" {
		scheme = "http"
	}
	path := o.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s://localhost:%d%s", scheme, port, path)
}
