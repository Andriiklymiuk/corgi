package utils

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// ComposeField is one key corgi-compose.yml accepts, read from the embedded
// JSON Schema, so `corgi vocab` and the did-you-mean on unknown keys never
// drift from what the parser takes.
type ComposeField struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type schemaNode struct {
	Type                 any                    `json:"type"`
	Description          string                 `json:"description"`
	Enum                 []any                  `json:"enum"`
	Ref                  string                 `json:"$ref"`
	Items                *schemaNode            `json:"items"`
	Properties           map[string]*schemaNode `json:"properties"`
	AdditionalProperties any                    `json:"additionalProperties"`
	OneOf                []*schemaNode          `json:"oneOf"`
	AnyOf                []*schemaNode          `json:"anyOf"`
}

type schemaDoc struct {
	schemaNode
	Definitions map[string]*schemaNode `json:"definitions"`
}

var (
	composeFieldsOnce sync.Once
	composeFields     []ComposeField
)

// The sections a definition is reached from, for the path an agent writes.
var definitionPaths = map[string]string{
	"service":                  "services.<name>",
	"databaseService":          "db_services.<name>",
	"required":                 "required.<name>",
	"dependsOnService":         "services.<name>.depends_on_services[]",
	"dependsOnDb":              "services.<name>.depends_on_db[]",
	"script":                   "services.<name>.scripts[]",
	"warmup":                   "services.<name>.warmup",
	"e2e":                      "e2e",
	"runner":                   "services.<name>.runner",
	"tunnel":                   "services.<name>.tunnel",
	"seedFromDb":               "db_services.<name>.seedFromDb",
	"additionalDatabaseConfig": "db_services.<name>.additional",
	"snsSubscription":          "db_services.<name>.subscriptions[]",
	"awsSecret":                "db_services.<name>.secrets[]",
	"ssmParameter":             "db_services.<name>.parameters[]",
	"supabaseAuthUser":         "db_services.<name>.authUsers[]",
}

func ComposeFields() []ComposeField {
	composeFieldsOnce.Do(func() {
		var doc schemaDoc
		if err := json.Unmarshal([]byte(composeJSONSchema), &doc); err != nil {
			return
		}
		composeFields = appendFields(nil, "", doc.Properties)
		defs := make([]string, 0, len(doc.Definitions))
		for k := range doc.Definitions {
			defs = append(defs, k)
		}
		sort.Strings(defs)
		for _, d := range defs {
			prefix, ok := definitionPaths[d]
			if !ok {
				prefix = d
			}
			composeFields = appendFields(composeFields, prefix, doc.Definitions[d].Properties)
		}
	})
	return composeFields
}

func appendFields(out []ComposeField, prefix string, props map[string]*schemaNode) []ComposeField {
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		n := props[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		out = append(out, ComposeField{Name: k, Path: path, Type: schemaType(n), Description: n.Description, Enum: enumOf(n)})
	}
	return out
}

func schemaType(n *schemaNode) string {
	if n == nil {
		return ""
	}
	switch t := n.Type.(type) {
	case string:
		return namedSchemaType(t, n.Items)
	case []any:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "|")
	}
	if n.Ref != "" {
		return n.Ref[strings.LastIndex(n.Ref, "/")+1:]
	}
	var alts []string
	for _, a := range append(n.OneOf, n.AnyOf...) {
		if s := schemaType(a); s != "" {
			alts = append(alts, s)
		}
	}
	return strings.Join(alts, "|")
}

func namedSchemaType(t string, items *schemaNode) string {
	if t != "array" || items == nil {
		return t
	}
	if it := schemaType(items); it != "" {
		return it + "[]"
	}
	return t
}

func enumOf(n *schemaNode) []string {
	var out []string
	for _, e := range n.Enum {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 && n.Items != nil {
		return enumOf(n.Items)
	}
	return out
}

// ComposeFieldNames lists every distinct key the schema accepts.
func ComposeFieldNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range ComposeFields() {
		if !seen[f.Name] {
			seen[f.Name] = true
			out = append(out, f.Name)
		}
	}
	return out
}

// LookupComposeField returns every place a key appears (a key such as port is
// valid on both services and db_services).
func LookupComposeField(name string) []ComposeField {
	var out []ComposeField
	for _, f := range ComposeFields() {
		if f.Name == name || f.Path == name {
			out = append(out, f)
		}
	}
	return out
}
