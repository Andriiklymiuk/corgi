package cmd

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"golang.org/x/term"
)

type packFile struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode,omitempty"`
	Data []byte `json:"data"`
}

type packClaude struct {
	Dir          string                     `json:"dir"`
	Files        []packFile                 `json:"files,omitempty"`
	Marketplaces []string                   `json:"marketplaces,omitempty"`
	Plugins      []string                   `json:"plugins,omitempty"`
	MCP          json.RawMessage            `json:"mcp,omitempty"`
	ProjectMCP   map[string]json.RawMessage `json:"projectMcp,omitempty"`
}

type carryOptions struct {
	only   []string
	tokens bool
	env    bool
	claude bool
}

const (
	carryMagic        = "CORGICARRY1\n"
	carryIterations   = 600_000
	carryPassEnv      = "CORGI_CARRY_PASSPHRASE"
	carryFileLimit    = 2 << 20
	carryTotalLimit   = 200 << 20
	carryEnvFileLimit = 1 << 20
	carryEnvDepth     = 2
)

var carrySkipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".corgi": true, "vendor": true, "Pods": true, "dist": true, "build": true,
	".next": true, ".expo": true, ".venv": true, "venv": true, "target": true, "DerivedData": true, ".turbo": true,
}

func isEnvFileName(name string) bool {
	lower := strings.ToLower(name)
	for _, sample := range []string{"example", "sample", "template", "dist"} {
		if strings.HasSuffix(lower, "."+sample) || strings.HasSuffix(lower, "-"+sample) {
			return false
		}
	}
	return name == ".env" || strings.HasPrefix(name, ".env.") || strings.HasPrefix(name, ".env-") ||
		strings.HasSuffix(name, ".env") || name == ".npmrc" || name == ".yarnrc.yml"
}

func homeRelative(p, home string) string {
	if home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

func underHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, filepath.FromSlash(p[2:]))
	}
	return p
}

func buildCarryPack(dir string, opt carryOptions) (peerPack, error) {
	pack, err := buildPeerPack(dir, opt.only, opt.tokens)
	if err != nil {
		return pack, err
	}
	home, _ := os.UserHomeDir()
	pack.Home = home
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return pack, err
	}
	if opt.tokens {
		pack.Notify = user.NotifyUrl
	}
	total := 0
	for i := range pack.Workspaces {
		w := &pack.Workspaces[i]
		abs := workspacePath(dir, w.ID)
		if abs == "" {
			continue
		}
		w.Path = homeRelative(abs, home)
		if opt.claude {
			if cd := user.Workspaces[w.ID].ConfigDir; cd != "" {
				w.ConfigDir = homeRelative(expandTilde(cd), home)
			}
		}
		if opt.env {
			files, size, err := collectEnvFiles(abs)
			if err != nil {
				return pack, fmt.Errorf("%s: %w", w.ID, err)
			}
			w.Files, total = files, total+size
		}
	}
	if opt.claude {
		for _, cd := range claudeConfigDirs(user, home) {
			pc, size, err := collectClaudeDir(cd, home)
			if err != nil {
				return pack, err
			}
			pack.Claude, total = append(pack.Claude, pc), total+size
		}
	}
	if total > carryTotalLimit {
		return pack, fmt.Errorf("the carry is %d MB, over %d MB; leave out --with-claude or --with-env", total>>20, carryTotalLimit>>20)
	}
	return pack, nil
}

func workspacePath(dir, id string) string {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return ""
	}
	if ws, ok := registry.Find(id); ok {
		return ws.AbsPath
	}
	return ""
}

func collectEnvFiles(root string) ([]packFile, int, error) {
	var files []packFile
	total := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if p != root && (carrySkipDirs[d.Name()] || strings.Count(rel, string(filepath.Separator)) >= carryEnvDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !isEnvFileName(d.Name()) || gitTracks(filepath.Dir(p), d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > carryEnvFileLimit {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		files = append(files, packFile{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm()), Data: data})
		total += len(data)
		return nil
	})
	return files, total, err
}

