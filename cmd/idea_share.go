package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/idea"

	"github.com/spf13/cobra"
)

var ideaShareCmd = &cobra.Command{
	Use:   "share <slug | file.md> [--off] [--rotate]",
	Short: "The link for the team: anyone with it reads and comments, the agent answers. A markdown file becomes a page first",
	Long: `  corgi idea share docs/specs/checkout.md
      the file becomes a page (kept word for word), and the link for the
      team is printed and copied; share the same file again after editing
      it and the same page is updated, the link stays
  corgi idea share checkout
      the link of a page that is already here
  corgi idea share checkout --off
      close it: the link stops working
  corgi idea share checkout --rotate
      a new link: the old one stops working
  corgi idea share checkout --for 7d
      the link stops working in a week (--for 0 keeps it open)
  corgi idea share checkout --read-only
      people read, nobody writes (--read-only=false opens comments again)

People who open the link read the page, highlight words and comment. Each
comment reaches the agent, which answers it and amends the page; you hear
about every comment on the desktop and the phone.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		off, _ := cmd.Flags().GetBool("off")
		rotate, _ := cmd.Flags().GetBool("rotate")
		typ, _ := cmd.Flags().GetString("type")
		ws, _ := cmd.Flags().GetString("workspace")
		noCopy, _ := cmd.Flags().GetBool("no-copy")
		if typ != "" && !idea.ValidTypeName(typ) {
			exitWithError("idea", fmt.Errorf("--type is one of %s", strings.Join(idea.Types, ", ")), 2)
		}
		req := shareRequest{Target: args[0], Type: typ, Workspace: ws, By: osUserName(), Off: off, Rotate: rotate}
		if cmd.Flags().Changed("for") {
			raw, _ := cmd.Flags().GetString("for")
			d, err := parseLinkFor(raw)
			if err != nil {
				exitWithError("idea", err, 2)
			}
			req.For = &d
		}
		if cmd.Flags().Changed("read-only") {
			ro, _ := cmd.Flags().GetBool("read-only")
			req.ReadOnly = &ro
		}
		res, err := shareIdea(dir, req, time.Now())
		if err != nil {
			exitWithError("idea", err, 1)
		}
		copied := false
		if res.Link != "" && !noCopy && !utils.JSONOutput {
			copied = copyToClipboard(res.Link)
		}
		if utils.JSONOutput {
			utils.PrintJSON(res)
			return
		}
		res.print(copied)
	},
}

type shareRequest struct {
	Target    string // a slug, or a markdown file
	Markdown  string // or the page's text, with Title
	Title     string
	Type      string
	Workspace string
	By        string
	Off       bool
	Rotate    bool
	// For, when set, is how long the link works from now; 0 lifts a limit.
	For *time.Duration
	// ReadOnly, when set, makes the link read-only or opens it to comments.
	ReadOnly *bool
}

type shareResult struct {
	Media    []string  `json:"media,omitempty"`
	Skipped  []string  `json:"skipped,omitempty"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Open     bool      `json:"open"`
	Link     string    `json:"link"`
	Public   bool      `json:"public"`
	Created  bool      `json:"created,omitempty"`
	Updated  bool      `json:"updated,omitempty"`
	File     string    `json:"file"`
	Hint     string    `json:"hint,omitempty"`
	Until    time.Time `json:"until,omitempty"`
	ReadOnly bool      `json:"readOnly,omitempty"`
}

func shareIdea(dir string, req shareRequest, now time.Time) (shareResult, error) {
	slug, created, updated, err := pageToShare(dir, req, now)
	if err != nil {
		return shareResult{}, err
	}
	var media, skipped []string
	if page, err := idea.Load(dir, slug); err == nil && page.Meta.Source != "" {
		src := filepath.Dir(page.Meta.Source)
		media, skipped = copyPageMedia(page, src, repoRootOf(src))
	}
	id, err := idea.Update(dir, slug, func(id *idea.Idea) error {
		switch {
		case req.Off:
			id.Meta.Key = ""
		case req.Rotate || id.Meta.Key == "" || !id.Meta.LinkOpen(now):
			id.Meta.Key = idea.NewKey()
			id.Meta.KeyUntil = time.Time{}
		}
		if req.For != nil {
			id.Meta.KeyUntil = time.Time{}
			if *req.For > 0 {
				id.Meta.KeyUntil = now.Add(*req.For)
			}
		}
		if req.ReadOnly != nil {
			id.Meta.ReadOnly = *req.ReadOnly
		}
		return nil
	})
	if err != nil {
		return shareResult{}, err
	}
	res := shareResult{Slug: id.Meta.Slug, Name: firstNonEmpty(id.Meta.Name, id.Meta.Title), Open: id.Meta.Key != "",
		Created: created, Updated: updated, File: filepath.Join(id.Dir, idea.DocName), Media: media, Skipped: skipped,
		Until: id.Meta.KeyUntil, ReadOnly: id.Meta.ReadOnly}
	if !res.Open {
		return res, nil
	}
	local, public := ideaLinkBases()
	res.Link = ideaLinkFrom(local, public, id.Meta, false)
	res.Public = public != ""
	switch {
	case res.Link == "":
		res.Hint = "corgi agent up first - the link appears once the dashboard runs"
	case !res.Public:
		res.Hint = "this link works on this Mac only - corgi agent up gives it a public address the team can open"
	}
	return res, nil
}

