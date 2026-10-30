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

	// Pictures and videos written as HTML, the way a README centres them.
	htmlLinkedImage = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>\s*(<img\b[^>]*>)\s*</a>`)
	htmlImage       = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	htmlVideo       = regexp.MustCompile(`(?is)<video\b[^>]*>.*?</video>|<video\b[^>]*/?>`)
	htmlAttr        = regexp.MustCompile(`(?is)\b(src|alt|width)\s*=\s*["']([^"']*)["']`)
	htmlWrapper     = regexp.MustCompile(`(?i)</?(?:p|div|center|picture|span)\b[^>]*>`)
	htmlBreak       = regexp.MustCompile(`(?i)<br\s*/?>`)
	htmlSub         = regexp.MustCompile(`(?is)<sub>(.*?)</sub>`)
	widthTitle      = regexp.MustCompile(`^w=(\d{1,4})$`)
	htmlSource      = regexp.MustCompile(`(?is)<source\b[^>]*>`)
	htmlBlockOpen   = regexp.MustCompile(`(?i)<(?:div|center)\b[^>]*>`)
	htmlBlockClose  = regexp.MustCompile(`(?i)</(?:div|center)>`)
)

// htmlMediaToMarkdown turns a page's HTML pictures, videos and the tags that
// centre them into markdown, so they show: raw HTML is never rendered, and
// a README is mostly <p align="center"><img …></p>. Code is left alone.
func htmlMediaToMarkdown(md string) string {
	if !strings.Contains(md, "<") {
		return md
	}
	lines := strings.Split(md, "\n")
	fenced := false
	depth := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		inBlock := depth > 0
		if fenced || (!inBlock && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t"))) {
			continue
		}
		wrapped := htmlWrapper.MatchString(line)
		depth += len(htmlBlockOpen.FindAllString(line, -1)) - len(htmlBlockClose.FindAllString(line, -1))
		if depth < 0 {
			depth = 0
		}
		parts := strings.Split(line, "`")
		for j := 0; j < len(parts); j += 2 {
			parts[j] = htmlMediaInText(parts[j])
		}
		out := strings.Join(parts, "`")
		// Inside a centring block the lines were indented for HTML, not as code;
		// what a wrapper held is a paragraph of its own.
		if wrapped || inBlock {
			out = strings.TrimLeft(out, " \t")
		}
		if wrapped && strings.TrimSpace(out) != "" {
			out = "\n" + out + "\n"
		}
		lines[i] = out
	}
	return strings.Join(lines, "\n")
}

func htmlMediaInText(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	s = htmlLinkedImage.ReplaceAllStringFunc(s, func(m string) string {
		sub := htmlLinkedImage.FindStringSubmatch(m)
		return "[" + markdownImage(sub[2]) + "](" + sub[1] + ")"
	})
	s = htmlImage.ReplaceAllStringFunc(s, markdownImage)
	s = htmlVideo.ReplaceAllStringFunc(s, func(m string) string {
		if src := attrsOf(m)["src"]; src != "" {
			return "![](" + src + ")"
		}
		if sm := htmlSource.FindString(m); sm != "" {
			if src := attrsOf(sm)["src"]; src != "" {
				return "![](" + src + ")"
			}
		}
		return ""
	})
	s = htmlSub.ReplaceAllString(s, "*$1*")
	s = htmlBreak.ReplaceAllString(s, "  ")
	return htmlWrapper.ReplaceAllString(s, "")
}

func attrsOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range htmlAttr.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}

// markdownImage is one <img> as ![alt](src), its width carried in the title.
func markdownImage(tag string) string {
	a := attrsOf(tag)
	if a["src"] == "" || strings.ContainsAny(a["src"], " ()") {
		return ""
	}
	alt := strings.NewReplacer("[", "", "]", "").Replace(html.UnescapeString(a["alt"]))
	out := "![" + alt + "](" + a["src"]
	if w := strings.TrimSuffix(a["width"], "px"); w != "" && widthTitle.MatchString("w="+w) {
		out += ` "w=` + w + `"`
	}
	return out + ")"
}

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
	width := ""
	if m := widthTitle.FindStringSubmatch(string(node.LinkData.Title)); m != nil {
		width = ` style="width:` + m[1] + `px"`
	}
	if video {
		fmt.Fprintf(w, `<video %s controls playsinline preload="metadata" title="%s"%s></video>`, src, alt, width)
	} else {
		fmt.Fprintf(w, `<img %s alt="%s" loading="lazy"%s />`, src, alt, width)
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
	for _, m := range mediaLink.FindAllStringSubmatch(htmlMediaToMarkdown(id.Doc), -1) {
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