var gitTracks = func(dir, name string) bool {
	return exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", name).Run() == nil
}

func defaultClaudeDir(home string) string {
	if env := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); env != "" {
		return expandTilde(env)
	}
	return filepath.Join(home, ".claude")
}

func claudeConfigDirs(user *config.UserConfig, home string) []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(expandTilde(p))
		if seen[p] {
			return
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			seen[p] = true
			dirs = append(dirs, p)
		}
	}
	add(defaultClaudeDir(home))
	add(user.Defaults.ConfigDir)
	for _, cfgs := range []map[string]config.WorkspaceConfig{user.Workspaces, user.Profiles} {
		keys := make([]string, 0, len(cfgs))
		for k := range cfgs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			add(cfgs[k].ConfigDir)
		}
	}
	return dirs
}

var claudeTopFiles = regexp.MustCompile(`^(settings\.json|keybindings\.json|statusline[^/]*|[^/]+\.md)$`)

var claudeSubDirs = []string{"rules", "output-styles", "skills", "agents", "commands", "hooks"}

func collectClaudeDir(dir, home string) (packClaude, int, error) {
	pc := packClaude{Dir: homeRelative(dir, home)}
	total := 0
	take := func(p string) {
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() > carryFileLimit {
			return
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(dir, p)
		pc.Files = append(pc.Files, packFile{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm()), Data: data})
		total += len(data)
	}
	walk := func(root string) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				take(p)
			}
			return nil
		})
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && claudeTopFiles.MatchString(e.Name()) {
				take(filepath.Join(dir, e.Name()))
			}
		}
	}
	for _, sub := range claudeSubDirs {
		walk(filepath.Join(dir, sub))
	}
	memories, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "memory"))
	for _, m := range memories {
		walk(m)
	}
	pc.Marketplaces, pc.Plugins = readClaudePlugins(dir, home)
	pc.MCP, pc.ProjectMCP = readClaudeMCP(claudeStateFile(dir, home), home)
	return pc, total, nil
}

func claudeStateFile(dir, home string) string {
	if filepath.Clean(dir) == filepath.Join(home, ".claude") {
		return filepath.Join(home, ".claude.json")
	}
	return filepath.Join(dir, ".claude.json")
}

func readClaudePlugins(dir, home string) ([]string, []string) {
	var markets map[string]struct {
		Source struct {
			Source string `json:"source"`
			Repo   string `json:"repo"`
			URL    string `json:"url"`
			Path   string `json:"path"`
		} `json:"source"`
	}
	var sources []string
	if raw, err := os.ReadFile(filepath.Join(dir, "plugins", "known_marketplaces.json")); err == nil && json.Unmarshal(raw, &markets) == nil {
		for _, m := range markets {
			switch {
			case m.Source.Repo != "":
				sources = append(sources, m.Source.Repo)
			case m.Source.URL != "":
				sources = append(sources, m.Source.URL)
			case m.Source.Path != "":
				sources = append(sources, homeRelative(m.Source.Path, home))
			}
		}
	}
	sort.Strings(sources)
	var installed struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	var plugins []string
	if raw, err := os.ReadFile(filepath.Join(dir, "plugins", "installed_plugins.json")); err == nil && json.Unmarshal(raw, &installed) == nil {
		for k := range installed.Plugins {
			plugins = append(plugins, k)
		}
	}
	sort.Strings(plugins)
	return sources, plugins
}

