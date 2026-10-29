package cmd

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils/agent/idea"

	"github.com/russross/blackfriday/v2"
)

var (
	videoExt = map[string]bool{".mp4": true, ".mov": true, ".m4v": true, ".webm": true}
	imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
	// The targets of ![...](...) and [...](...) in a page.
	mediaLink = regexp.MustCompile(`\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
)

func mediaKind(target string) (video, image bool) {
	u, err := url.Parse(target)
	if err != nil {
		return false, false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	return videoExt[ext], imageExt[ext]
}

func isRelative(target string) bool {
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "" && u.Host == "" && !strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "#")
}

// mediaRenderer draws pictures and videos: a video as a player, and a file
// the page names by a relative path as data-file, which the page points at
// its own copy with its key.
type mediaRenderer struct{ *blackfriday.HTMLRenderer }

func (m mediaRenderer) RenderNode(w io.Writer, node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
	if node.Type != blackfriday.Image || !entering {
		return m.HTMLRenderer.RenderNode(w, node, entering)
	}
	dest := string(node.LinkData.Destination)
	video, image := mediaKind(dest)
	relative := isRelative(dest)
	web := strings.HasPrefix(dest, "https://") || strings.HasPrefix(dest, "http://")
	alt := html.EscapeString(nodeText(node))
	if !relative && !web {
		// No other scheme reaches a page someone was sent: the words alone.
		_, _ = io.WriteString(w, alt)
		return blackfriday.SkipChildren
	}
	if !video && !image && relative {
		return m.HTMLRenderer.RenderNode(w, node, entering)
	}
	src := `src="` + html.EscapeString(dest) + `"`
	if relative {
		name, _ := url.PathUnescape(strings.SplitN(dest, "?", 2)[0])
		src = `data-file="` + html.EscapeString(idea.MediaName(name)) + `"`
	}
	if video {
		fmt.Fprintf(w, `<video %s controls playsinline preload="metadata" title="%s"></video>`, src, alt)
	} else {
		fmt.Fprintf(w, `<img %s alt="%s" loading="lazy" />`, src, alt)
	}
	return blackfriday.SkipChildren
}

func nodeText(n *blackfriday.Node) string {
	var b strings.Builder
	n.Walk(func(c *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		if entering && c.Type == blackfriday.Text {
			b.Write(c.Literal)
		}
		return blackfriday.GoToNext
	})
	return b.String()
}

// MaxMedia is the biggest picture or video a shared file takes along.
const MaxMedia = 40 << 20

// copyPageMedia takes the pictures and videos a shared file names by a
// relative path onto the page, so people with the link see them. Only
// media files, and only from inside root - a page cannot carry anything
// else off the machine.
func copyPageMedia(id *idea.Idea, sourceDir, root string) (copied, skipped []string) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, m := range mediaLink.FindAllStringSubmatch(id.Doc, -1) {
		target := strings.SplitN(m[1], "?", 2)[0]
		if !isRelative(target) || seen[target] {
			continue
		}
		seen[target] = true
		video, image := mediaKind(target)
		if !video && !image {
			continue
		}
		unescaped, err := url.PathUnescape(target)
		if err != nil {
			continue
		}
		path, err := filepath.EvalSymlinks(filepath.Join(sourceDir, filepath.FromSlash(unescaped)))
		if err != nil {
			skipped = append(skipped, target+" (not found)")
			continue
		}
		if rel, err := filepath.Rel(rootReal, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			skipped = append(skipped, target+" (outside the repository)")
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > MaxMedia {
			skipped = append(skipped, fmt.Sprintf("%s (over %d MB)", target, MaxMedia>>20))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := id.PutMedia(filepath.Base(path), data); err != nil {
			skipped = append(skipped, target+" ("+err.Error()+")")
			continue
		}
		copied = append(copied, target)
	}
	return copied, skipped
}
