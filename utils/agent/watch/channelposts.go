package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const reviewThreadLife = 30 * 24 * time.Hour

type ChannelPost struct {
	Workspace string    `json:"workspace"`
	Channel   string    `json:"channel"`
	TS        string    `json:"ts"`
	Refs      []string  `json:"refs"`
	At        time.Time `json:"at"`
	FixedAt   time.Time `json:"fixedAt,omitzero"`
}

var reviewThreadsMu sync.Mutex

func reviewThreadsPath(dir string) string {
	return filepath.Join(dir, "watch", "review-threads.json")
}

func loadChannelPosts(dir string) []ChannelPost {
	var list []ChannelPost
	raw, err := os.ReadFile(reviewThreadsPath(dir))
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(raw, &list)
	return list
}

func saveChannelPosts(dir string, list []ChannelPost) error {
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(reviewThreadsPath(dir)), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(reviewThreadsPath(dir), raw, 0o600)
}

func RememberChannelPost(dir string, t ChannelPost, now time.Time) error {
	reviewThreadsMu.Lock()
	defer reviewThreadsMu.Unlock()
	var kept []ChannelPost
	for _, old := range loadChannelPosts(dir) {
		if old.Workspace == t.Workspace && old.Channel == t.Channel && old.TS == t.TS {
			t.FixedAt = old.FixedAt
			continue
		}
		if now.Sub(old.At) > reviewThreadLife {
			continue
		}
		kept = append(kept, old)
	}
	return saveChannelPosts(dir, append(kept, t))
}

func ChannelPostsNaming(dir, workspace string, refs []string) []ChannelPost {
	reviewThreadsMu.Lock()
	defer reviewThreadsMu.Unlock()
	wanted := map[string]bool{}
	for _, r := range refs {
		wanted[r] = true
	}
	var out []ChannelPost
	for _, t := range loadChannelPosts(dir) {
		if t.Workspace != workspace {
			continue
		}
		for _, r := range t.Refs {
			if wanted[r] {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func MarkChannelPostFixed(dir string, t ChannelPost, now time.Time) error {
	reviewThreadsMu.Lock()
	defer reviewThreadsMu.Unlock()
	list := loadChannelPosts(dir)
	for i := range list {
		if list[i].Workspace == t.Workspace && list[i].Channel == t.Channel && list[i].TS == t.TS {
			list[i].FixedAt = now
		}
	}
	return saveChannelPosts(dir, list)
}

func NewestChannelPostNaming(dir, workspace, ref string) (ChannelPost, bool) {
	var newest ChannelPost
	for _, t := range ChannelPostsNaming(dir, workspace, []string{ref}) {
		if slackTSNewer(t.TS, newest.TS) {
			newest = t
		}
	}
	return newest, newest.TS != ""
}

func PullRefsIn(links []string) []string {
	var refs []string
	for _, link := range links {
		if ref := PullRef(link); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}