func readClaudeMCP(path, home string) (json.RawMessage, map[string]json.RawMessage) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var state struct {
		MCP      json.RawMessage `json:"mcpServers"`
		Projects map[string]struct {
			MCP json.RawMessage `json:"mcpServers"`
		} `json:"projects"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return nil, nil
	}
	projects := map[string]json.RawMessage{}
	for p, v := range state.Projects {
		if hasMCPServers(v.MCP) {
			projects[homeRelative(p, home)] = v.MCP
		}
	}
	if !hasMCPServers(state.MCP) {
		state.MCP = nil
	}
	if len(projects) == 0 {
		projects = nil
	}
	return state.MCP, projects
}

func hasMCPServers(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && len(m) > 0
}

type carryApply struct {
	home    string
	oldHome string
	moves   [][2]string
	done    []string
	kept    int
}

func claudeProjectKey(p string) string {
	var b strings.Builder
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (c *carryApply) movePath(p string) string {
	for _, m := range c.moves {
		if p == m[0] || strings.HasPrefix(p, m[0]+"/") {
			return m[1] + p[len(m[0]):]
		}
	}
	return p
}

func (c *carryApply) place(p string) string {
	old := c.oldHome
	if old == "" {
		old = c.home
	}
	return c.movePath(underHome(p, old))
}

func (c *carryApply) moveProjectKey(key string) string {
	for _, m := range c.moves {
		from, to := claudeProjectKey(m[0]), claudeProjectKey(m[1])
		if key == from || strings.HasPrefix(key, from+"-") {
			return to + key[len(from):]
		}
	}
	return key
}

func (c *carryApply) writeIfAbsent(target string, f packFile, rewrite bool) error {
	if _, err := os.Lstat(target); err == nil {
		c.kept++
		return nil
	}
	data := f.Data
	if rewrite && c.oldHome != "" && c.oldHome != c.home && bytes.Contains(data, []byte(c.oldHome)) {
		data = bytes.ReplaceAll(data, []byte(c.oldHome), []byte(c.home))
	}
	mode := fs.FileMode(f.Mode)
	if mode == 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, mode)
}

func safeJoin(root, rel string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(root, clean), true
}

func (c *carryApply) envFiles(target string, w packWorkspace) error {
	wrote := 0
	for _, f := range w.Files {
		p, ok := safeJoin(target, f.Path)
		if !ok {
			continue
		}
		before := c.kept
		if err := c.writeIfAbsent(p, f, false); err != nil {
			return fmt.Errorf("%s: %s: %w", w.ID, f.Path, err)
		}
		if c.kept == before {
			wrote++
		}
	}
	if wrote > 0 {
		c.done = append(c.done, fmt.Sprintf("%s: %d env file(s)", w.ID, wrote))
	}
	return nil
}

func (c *carryApply) claudeDir(pc packClaude, runClaude func(dir string, args ...string) error) error {
	dir := c.place(pc.Dir)
	wrote := 0
	for _, f := range pc.Files {
		rel := f.Path
		if parts := strings.SplitN(rel, "/", 3); len(parts) == 3 && parts[0] == "projects" {
			rel = "projects/" + c.moveProjectKey(parts[1]) + "/" + parts[2]
		}
		p, ok := safeJoin(dir, rel)
		if !ok {
			continue
		}
		before := c.kept
		if err := c.writeIfAbsent(p, f, true); err != nil {
			return fmt.Errorf("%s: %w", pc.Dir, err)
		}
		if c.kept == before {
			wrote++
		}
	}
	if err := c.mergeMCP(claudeStateFile(dir, c.home), pc); err != nil {
		return fmt.Errorf("%s: mcp servers: %w", pc.Dir, err)
	}
	c.done = append(c.done, fmt.Sprintf("claude %s: %d file(s)", pc.Dir, wrote))
	if runClaude == nil || len(pc.Marketplaces)+len(pc.Plugins) == 0 {
		return nil
	}
	have := map[string]bool{}
	_, installed := readClaudePlugins(dir, c.home)
	for _, p := range installed {
		have[p] = true
	}
	added := 0
	for _, src := range pc.Marketplaces {
		_ = runClaude(dir, "plugin", "marketplace", "add", underHome(src, c.home))
	}
	for _, p := range pc.Plugins {
		if have[p] {
			continue
		}
		if runClaude(dir, "plugin", "install", p) == nil {
			added++
		}
	}
	c.done = append(c.done, fmt.Sprintf("claude %s: %d of %d plugin(s) installed", pc.Dir, added+len(have), len(pc.Plugins)))
	return nil
}

func (c *carryApply) mergeMCP(path string, pc packClaude) error {
	if pc.MCP == nil && len(pc.ProjectMCP) == 0 {
		return nil
	}
	state := map[string]any{}
	mode := fs.FileMode(0o600)
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &state); err != nil {
			return fmt.Errorf("%s is not JSON; left alone", path)
		}
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	changed := mergeServers(state, pc.MCP)
	projects, _ := state["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}
	for p, servers := range pc.ProjectMCP {
		key := c.place(p)
		entry, _ := projects[key].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
		}
		if mergeServers(entry, servers) {
			projects[key], changed = entry, true
		}
	}
	if !changed {
		return nil
	}
	if len(projects) > 0 {
		state["projects"] = projects
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".corgi-tmp"
	if err := os.WriteFile(tmp, out, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func mergeServers(into map[string]any, raw json.RawMessage) bool {
	var servers map[string]any
	if json.Unmarshal(raw, &servers) != nil || len(servers) == 0 {
		return false
	}
	have, _ := into["mcpServers"].(map[string]any)
	if have == nil {
		have = map[string]any{}
	}
	changed := false
	for name, s := range servers {
		if _, ok := have[name]; !ok {
			have[name], changed = s, true
		}
	}
	if changed {
		into["mcpServers"] = have
	}
	return changed
}

var runClaudeCommand = func(dir string, args ...string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return err
	}
	cmd := exec.Command("claude", args...)
	home, _ := os.UserHomeDir()
	env := os.Environ()
	if filepath.Clean(dir) != filepath.Join(home, ".claude") {
		env = append(env, "CLAUDE_CONFIG_DIR="+dir)
	}
	cmd.Env = env
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Minute):
		_ = cmd.Process.Kill()
		<-done
		return errors.New("timed out")
	}
}

func sealCarry(plain []byte, pass string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	gcm, err := carryCipher(pass, salt)
	if err != nil {
		return nil, err
	}
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	if _, err := zw.Write(plain); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(carryMagic), salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, zipped.Bytes(), []byte(carryMagic)), nil
}

func openCarry(raw []byte, pass string) ([]byte, error) {
	if !isCarryFile(raw) {
		return nil, errors.New("not a corgi carry file")
	}
	rest := raw[len(carryMagic):]
	if len(rest) < 16+12 {
		return nil, errors.New("the carry file is cut short")
	}
	gcm, err := carryCipher(pass, rest[:16])
	if err != nil {
		return nil, err
	}
	nonce, body := rest[16:16+gcm.NonceSize()], rest[16+gcm.NonceSize():]
	zipped, err := gcm.Open(nil, nonce, body, []byte(carryMagic))
	if err != nil {
		return nil, errors.New("wrong passphrase, or the file changed on the way")
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

func isCarryFile(raw []byte) bool { return bytes.HasPrefix(raw, []byte(carryMagic)) }

func carryCipher(pass string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, pass, salt, carryIterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func newCarryPassphrase() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	var b strings.Builder
	for i := 0; i < 20; i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String(), nil
}

func askCarryPassphrase() (string, error) {
	if p := os.Getenv(carryPassEnv); p != "" {
		return p, nil
	}
	tty, err := os.Open("/dev/tty")
	if err != nil || !term.IsTerminal(int(tty.Fd())) {
		return "", fmt.Errorf("the carry file is sealed: set %s or run this in a terminal", carryPassEnv)
	}
	defer tty.Close()
	fmt.Fprint(os.Stderr, "passphrase: ")
	raw, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(string(raw)), err
}
