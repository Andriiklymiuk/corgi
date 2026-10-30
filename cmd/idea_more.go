package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/idea"
	"github.com/spf13/cobra"
)

// ---- sign-off ---------------------------------------------------------------

type approveResult struct {
	Idea      idea.Meta `json:"idea"`
	Approvers []string  `json:"approvers"`
	Needed    int       `json:"needed"`
	Approved  bool      `json:"approved"`
	// JustApproved says this sign-off is the one that made it approved.
	JustApproved bool       `json:"justApproved"`
	Build        *ideaBuild `json:"build,omitempty"`
	Why          string     `json:"why,omitempty"`
}

// approveIdea signs the page off for by (or takes it back). The sign-off that
// makes the page approved hands it to the agents when the page asks for it -
// and only with the owner's own sign-off among them, so a leaked link cannot
// start a build. owner says it comes from this machine or a paired device.
func approveIdea(dir, slug, by string, undo, build, owner bool) (approveResult, error) {
	var made bool
	now := time.Now()
	id, err := idea.Update(dir, slug, func(id *idea.Idea) error {
		if undo {
			id.Unapprove(by)
			return nil
		}
		var err error
		made, err = id.Approve(by, owner, now)
		return err
	})
	if err != nil {
		return approveResult{}, err
	}
	res := approveResult{Idea: id.Meta, Approvers: id.Meta.Approvers(), Needed: id.Meta.NeededApprovals(), Approved: id.Meta.IsApproved(), JustApproved: made}
	if made && id.Meta.BuildWhenApproved && !id.Meta.OwnerApproved() {
		res.Why = "approved, but not built: the build starts with your own sign-off among them (corgi idea approve " + slug + ")"
		made = false
		res.JustApproved = false
	}
	switch {
	case undo:
		ideaNotify(dir, id.Meta, "sign-off taken back by "+by, "")
	case made:
		ideaNotify(dir, id.Meta, "approved", strings.Join(res.Approvers, ", ")+" signed it off")
	default:
		ideaNotify(dir, id.Meta, fmt.Sprintf("signed off by %s (%d of %d)", by, len(res.Approvers), res.Needed), "")
	}
	if made && build && id.Meta.BuildWhenApproved {
		b, err := buildIdea(dir, slug, "", "", 1, true)
		if err != nil {
			res.Why = err.Error()
		} else {
			res.Build = &b
		}
	}
	return res, nil
}

// ---- decisions --------------------------------------------------------------

