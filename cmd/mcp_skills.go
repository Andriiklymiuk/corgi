package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	skillScheme     = "skill://corgi/"
	skillFile       = "SKILL.md"
	skillsTTLMs     = 300000
	skillsDirectory = "plugins/corgi/skills"
	mimeDirectory   = "inode/directory"
	mimeMarkdown    = "text/markdown"
)

var BundledSkills fs.FS

type skillResource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type skillEntry struct {
	URI         string          `json:"uri"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Resources   []skillResource `json:"resources"`
}

type skillSet struct {
	entries []skillEntry
	files   map[string][]byte
	dirs    map[string][]string
}

var (
	skillsOnce   sync.Once
	skillsLoaded skillSet
)

func bundledSkills() skillSet {
	skillsOnce.Do(func() {
		if BundledSkills == nil {
			return
		}
		if sub, err := fs.Sub(BundledSkills, skillsDirectory); err == nil {
			skillsLoaded = readSkillSet(sub)
		}
	})
	return skillsLoaded
}

func readSkillSet(root fs.FS) skillSet {
	set := skillSet{files: map[string][]byte{}, dirs: map[string][]string{}}
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil
		}
		parent := path.Dir(p)
		if parent == "." {
			parent = ""
		}
		set.dirs[parent] = append(set.dirs[parent], p)
		if d.IsDir() {
			return nil
		}
		if raw, err := fs.ReadFile(root, p); err == nil {
			set.files[p] = raw
		}
		return nil
	})
	for _, name := range set.dirs[""] {
		front := skillFrontmatter(set.files[name+"/"+skillFile])
		if front["name"] != name || front["description"] == nil {
			continue
		}
		entry := skillEntry{URI: skillScheme + name + "/" + skillFile, Frontmatter: front}
		for p, raw := range set.files {
			if strings.HasPrefix(p, name+"/") {
				sum := sha256.Sum256(raw)
				entry.Resources = append(entry.Resources, skillResource{URI: skillScheme + p, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(raw)})
			}
		}
		sort.Slice(entry.Resources, func(i, j int) bool { return entry.Resources[i].URI < entry.Resources[j].URI })
		set.entries = append(set.entries, entry)
	}
	sort.Slice(set.entries, func(i, j int) bool { return set.entries[i].URI < set.entries[j].URI })
	return set
}

func skillFrontmatter(raw []byte) map[string]any {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return nil
	}
	front := map[string]any{}
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &front); err != nil {
		return nil
	}
	return front
}

func skillPath(uri string) (string, bool) {
	if !strings.HasPrefix(uri, skillScheme) {
		return "", false
	}
	p := path.Clean(strings.TrimPrefix(uri, skillScheme))
	if p == "." || strings.HasPrefix(p, "..") {
		return "", false
	}
	return p, true
}

func (s skillSet) entry(uri string) (skillEntry, bool) {
	for _, e := range s.entries {
		if e.URI == uri {
			return e, true
		}
	}
	return skillEntry{}, false
}

func (s skillSet) content(uri string) (map[string]any, bool) {
	p, ok := skillPath(uri)
	raw, found := s.files[p]
	if !ok || !found {
		return nil, false
	}
	if !utf8.Valid(raw) {
		return map[string]any{"uri": uri, "mimeType": "application/octet-stream", "blob": base64.StdEncoding.EncodeToString(raw)}, true
	}
	return map[string]any{"uri": uri, "mimeType": skillMime(p), "text": string(raw)}, true
}

func (s skillSet) children(uri string) ([]map[string]any, bool) {
	p, ok := skillPath(uri)
	kids, found := s.dirs[p]
	if !ok || !found {
		return nil, false
	}
	out := []map[string]any{}
	for _, kid := range kids {
		mime := skillMime(kid)
		if _, isFile := s.files[kid]; !isFile {
			mime = mimeDirectory
		}
		out = append(out, map[string]any{"uri": skillScheme + kid, "name": path.Base(kid), "mimeType": mime})
	}
	return out, true
}

func skillMime(p string) string {
	switch path.Ext(p) {
	case ".md":
		return mimeMarkdown
	case ".json":
		return mimeJSON
	}
	return "text/plain"
}
