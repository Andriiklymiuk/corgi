package idea

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const localDirName = "ideas-local"

func WorkspaceRoot(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".corgi", "ideas")
}

type localMeta struct {
	Key               string    `json:"key,omitempty"`
	KeyUntil          time.Time `json:"keyUntil,omitempty"`
	ReadOnly          bool      `json:"readOnly,omitempty"`
	Answering         bool      `json:"answering,omitempty"`
	Source            string    `json:"source,omitempty"`
	Workspace         string    `json:"workspace,omitempty"`
	Auto              bool      `json:"auto,omitempty"`
	BuildWhenApproved bool      `json:"buildWhenApproved,omitempty"`
	Tasks             []int     `json:"tasks,omitempty"`
	Plan              int       `json:"plan,omitempty"`
}

func localPath(agentDir, slug string) string {
	return filepath.Join(agentDir, localDirName, slug+".json")
}

func Home(agentDir, slug string) (string, bool) {
	link := Dir(agentDir, slug)
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	return target, true
}

func shownHome(target string) string {
	return filepath.Join(".corgi", "ideas", filepath.Base(target))
}

func takeLocal(m *Meta) localMeta {
	l := localMeta{Key: m.Key, KeyUntil: m.KeyUntil, ReadOnly: m.ReadOnly, Answering: m.Answering, Source: m.Source,
		Workspace: m.Workspace, Auto: m.Auto, BuildWhenApproved: m.BuildWhenApproved, Tasks: m.Tasks, Plan: m.Plan}
	m.Key, m.KeyUntil, m.ReadOnly, m.Answering, m.Source = "", time.Time{}, false, false, ""
	m.Workspace, m.Auto, m.BuildWhenApproved, m.Tasks, m.Plan = "", false, false, nil, 0
	return l
}

func (l localMeta) fill(m *Meta) {
	m.Key, m.KeyUntil, m.ReadOnly, m.Answering, m.Source = l.Key, l.KeyUntil, l.ReadOnly, l.Answering, l.Source
	m.Workspace, m.Auto, m.BuildWhenApproved, m.Tasks, m.Plan = l.Workspace, l.Auto, l.BuildWhenApproved, l.Tasks, l.Plan
}

func readLocal(agentDir, slug string) localMeta {
	var l localMeta
	if raw, err := os.ReadFile(localPath(agentDir, slug)); err == nil {
		_ = json.Unmarshal(raw, &l)
	}
	return l
}

func writeLocal(agentDir, slug string, l localMeta) error {
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localPath(agentDir, slug)), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(localPath(agentDir, slug), raw, 0o600)
}

func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func moveDir(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyTree(from, to); err != nil {
		_ = os.RemoveAll(to)
		return err
	}
	return os.RemoveAll(from)
}

func copyTree(from, to string) error {
	return filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func openForTheRepository(dir string) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return os.Chmod(path, 0o755)
		}
		return os.Chmod(path, 0o644)
	})
}

func MoveToWorkspace(agentDir, slug, workspaceDir string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if !ValidSlug(slug) {
		return "", fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	link := Dir(agentDir, slug)
	if _, err := os.Stat(filepath.Join(link, MetaName)); err != nil {
		return "", fmt.Errorf("%w: %s - corgi idea list", ErrNotFound, slug)
	}
	root := WorkspaceRoot(workspaceDir)
	current, linked := Home(agentDir, slug)
	if linked && sameDir(filepath.Dir(current), root) {
		return current, nil
	}
	target := filepath.Join(root, slug)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%s already holds a page named %s: corgi idea list finds it, or rename one of them", root, slug)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	from := link
	if linked {
		from = current
	}
	if err := moveDir(from, target); err != nil {
		return "", err
	}
	if linked {
		_ = os.Remove(link)
	}
	openForTheRepository(target)
	if err := os.Symlink(target, link); err != nil {
		_ = moveDir(target, link)
		return "", fmt.Errorf("could not link %s into the workspace: %v", slug, err)
	}
	if err := resave(agentDir, slug, linked); err != nil {
		return "", err
	}
	return target, nil
}

func MoveHome(agentDir, slug string) error {
	mu.Lock()
	defer mu.Unlock()
	target, linked := Home(agentDir, slug)
	if !linked {
		return nil
	}
	local := readLocal(agentDir, slug)
	link := Dir(agentDir, slug)
	if err := os.Remove(link); err != nil {
		return err
	}
	if err := moveDir(target, link); err != nil {
		_ = os.Symlink(target, link)
		return err
	}
	_ = os.Chmod(link, 0o700)
	raw, err := os.ReadFile(filepath.Join(link, MetaName))
	if err != nil {
		return err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	local.fill(&m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(link, MetaName), out, 0o600); err != nil {
		return err
	}
	_ = os.Remove(localPath(agentDir, slug))
	Touch(agentDir)
	return nil
}

func resave(agentDir, slug string, hasLocal bool) error {
	dir := Dir(agentDir, slug)
	raw, err := os.ReadFile(filepath.Join(dir, MetaName))
	if err != nil {
		return err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if local := takeLocal(&m); !hasLocal {
		if err := writeLocal(agentDir, slug, local); err != nil {
			return err
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(dir, MetaName), out, 0o644); err != nil {
		return err
	}
	Touch(agentDir)
	return nil
}

func Adopt(agentDir, workspaceDir, workspaceID string) []string {
	mu.Lock()
	defer mu.Unlock()
	root := WorkspaceRoot(workspaceDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	store := Root(agentDir)
	held := map[string]bool{}
	if links, err := os.ReadDir(store); err == nil {
		for _, l := range links {
			if target, ok := Home(agentDir, l.Name()); ok {
				if real, err := filepath.EvalSymlinks(target); err == nil {
					held[real] = true
				}
			}
		}
	}
	var adopted []string
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if !e.IsDir() || !ValidSlug(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, MetaName)); err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || held[real] {
			continue
		}
		slug := freeSlug(agentDir, e.Name())
		if slug == "" || os.MkdirAll(store, 0o700) != nil || os.Symlink(dir, Dir(agentDir, slug)) != nil {
			continue
		}
		_ = writeLocal(agentDir, slug, localMeta{Workspace: workspaceID})
		held[real] = true
		adopted = append(adopted, slug)
	}
	if len(adopted) > 0 {
		Touch(agentDir)
	}
	return adopted
}

func freeSlug(agentDir, base string) string {
	slug := base
	for n := 2; n < 100; n++ {
		if _, err := os.Lstat(Dir(agentDir, slug)); os.IsNotExist(err) {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, n)
		if !ValidSlug(slug) {
			return ""
		}
	}
	return ""
}

func Remove(agentDir, slug string) error {
	mu.Lock()
	defer mu.Unlock()
	link := Dir(agentDir, slug)
	if target, linked := Home(agentDir, slug); linked {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if err := os.RemoveAll(link); err != nil {
		return err
	}
	_ = os.Remove(localPath(agentDir, slug))
	Touch(agentDir)
	return nil
}