// pageToShare finds or makes the page: an existing slug, or a markdown
// file (or text) kept word for word, updated in place when shared again.
func pageToShare(dir string, req shareRequest, now time.Time) (slug string, created, updated bool, err error) {
	target := strings.TrimSpace(req.Target)
	if req.Markdown == "" && idea.ValidSlug(target) {
		if _, err := idea.Load(dir, target); err == nil {
			return target, false, false, nil
		}
	}
	text, source := req.Markdown, ""
	if text == "" {
		path, err := filepath.Abs(target)
		if err != nil {
			return "", false, false, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, false, fmt.Errorf("%s is neither a page here nor a file - corgi idea list shows the pages", target)
			}
			return "", false, false, err
		}
		text, source = string(data), path
	}
	if strings.TrimSpace(text) == "" {
		return "", false, false, fmt.Errorf("the page is empty")
	}
	text = strings.TrimRight(text, "\n") + "\n"
	if source != "" {
		for _, m := range idea.List(dir) {
			if m.Source != source {
				continue
			}
			changed := false
			_, err := idea.Update(dir, m.Slug, func(id *idea.Idea) error {
				if id.Doc == text {
					return nil
				}
				changed = true
				id.ChangeBy, id.ChangeNote = firstNonEmpty(req.By, "someone"), "updated from "+filepath.Base(source)
				return id.SetDoc(text, now)
			})
			return m.Slug, false, changed, err
		}
	}
	title := firstNonEmpty(strings.TrimSpace(req.Title), firstHeading(text))
	if title == "" && source != "" {
		title = strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	}
	if title == "" {
		title = "page"
	}
	typ := req.Type
	if !idea.ValidTypeName(typ) {
		typ = idea.TypeSpec
	}
	ws := req.Workspace
	if ws == "" && source != "" {
		_, ws = workspaceRootFor(filepath.Dir(source))
	}
	if ws != "" {
		if _, err := workspaceRoot(ws); err != nil {
			return "", false, false, err
		}
	}
	meta := idea.Meta{Type: typ, Name: title, Workspace: ws, Source: source, By: firstNonEmpty(req.By, "someone"), Auto: true}
	id, err := idea.Create(dir, "", title, text, meta, now)
	if err != nil {
		return "", false, false, err
	}
	nudgeDaemon(dir)
	return id.Meta.Slug, true, false, nil
}

// repoRootOf is the git checkout dir sits in, or dir itself.
func repoRootOf(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

func (r shareResult) print(copied bool) {
	switch {
	case r.Created:
		fmt.Printf("%s - a new page from the file\n", r.Name)
	case r.Updated:
		fmt.Printf("%s - updated from the file, same link\n", r.Name)
	default:
		fmt.Println(r.Name)
	}
	if len(r.Media) > 0 {
		fmt.Printf("  %d picture(s) and video(s) go with it\n", len(r.Media))
	}
	for _, s := range r.Skipped {
		fmt.Println("  left out: " + s)
	}
	if !r.Open {
		fmt.Println("  closed: only this Mac and paired phones read it now")
		return
	}
	if r.Link != "" {
		fmt.Println("  " + r.Link)
	}
	if copied {
		fmt.Println("  copied")
	}
	if !r.Until.IsZero() {
		fmt.Printf("  works until %s\n", r.Until.Local().Format("Mon 2 Jan 15:04"))
	}
	if r.Hint != "" {
		fmt.Println("  " + r.Hint)
		return
	}
	if r.ReadOnly {
		fmt.Println("  anyone with the link reads it; nobody can comment (--read-only=false opens comments)")
		return
	}
	fmt.Println("  anyone with the link reads and comments; the agent answers each comment and amends the page")
}

var copyToClipboard = func(text string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	c := exec.Command("pbcopy")
	c.Stdin = strings.NewReader(text)
	return c.Run() == nil
}

// parseLinkFor reads how long a link works: 30m, 12h, 7d; 0 for no limit.
func parseLinkFor(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "0" || s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && strings.HasSuffix(s, "d") && n > 0 {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("--for is like 30m, 12h or 7d")
	}
	return d, nil
}