func decisionsText(ds []idea.Decision) string {
	var b strings.Builder
	for i, d := range ds {
		fmt.Fprintf(&b, "%d. %s", i+1, d.Text)
		if d.Comment > 0 {
			fmt.Fprintf(&b, " (thread %d)", d.Comment)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---- the clickable mockup ---------------------------------------------------

const mockSoul = `You are a product designer who also writes front-end code. From the page below, draw a clickable mockup of the product's screens as ONE self-contained HTML file: inline CSS and JavaScript only, no external scripts, fonts, images or network calls (draw icons as inline SVG, pictures as plain shapes). Show every screen or surface the page names, each as a section with the screen's name as a small label above it, laid out at phone width (390px) unless the page is clearly a desktop or dashboard product. Make it clickable: buttons and links move between screens (show one screen at a time, with a small row of screen names at the top to jump between them), forms accept typing, lists hold realistic sample data in the product's own words (names, prices, times - never lorem ipsum). Use one calm palette and a system font; it should look like a real early design, not a wireframe and not a template. When notes from reviewers are given, change the mockup the way they ask and keep the rest. Answer with the HTML only, starting with <!doctype html>, no prose and no code fence.`

var ideaMocking sync.Map

// mockIdea has the agent draw the page's screens as one clickable HTML file,
// kept beside the page as files/mock.html. notes are reviewers' remarks on
// the mockup to fold into the next drawing.
func mockIdea(dir, slug, model, notes string) error {
	if _, busy := ideaMocking.LoadOrStore(slug, true); busy {
		return fmt.Errorf("the mockup of %s is being drawn already", slug)
	}
	defer ideaMocking.Delete(slug)
	id, err := idea.Load(dir, slug)
	if err != nil {
		return err
	}
	if model == "" {
		model = firstNonEmpty(id.Meta.Model, ideaModel)
	}
	runIn := ideaWorkspaceRoot(id.Meta.Workspace)
	if runIn == "" {
		runIn = mustCwd()
	}
	_, _ = idea.Update(dir, slug, func(id *idea.Idea) error { id.Meta.Mocking = true; return nil })
	done := func() {
		_, _ = idea.Update(dir, slug, func(id *idea.Idea) error { id.Meta.Mocking = false; return nil })
	}
	prompt := "# The page\n\n" + id.Doc
	if notes = strings.TrimSpace(notes); notes != "" {
		prompt += "\n\n# Notes on the last mockup\n\n" + notes
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*ideaTimeout)
	defer cancel()
	out, err := runIdeaPrint(ctx, runIn, model, mockSoul, prompt)
	if err != nil {
		done()
		return err
	}
	html, err := mockHTML(out)
	if err != nil {
		done()
		return err
	}
	if err := id.PutMedia(idea.MockName, []byte(html)); err != nil {
		done()
		return err
	}
	updated, err := idea.Update(dir, slug, func(id *idea.Idea) error {
		id.Meta.Mocking, id.Meta.Mock, id.Meta.MockAt = false, idea.MockName, time.Now()
		return nil
	})
	if err != nil {
		return err
	}
	ideaNotify(dir, updated.Meta, "the mockup is drawn", "open the page and press the screens")
	nudgeDaemon(dir)
	return nil
}

func mockHTML(out string) (string, error) {
	lower := strings.ToLower(out)
	start := strings.Index(lower, "<!doctype html")
	if start < 0 {
		start = strings.Index(lower, "<html")
	}
	end := strings.LastIndex(lower, "</html>")
	if start < 0 || end < start {
		return "", fmt.Errorf("the agent did not draw an HTML page: %s", clipTitle(strings.ReplaceAll(out, "\n", " "), 200))
	}
	return out[start:end+len("</html>")] + "\n", nil
}

// mockNotes are the open comments on the mockup, for the next drawing.
func mockNotes(cs []idea.Comment) string {
	var b strings.Builder
	for _, c := range cs {
		if c.Anchor == nil || c.Anchor.Heading != mockHeading {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", c.By, c.Text)
		for _, r := range c.Replies {
			if r.By != idea.Agent {
				fmt.Fprintf(&b, "  - %s: %s\n", r.By, r.Text)
			}
		}
	}
	return b.String()
}

// mockHeading is the heading a comment on the mockup carries.
const mockHeading = "Mockup"

func scheduleMock(dir, slug, notes string) {
	go func() {
		if err := mockIdea(dir, slug, "", notes); err != nil {
			utils.Infof("idea %s: mockup: %v\n", slug, err)
		}
	}()
}

// ---- team templates -----------------------------------------------------------

// ideaTemplatesDir is where a team keeps its own page types, one markdown
// file each, in the repository: .corgi/idea-templates/prd.md is --type prd.
const ideaTemplatesDir = ".corgi/idea-templates"

// ideaTemplateRoots are the folders a template is looked for in: the
// workspace's, then the current folder and the ones above it.
func ideaTemplateRoots(ws string) []string {
	var roots []string
	if r := ideaWorkspaceRoot(ws); r != "" {
		roots = append(roots, r)
	}
	if cwd, err := os.Getwd(); err == nil {
		for d := cwd; ; d = filepath.Dir(d) {
			roots = append(roots, d)
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return roots
}

// ideaTemplate is the team's template for that type, and where it lives.
func ideaTemplate(name string, roots []string) (string, string) {
	if !idea.ValidTypeName(name) || idea.ValidType(name) {
		return "", ""
	}
	for _, r := range roots {
		p := filepath.Join(r, ideaTemplatesDir, name+".md")
		if data, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(data)) != "" {
			return string(data), p
		}
	}
	return "", ""
}

type ideaTemplateRow struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

// ideaTemplates is every type a page can be: the built-in ones and the
// team's own.
func ideaTemplates(roots []string) []ideaTemplateRow {
	var out []ideaTemplateRow
	for _, t := range idea.Types {
		out = append(out, ideaTemplateRow{Type: t})
	}
	seen := map[string]bool{}
	for _, r := range roots {
		entries, err := os.ReadDir(filepath.Join(r, ideaTemplatesDir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".md")
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || seen[name] || idea.ValidType(name) || !idea.ValidTypeName(name) {
				continue
			}
			seen[name] = true
			out = append(out, ideaTemplateRow{Type: name, Path: filepath.Join(r, ideaTemplatesDir, e.Name())})
		}
	}
	return out
}

func templateSoul(template string) string {
	return `You are a staff engineer writing a page in this team's own format. Someone tells you what it is about in a sentence or two; the repository picture, when given, tells you what exists, so build on it and name real files and services. Give it a short name (two or three words), 3-6 tags (lowercase, one word each), a one-sentence summary, and the page in markdown that follows the team's template below: its headings, in its order, each one written through. Lines in the template that tell the writer what to put there are instructions - follow them and leave them out of the page; placeholders are filled, never copied.

--- The team's template ---
` + template + `
--- End of the template ---
` + ideaJSONTail
}

// ---- the daily digest -------------------------------------------------------

// ideaDigestLines are the pages that wait on a person: proposals nobody
// took, answers nobody closed, sign-offs still missing.
func ideaDigestLines(dir string) []string {
	type row struct {
		line string
		at   time.Time
	}
	var rows []row
	for _, m := range idea.List(dir) {
		if m.Status == idea.Built {
			continue
		}
		id, err := idea.Load(dir, m.Slug)
		if err != nil {
			continue
		}
		proposals, answered := 0, 0
		for _, c := range id.Comments.Comments {
			switch {
			case c.Kind == idea.KindProposal && c.Status == idea.StatusOpen && lastAuthorIsAgent(c):
				proposals++
			case c.Status == idea.StatusAnswered || c.Status == idea.StatusFixed:
				answered++
			}
		}
		var parts []string
		if answered > 0 {
			parts = append(parts, fmt.Sprintf("%d answered, to close", answered))
		}
		if proposals > 0 {
			parts = append(parts, fmt.Sprintf("%d proposal(s) to take or leave", proposals))
		}
		if m.Status == idea.Draft && len(m.Approvals) > 0 && !m.IsApproved() {
			parts = append(parts, fmt.Sprintf("signed off %d of %d", len(m.Approvers()), m.NeededApprovals()))
		}
		if len(parts) == 0 {
			continue
		}
		rows = append(rows, row{fmt.Sprintf("💡 %s: %s", firstNonEmpty(m.Name, m.Title), strings.Join(parts, ", ")), m.UpdatedAt})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
	var out []string
	for i, r := range rows {
		if i == 5 {
			out = append(out, fmt.Sprintf("💡 and %d more page(s) - corgi idea list", len(rows)-5))
			break
		}
		out = append(out, r.line)
	}
	return out
}

func lastAuthorIsAgent(c idea.Comment) bool {
	if len(c.Replies) == 0 {
		return c.By == idea.Agent
	}
	return c.Replies[len(c.Replies)-1].By == idea.Agent
}

// ---- commands ---------------------------------------------------------------

var ideaApproveCmd = &cobra.Command{
	Use:   "approve <slug>",
	Short: "Sign the page off: once enough people have, it is approved (and built, when it asks for that)",
	Long: `  corgi idea approve checkout
  corgi idea approve checkout --by max
  corgi idea approve checkout --undo
  corgi idea set checkout --approvals 2 --build-when-approved on

A sign-off is for the page as it reads now: a change to the text after it
asks for the sign-offs again. The browser page has the same button.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		by, _ := cmd.Flags().GetString("by")
		undo, _ := cmd.Flags().GetBool("undo")
		if by == "" {
			by = osUserName()
		}
		res, err := approveIdea(dir, args[0], by, undo, true, true)
		if err != nil {
			exitWithError("idea", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(res)
			return
		}
		state := "not yet approved"
		if res.Approved {
			state = "approved"
		}
		fmt.Printf("%s: %s · signed off by %s (%d of %d)\n", args[0], state, firstNonEmpty(strings.Join(res.Approvers, ", "), "nobody"), len(res.Approvers), res.Needed)
		switch {
		case res.Build != nil:
			fmt.Printf("handed to the agents: %s, %d task(s)\n", res.Build.Plan.Ref(), len(res.Build.Tasks))
		case res.Why != "":
			fmt.Printf("not built: %s\n", res.Why)
		}
	},
}

var ideaDecideCmd = &cobra.Command{
	Use:   "decide <slug> [\"<what was decided>\"]",
	Short: "The page's decisions: list them, add one, reword the one a thread left, or drop one",
	Long: `Resolving a thread writes its decision on its own: the first sentence of
the agent's answer, or "Not doing: …" for a proposal nobody took.

  corgi idea decide checkout                                  the list
  corgi idea decide checkout "Apple Pay ships first"           add one
  corgi idea decide checkout "Card form stays" --thread 4      reword thread 4's
  corgi idea decide checkout --drop 2                          drop the second`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		thread, _ := cmd.Flags().GetInt("thread")
		drop, _ := cmd.Flags().GetInt("drop")
		by, _ := cmd.Flags().GetString("by")
		if by == "" {
			by = osUserName()
		}
		var id *idea.Idea
		var err error
		switch {
		case drop > 0:
			id, err = idea.Update(dir, args[0], func(id *idea.Idea) error { return id.Undecide(drop) })
		case len(args) == 2:
			id, err = idea.Update(dir, args[0], func(id *idea.Idea) error {
				_, err := id.Decide(args[1], by, thread, time.Now())
				return err
			})
		default:
			id, err = idea.Load(dir, args[0])
		}
		if err != nil {
			exitWithError("idea", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"decisions": id.Meta.Decisions})
			return
		}
		if len(id.Meta.Decisions) == 0 {
			fmt.Println("no decisions yet - resolving a thread writes one")
			return
		}
		fmt.Print(decisionsText(id.Meta.Decisions))
	},
}

var ideaMockCmd = &cobra.Command{
	Use:   "mock <slug>",
	Short: "The agent draws the page's screens as one clickable HTML mockup, shown on the page",
	Long: `  corgi idea mock checkout
  corgi idea mock checkout --open       open it in the browser after

The mockup sits beside the page (files/mock.html) and under it in the
browser, in a sandbox. Comments on it ("Comment on the mockup") are read
by the agent the next time it draws.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		model, _ := cmd.Flags().GetString("model")
		open, _ := cmd.Flags().GetBool("open")
		id, err := idea.Load(dir, args[0])
		if err != nil {
			exitWithError("idea", err, 2)
		}
		utils.Infof("drawing the mockup…\n")
		if err := mockIdea(dir, args[0], model, mockNotes(id.Comments.Comments)); err != nil {
			exitWithError("idea", err, 1)
		}
		path := filepath.Join(id.Dir, idea.FilesDir, idea.MockName)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"mock": path, "link": ideaLink(dir, id.Meta, true)})
			return
		}
		fmt.Printf("mockup: %s\n", path)
		if link := ideaLink(dir, id.Meta, true); link != "" {
			fmt.Printf("on the page: %s\n", link)
		}
		if open {
			_ = openInBrowser("file://" + path)
		}
	},
}

var ideaTemplatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "The page types: idea, spec, hld, rfc, and the team's own from .corgi/idea-templates/",
	Long: `A team's own page type is a markdown file in the repository:

  .corgi/idea-templates/prd.md          corgi idea new "…" --type prd

The file is the shape: its headings, in order, with a line under each saying
what goes there. The agent writes the page in that shape.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ws, _ := cmd.Flags().GetString("workspace")
		rows := ideaTemplates(ideaTemplateRoots(ws))
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"types": rows})
			return
		}
		for _, r := range rows {
			if r.Path == "" {
				fmt.Printf("%-12s built in\n", r.Type)
			} else {
				fmt.Printf("%-12s %s\n", r.Type, r.Path)
			}
		}
		if len(rows) == len(idea.Types) {
			fmt.Println("add your own: " + ideaTemplatesDir + "/<type>.md in the repository")
		}
	},
}

func init() {
	ideaApproveCmd.Flags().String("by", "", "your name on the sign-off (default: $USER)")
	ideaApproveCmd.Flags().Bool("undo", false, "take your sign-off back")
	ideaDecideCmd.Flags().Int("thread", 0, "the thread the decision belongs to (replaces the line it left)")
	ideaDecideCmd.Flags().Int("drop", 0, "drop the decision at this place in the list")
	ideaDecideCmd.Flags().String("by", "", "who decided (default: $USER)")
	ideaMockCmd.Flags().String("model", "", "the designer's model (default: the page's)")
	ideaMockCmd.Flags().Bool("open", false, "open the mockup in the browser")
	ideaTemplatesCmd.Flags().String("workspace", "", "look in this workspace's repository too")
	ideaCmd.AddCommand(ideaApproveCmd, ideaDecideCmd, ideaMockCmd, ideaTemplatesCmd)
}
