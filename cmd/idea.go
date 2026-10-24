package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

// An idea is one page for one thing you want built - "a scheduling app for
// a coffee shop" - written by the agent from a sentence, read by you and
// whoever you send the link to, argued over in comments on highlighted
// words, answered and amended by the agent, and handed back to the agent
// to build when it reads right. The page is markdown in the agent
// directory; the comments and every answer sit beside it in JSON.

const ideaTimeout = 5 * time.Minute

const ideaModel = "sonnet"

const ideaSoul = `You are a product-minded staff engineer. Someone tells you in a sentence what they want built. Write the page a small team and a fleet of coding agents would build from: a name for the project (short, memorable, one or two words), 3-6 tags (lowercase, one word each), a one-sentence summary, and the page itself in markdown. The page has these sections, in this order, each short and concrete: "## What it is" (the pitch, who it is for), "## Users and jobs" (who uses it and what they come to do), "## Screens" (one line per screen or surface), "## Data" (the entities and their fields), "## Infra" (a specific stack choice and why, hosting, auth, payments if any - name real tools), "## Open questions" (what you had to guess), "## Tasks" (a checklist "- [ ] ..." of 4-8 first tasks, each one session's work). Write in plain words, no fluff. Also offer 2-4 proposals: things worth deciding that you did not put in the page (an alternative stack, a feature to cut or add, a risk). Each proposal quotes a short exact phrase from the page it is about (copy it verbatim, under 80 characters) and says the proposal in one or two sentences. Answer with JSON only, no prose and no code fence: {"name": "...", "tags": ["..."], "summary": "...", "markdown": "...", "tasks": [{"title": "...", "body": "..."}], "proposals": [{"quote": "...", "text": "..."}]} where "tasks" repeats the page's task checklist with a title (one line, imperative, under 80 characters) and a body that says what to build and how one knows it is done.`

const ideaAnswerSoul = `You are the engineer who wrote this page and keeps it. People left comments on it: a comment quotes the words it is about (or is about the whole page) and may have replies. Answer every comment listed as waiting. When a comment asks for a change, or you agree one is right, make it: return the whole page with the change in it, and say in the reply what you changed and where (the section). When a comment is a question, answer it in the reply and leave the page alone. Be brief and specific; never pad. Keep the page's sections and headings; change only what the comments call for, and keep the "## Tasks" checklist in step with the rest. If the comments changed something big, add one or two new proposals (each quoting a short exact phrase from the new page it is about, under 80 characters). Answer with JSON only, no prose and no code fence: {"page": "<the whole page, or an empty string when nothing on it changed>", "answers": [{"id": 3, "reply": "...", "changed": true}], "proposals": [{"quote": "...", "text": "..."}]} where "changed" says that reply came with a change to the page.`

type ideaProposal struct {
	Quote string `json:"quote"`
	Text  string `json:"text"`
}

type ideaPageAnswer struct {
	Name      string         `json:"name"`
	Tags      []string       `json:"tags"`
	Summary   string         `json:"summary"`
	Markdown  string         `json:"markdown"`
	Tasks     []plannerTask  `json:"tasks"`
	Proposals []ideaProposal `json:"proposals"`
}

type ideaReply struct {
	ID      int    `json:"id"`
	Reply   string `json:"reply"`
	Changed bool   `json:"changed"`
}

type ideaCommentsAnswer struct {
	Page      string         `json:"page"`
	Answers   []ideaReply    `json:"answers"`
	Proposals []ideaProposal `json:"proposals"`
}

// runIdeaPrint is one unattended agent run in dir's workspace (claude or
// codex, whichever the workspace runs); tests swap it.
var runIdeaPrint = printIn

func jsonBody(text string) (string, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("the agent did not answer with JSON: %s", clipTitle(strings.ReplaceAll(text, "\n", " "), 200))
	}
	return text[start : end+1], nil
}

func parseIdeaPage(text string) (ideaPageAnswer, error) {
	var ans ideaPageAnswer
	body, err := jsonBody(text)
	if err != nil {
		return ans, err
	}
	if err := json.Unmarshal([]byte(body), &ans); err != nil {
		return ans, fmt.Errorf("could not read the agent's answer: %v", err)
	}
	ans.Name = clipTitle(strings.TrimSpace(ans.Name), 60)
	ans.Markdown = strings.TrimSpace(ans.Markdown)
	if ans.Markdown == "" {
		return ans, fmt.Errorf("the agent wrote no page")
	}
	ans.Tags = cleanTags(ans.Tags)
	ans.Summary = clipTitle(strings.TrimSpace(ans.Summary), 300)
	var tasks []plannerTask
	for _, t := range ans.Tasks {
		t.Title = clipTitle(strings.TrimSpace(t.Title), 200)
		if t.Title != "" {
			tasks = append(tasks, t)
		}
	}
	if len(tasks) > 12 {
		tasks = tasks[:12]
	}
	ans.Tasks = tasks
	return ans, nil
}

func parseIdeaComments(text string) (ideaCommentsAnswer, error) {
	var ans ideaCommentsAnswer
	body, err := jsonBody(text)
	if err != nil {
		return ans, err
	}
	if err := json.Unmarshal([]byte(body), &ans); err != nil {
		return ans, fmt.Errorf("could not read the agent's answer: %v", err)
	}
	ans.Page = strings.TrimSpace(ans.Page)
	return ans, nil
}

var tagPattern = regexp.MustCompile(`[^a-z0-9-]+`)

func cleanTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.Trim(tagPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(t)), "-"), "-")
		if t == "" || seen[t] || len(t) > 30 {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 8 {
			break
		}
	}
	return out
}

// anchorFor turns a proposal's quote into an anchor on the page, or nil
// when the words are not on it.
func anchorFor(doc, quote string) *idea.Anchor {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return nil
	}
	at := strings.Index(doc, quote)
	if at < 0 {
		return nil
	}
	a := &idea.Anchor{Quote: quote, Heading: idea.HeadingFor(doc, quote)}
	if at > 0 {
		from := at - 40
		if from < 0 {
			from = 0
		}
		a.Prefix = doc[from:at]
	}
	if end := at + len(quote); end < len(doc) {
		to := end + 40
		if to > len(doc) {
			to = len(doc)
		}
		a.Suffix = doc[end:to]
	}
	return a
}

func ideaWorkspaceRoot(ws string) string {
	if ws == "" {
		return ""
	}
	root, err := workspaceRoot(ws)
	if err != nil {
		return ""
	}
	return root
}

// newIdea writes the page for one sentence: the agent names it, tags it,
// writes the page, puts its first tasks on the board and leaves its
// proposals as comments for a person to take or leave.
func newIdea(dir, text, ws, model, by string, withTasks bool, now time.Time) (*idea.Idea, []watch.Task, error) {
	root := ""
	if ws != "" {
		var err error
		if root, err = workspaceRoot(ws); err != nil {
			return nil, nil, err
		}
	}
	runIn := root
	if runIn == "" {
		runIn = mustCwd()
	}
	prompt := "Build this: " + strings.TrimSpace(text) + "\n"
	if root != "" {
		prompt += "\nIt goes into an existing repository; fit its stack and conventions.\n" + repoPicture(root)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ideaTimeout)
	defer cancel()
	out, err := runIdeaPrint(ctx, runIn, model, ideaSoul, prompt)
	if err != nil {
		return nil, nil, err
	}
	ans, err := parseIdeaPage(out)
	if err != nil {
		return nil, nil, err
	}
	title := clipTitle(strings.TrimSpace(text), 120)
	meta := idea.Meta{Name: ans.Name, Tags: ans.Tags, Summary: ans.Summary, Workspace: ws, By: by, Model: model, Auto: true}
	id, err := idea.Create(dir, "", title, ans.Markdown, meta, now)
	if err != nil {
		return nil, nil, err
	}
	var made []watch.Task
	if withTasks {
		tasks := watch.LoadTasks(dir)
		for _, pt := range ans.Tasks {
			t, err := tasks.Add(pt.Title, taskBodyFor(id.Meta, pt.Body), ws, "idea", now)
			if err != nil {
				continue
			}
			t, _ = tasks.Tag(t.Key(), id.Meta.Slug, id.Meta.Tags, now)
			made = append(made, t)
			id.Meta.Tasks = append(id.Meta.Tasks, t.ID)
		}
	}
	for _, p := range ans.Proposals {
		_, _ = id.AddComment(idea.KindProposal, idea.Agent, p.Text, anchorFor(id.Doc, p.Quote), nil, nil, now)
	}
	if err := id.Save(dir); err != nil {
		return nil, nil, err
	}
	nudgeDaemon(dir)
	return id, made, nil
}

func taskBodyFor(m idea.Meta, body string) string {
	body = strings.TrimSpace(body)
	line := fmt.Sprintf("From the idea page %q (corgi idea open %s).", firstNonEmpty(m.Name, m.Title), m.Slug)
	if body == "" {
		return line
	}
	return body + "\n\n" + line
}

var ideaAnswering sync.Map

// answerIdea is one pass of the agent over what waits on it: every open
// comment by a person and every proposal a person accepted. It replies to
// each, amends the page when a comment asks for it, and says who was told.
// It returns false when nothing waited.
func answerIdea(dir, slug, model string) (bool, error) {
	if _, busy := ideaAnswering.LoadOrStore(slug, true); busy {
		return false, fmt.Errorf("the agent is already answering %s", slug)
	}
	defer ideaAnswering.Delete(slug)
	id, err := idea.Load(dir, slug)
	if err != nil {
		return false, err
	}
	waiting := id.ForAgent()
	if len(waiting) == 0 {
		return false, nil
	}
	if model == "" {
		model = firstNonEmpty(id.Meta.Model, ideaModel)
	}
	_, _ = idea.Update(dir, slug, func(id *idea.Idea) error { id.Meta.Answering = true; return nil })
	clear := func() {
		_, _ = idea.Update(dir, slug, func(id *idea.Idea) error { id.Meta.Answering = false; return nil })
	}
	runIn := ideaWorkspaceRoot(id.Meta.Workspace)
	if runIn == "" {
		runIn = mustCwd()
	}
	var ids []string
	for _, c := range waiting {
		ids = append(ids, strconv.Itoa(c.ID))
	}
	prompt := fmt.Sprintf("# The page\n\n%s\n\n# Comments waiting on you: %s\n\n%s", id.Doc, strings.Join(ids, ", "), idea.Transcript(waiting))
	if others := otherComments(id, waiting); others != "" {
		prompt += "# Earlier comments, for context (do not answer these)\n\n" + others
	}
	ctx, cancel := context.WithTimeout(context.Background(), ideaTimeout)
	defer cancel()
	out, err := runIdeaPrint(ctx, runIn, model, ideaAnswerSoul, prompt)
	if err != nil {
		clear()
		return true, err
	}
	ans, err := parseIdeaComments(out)
	if err != nil {
		clear()
		return true, err
	}
	now := time.Now()
	var told []string
	changed := false
	updated, err := idea.Update(dir, slug, func(id *idea.Idea) error {
		id.Meta.Answering = false
		if ans.Page != "" && ans.Page != strings.TrimSpace(id.Doc) {
			if err := id.SetDoc(ans.Page+"\n", now); err == nil {
				changed = true
			}
		}
		answered := map[int]bool{}
		for _, a := range ans.Answers {
			if strings.TrimSpace(a.Reply) == "" {
				continue
			}
			if _, err := id.Reply(a.ID, idea.Agent, a.Reply, a.Changed && changed, nil, nil, now); err == nil {
				answered[a.ID] = true
				told = append(told, fmt.Sprintf("%d", a.ID))
			}
		}
		for _, c := range waiting {
			if !answered[c.ID] {
				_, _ = id.Reply(c.ID, idea.Agent, "I read this but had nothing to add. Say more if you want a change.", false, nil, nil, now)
			}
		}
		for _, p := range ans.Proposals {
			if strings.TrimSpace(p.Text) != "" {
				_, _ = id.AddComment(idea.KindProposal, idea.Agent, p.Text, anchorFor(id.Doc, p.Quote), nil, nil, now)
			}
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	what := "answered"
	if changed {
		what = "updated the page and answered"
	}
	ideaNotify(dir, updated.Meta, fmt.Sprintf("%s %d comment(s)", what, len(told)), firstReplyLine(updated, ans.Answers))
	nudgeDaemon(dir)
	return true, nil
}

func firstReplyLine(id *idea.Idea, answers []ideaReply) string {
	for _, a := range answers {
		if c, ok := id.Find(a.ID); ok && strings.TrimSpace(a.Reply) != "" {
			return clipTitle(c.Text, 60) + " → " + clipTitle(a.Reply, 120)
		}
	}
	return ""
}

func otherComments(id *idea.Idea, waiting []idea.Comment) string {
	skip := map[int]bool{}
	for _, c := range waiting {
		skip[c.ID] = true
	}
	var rest []idea.Comment
	for _, c := range id.Comments.Comments {
		if !skip[c.ID] {
			rest = append(rest, c)
		}
	}
	if len(rest) > 20 {
		rest = rest[len(rest)-20:]
	}
	return idea.Transcript(rest)
}

type ideaBuild struct {
	Plan    watch.Plan   `json:"plan"`
	Tasks   []watch.Task `json:"tasks"`
	Running bool         `json:"running"`
	Why     string       `json:"why,omitempty"`
}

// buildIdea hands the page to the agents: a planner cuts it into tasks on
// the board (replacing earlier ones from this page still in Todo), a plan
// works through them, and the daemon starts the first when the workspace
// is watched with worktrees. Without that, the tasks and the plan wait.
func buildIdea(dir, slug, ws, model string, slots int, run bool) (ideaBuild, error) {
	var res ideaBuild
	id, err := idea.Load(dir, slug)
	if err != nil {
		return res, err
	}
	if ws == "" {
		ws = id.Meta.Workspace
	}
	if ws == "" {
		return res, fmt.Errorf("say where it is built: corgi idea build %s --workspace <id> (corgi agent init in that folder first)", slug)
	}
	root, err := workspaceRoot(ws)
	if err != nil {
		return res, err
	}
	if model == "" {
		model = firstNonEmpty(id.Meta.Model, planModel)
	}
	if slots < 1 {
		slots = 1
	}
	prompt := fmt.Sprintf("Goal: build what this page describes.\n\nAt most 8 tasks.\n\n%s\n\n--- The page ---\n%s", repoPicture(root), id.Doc)
	ctx, cancel := context.WithTimeout(context.Background(), ideaTimeout)
	defer cancel()
	out, err := runIdeaPrint(ctx, root, model, planSoul, prompt)
	if err != nil {
		return res, err
	}
	ans, err := parsePlannerAnswer(out, 8)
	if err != nil {
		return res, err
	}
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	for _, t := range tasks.ByIdea(slug) {
		if t.State == "Todo" {
			_, _ = tasks.Remove(t.Key())
		}
	}
	var made []watch.Task
	for _, pt := range ans.Tasks {
		t, err := tasks.Add(pt.Title, taskBodyFor(id.Meta, pt.Body), ws, "idea", now)
		if err != nil {
			return res, err
		}
		t, _ = tasks.Tag(t.Key(), slug, id.Meta.Tags, now)
		made = append(made, t)
	}
	var planTasks []watch.PlanTask
	for i, pt := range ans.Tasks {
		var after []int
		for _, n := range pt.After {
			after = append(after, made[n-1].ID)
		}
		planTasks = append(planTasks, watch.PlanTask{ID: made[i].ID, After: after})
	}
	plans := watch.LoadPlans(dir)
	p, err := plans.Add(firstNonEmpty(id.Meta.Name, id.Meta.Title)+": "+id.Meta.Title, ans.Summary, ws, model, "idea", planTasks, slots, now)
	if err != nil {
		return res, err
	}
	writePlanFile(dir, p, made)
	res.Plan, res.Tasks = p, made
	if run {
		if why := planWatchReady(dir, ws, slots); why != "" {
			res.Why = why
		} else {
			p, _ = plans.SetState(p.ID, watch.PlanRunning, now)
			res.Plan = p
			res.Running = nudgePlans(dir)
			if !res.Running {
				res.Why = "the daemon is not running - corgi agent up, then corgi agent plan run " + p.Ref()
			}
		}
	}
	var refs []string
	for _, t := range made {
		refs = append(refs, t.Ref()+" "+t.Title)
	}
	note := fmt.Sprintf("Handed to the agents as %s: %d task(s) on the board.\n%s", p.Ref(), len(made), strings.Join(refs, "\n"))
	if res.Why != "" {
		note += "\n\nNot running yet: " + res.Why
	} else if res.Running {
		note += "\n\nRunning now, " + strconv.Itoa(slots) + " at a time."
	}
	_, err = idea.Update(dir, slug, func(id *idea.Idea) error {
		id.Meta.Workspace = ws
		id.Meta.Status = idea.Building
		id.Meta.Plan = p.ID
		id.Meta.Tasks = nil
		for _, t := range made {
			id.Meta.Tasks = append(id.Meta.Tasks, t.ID)
		}
		c, err := id.AddComment(idea.KindComment, idea.Agent, note, nil, nil, nil, now)
		if err == nil {
			_, _ = id.SetStatus(c.ID, idea.StatusAnswered, now)
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	nudgeDaemon(dir)
	ideaNotify(dir, id.Meta, fmt.Sprintf("building: %d task(s) as %s", len(made), p.Ref()), res.Why)
	return res, nil
}

// ---- notifications ------------------------------------------------------

const ideaNotifyKey = "ideaNotify"

var ideaNotifyLine = regexp.MustCompile(`(?m)^ideaNotify:.*$`)

// ideaNotifyOn says whether comments and answers on idea pages ring (the
// desktop and the notifyUrl, Telegram for most). On unless turned off.
func ideaNotifyOn(dir string) bool {
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(user.IdeaNotify), "off")
}

// ideaNotify tells the person about a change on an idea page, with the
// page's link, unless idea notifications are off.
var ideaNotify = func(dir string, m idea.Meta, what, detail string) {
	if !ideaNotifyOn(dir) {
		return
	}
	title := "💡 " + firstNonEmpty(m.Name, m.Title) + ": " + what
	body := strings.TrimSpace(detail)
	link := ideaLink(dir, m, false)
	utils.NotifyWithLink(title, body, ideaLink(dir, m, true))
	if user, err := config.LoadUser(agentUserConfigPath(dir)); err == nil && user != nil && user.NotifyUrl != "" {
		if send := webhookLinkNotifier(user.NotifyUrl, nil); send != nil {
			send(title, body, link)
		}
	}
}

// ideaLink is the page's open link: through the public URL when there is
// one (so a phone or a friend can open it), else on this machine. The
// share key rides in it; a closed page has no link.
func ideaLink(dir string, m idea.Meta, preferLocal bool) string {
	if m.Key == "" {
		return ""
	}
	path := "/idea/" + m.Slug + "?k=" + m.Key
	local := ""
	if l := localLauncherURL(); l != "" {
		local = strings.TrimSuffix(l, "/app") + path
	}
	public := ""
	if l := launcherURL(); l != "" {
		public = strings.TrimSuffix(l, "/app") + path
	}
	if preferLocal && local != "" {
		return local
	}
	if public != "" {
		return public
	}
	return local
}

// ---- the commands -------------------------------------------------------

var ideaCmd = &cobra.Command{
	Use:   "idea",
	Short: "One page per thing to build: the agent writes it, you and others comment, it answers, then builds",
	Long: `An idea is one page for one thing you want built, kept by the agent.

  corgi idea new "a scheduling app for a coffee shop"
      the agent (claude or codex, whichever the workspace runs) names it,
      tags it, writes the page - users, screens, data, infra, open questions,
      first tasks - puts the tasks on the board and leaves its proposals as
      comments on the page
  corgi idea open brewshift
      the page in the browser, from the dashboard's port: highlight words
      and comment; the agent answers and amends the page as you watch. The
      link has a share key, so anyone you send it to can read and comment
  corgi idea comment brewshift "use postgres" --quote "sqlite"
      the same from a terminal (or an editor); corgi idea resolve closes
  corgi idea build brewshift --workspace api
      hand it to the agents: tasks on the board, a plan that works through
      them in worktrees, the daemon starts the first

The page is markdown (corgi idea path <slug> prints where); edit it in any
editor, the browser follows. Comments, replies, statuses and files sit
beside it in comments.json. Nothing leaves this machine but what the
dashboard's public URL already carries.

Every new comment, answer and page change rings on the desktop and the
notifyUrl (Telegram): corgi idea notify off keeps them quiet.`,
}

var ideaNewCmd = &cobra.Command{
	Use:   "new \"<what you want built>\"",
	Short: "The agent writes the page for a sentence: a name, tags, the spec, first tasks, proposals",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		text := strings.TrimSpace(strings.Join(args, " "))
		if text == "" {
			_ = cmd.Help()
			return
		}
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		model, _ := cmd.Flags().GetString("model")
		kind, _ := cmd.Flags().GetString("kind")
		by, _ := cmd.Flags().GetString("by")
		noTasks, _ := cmd.Flags().GetBool("no-tasks")
		open, _ := cmd.Flags().GetBool("open")
		if kind != "" {
			if !harness.Known(kind) {
				exitWithError("idea", fmt.Errorf("--kind is one of %s", strings.Join(harness.Names(), ", ")), 2)
			}
			kindOverride = kind
		}
		if ws == "" {
			if cwd, err := os.Getwd(); err == nil {
				_, ws = workspaceRootFor(cwd)
			}
		}
		if ws != "" {
			if _, err := workspaceRoot(ws); err != nil {
				exitWithError("idea", err, 2)
			}
		}
		if model == "" {
			model = ideaModel
		}
		utils.Infof("writing the page with %s…\n", model)
		id, tasks, err := newIdea(dir, text, ws, model, firstNonEmpty(by, "cli"), !noTasks, time.Now())
		if err != nil {
			exitWithError("idea", err, 1)
		}
		link := ideaLink(dir, id.Meta, true)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"idea": id.Meta, "tasks": tasks, "file": filepath.Join(id.Dir, idea.DocName), "link": link, "proposals": len(id.Comments.Comments)})
			return
		}
		fmt.Printf("%s - %s\n", id.Meta.Slug, firstNonEmpty(id.Meta.Name, id.Meta.Title))
		if id.Meta.Summary != "" {
			fmt.Printf("  %s\n", id.Meta.Summary)
		}
		if len(id.Meta.Tags) > 0 {
			fmt.Printf("  tags: %s\n", strings.Join(id.Meta.Tags, ", "))
		}
		for _, t := range tasks {
			fmt.Printf("  %-8s %s\n", t.Ref(), t.Title)
		}
		fmt.Printf("  page: %s\n", filepath.Join(id.Dir, idea.DocName))
		fmt.Printf("  %d proposal(s) from the agent wait on the page\n", len(id.Comments.Comments))
		if link != "" {
			fmt.Printf("  open: %s\n", link)
		} else {
			fmt.Println("  corgi agent up, then corgi idea open " + id.Meta.Slug + " to read and comment in the browser")
		}
		if open && link != "" {
			_ = openInBrowser(link)
		}
	},
}

var ideaListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every idea page, newest change first, with what waits on whom",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		rows := ideaRows(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ideas": rows})
			return
		}
		if len(rows) == 0 {
			fmt.Println("no ideas yet - corgi idea new \"<what you want built>\"")
			return
		}
		for _, r := range rows {
			fmt.Printf("%-24s %-9s %-10s %s\n", r.Slug, r.Status, fmt.Sprintf("%d open", r.Open), firstNonEmpty(r.Name, r.Title))
			if r.ForAgent > 0 {
				fmt.Printf("%-24s   %d waiting on the agent\n", "", r.ForAgent)
			}
		}
	},
}

type ideaRow struct {
	idea.Meta
	Open     int            `json:"open"`
	ForAgent int            `json:"forAgent"`
	Counts   map[string]int `json:"counts"`
	File     string         `json:"file"`
	Link     string         `json:"link,omitempty"`
}

func ideaRows(dir string) []ideaRow {
	out := []ideaRow{}
	for _, m := range idea.List(dir) {
		id, err := idea.Load(dir, m.Slug)
		if err != nil {
			continue
		}
		out = append(out, ideaRow{Meta: id.Meta, Open: id.Open(), ForAgent: len(id.ForAgent()), Counts: id.Counts(),
			File: filepath.Join(id.Dir, idea.DocName), Link: ideaLink(dir, id.Meta, true)})
	}
	return out
}

var ideaOpenCmd = &cobra.Command{
	Use:   "open <slug>",
	Short: "The page in the browser, to read, highlight and comment",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		id, err := idea.Load(dir, args[0])
		if err != nil {
			exitWithError("idea", err, 2)
		}
		link := ideaLink(dir, id.Meta, true)
		if link == "" {
			if id.Meta.Key == "" {
				exitWithError("idea", fmt.Errorf("%s is closed - corgi idea share %s opens it again", id.Meta.Slug, id.Meta.Slug), 2)
			}
			exitWithError("idea", fmt.Errorf("the dashboard is not running - corgi agent up first (the page is at %s)", filepath.Join(id.Dir, idea.DocName)), 2)
		}
		if printOnly, _ := cmd.Flags().GetBool("print"); printOnly || utils.JSONOutput {
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"link": link, "file": filepath.Join(id.Dir, idea.DocName)})
				return
			}
			fmt.Println(link)
			return
		}
		if err := openInBrowser(link); err != nil {
			fmt.Println("open this in your browser:")
			fmt.Println("  " + link)
			return
		}
		fmt.Println("opened " + link)
	},
}

var ideaPathCmd = &cobra.Command{
	Use:   "path <slug>",
	Short: "Where the page's markdown is, for an editor",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		id, err := idea.Load(dir, args[0])
		if err != nil {
			exitWithError("idea", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"file": filepath.Join(id.Dir, idea.DocName), "dir": id.Dir})
			return
		}
		fmt.Println(filepath.Join(id.Dir, idea.DocName))
	},
}

var ideaShowCmd = &cobra.Command{
	Use:   "show <slug>",
	Short: "The page and its comments, as JSON with --json or as text",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		id, err := idea.Load(dir, args[0])
		if err != nil {
			exitWithError("idea", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(ideaDocument(dir, id, true))
			return
		}
		fmt.Print(id.Doc)
		if len(id.Comments.Comments) > 0 {
			fmt.Printf("\n---\n\n%s", idea.Transcript(id.Comments.Comments))
		}
	},
}

var ideaCommentCmd = &cobra.Command{
	Use:   "comment <slug> \"<words>\"",
	Short: "Comment on the page - on highlighted words with --quote, or on the whole page",
	Long: `  corgi idea comment brewshift "use postgres" --quote "We use sqlite"
  corgi idea comment brewshift "and a waitlist?" --by maria --link https://…
  corgi idea comment brewshift "yes, do that" --reply-to 4      accept a proposal
  corgi idea comment brewshift "see the sketch" --file ./sketch.png

The agent answers on its own when the dashboard runs (corgi agent up) or
when corgi idea watch is on; corgi idea answer <slug> does one pass now.`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		slug, text := args[0], strings.TrimSpace(strings.Join(args[1:], " "))
		quote, _ := cmd.Flags().GetString("quote")
		by, _ := cmd.Flags().GetString("by")
		links, _ := cmd.Flags().GetStringArray("link")
		files, _ := cmd.Flags().GetStringArray("file")
		replyTo, _ := cmd.Flags().GetInt("reply-to")
		if by == "" {
			by = osUserName()
		}
		var attached []idea.Attachment
		var result any
		id, err := idea.Update(dir, slug, func(id *idea.Idea) error {
			for _, f := range files {
				data, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				a, err := id.AddFile(filepath.Base(f), data, mimeFor(f))
				if err != nil {
					return err
				}
				attached = append(attached, a)
			}
			now := time.Now()
			if replyTo > 0 {
				r, err := id.Reply(replyTo, by, text, false, attached, links, now)
				result = r
				return err
			}
			var anchor *idea.Anchor
			if strings.TrimSpace(quote) != "" {
				anchor = anchorFor(id.Doc, quote)
				if anchor == nil {
					anchor = &idea.Anchor{Quote: strings.TrimSpace(quote)}
				}
			}
			c, err := id.AddComment(idea.KindComment, by, text, anchor, attached, links, now)
			result = c
			return err
		})
		if err != nil {
			exitWithError("idea", err, 1)
		}
		ideaNotify(dir, id.Meta, "comment by "+by, clipTitle(text, 160))
		scheduleIdeaAnswer(dir, slug)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"comment": result, "open": id.Open(), "forAgent": len(id.ForAgent())})
			return
		}
		switch v := result.(type) {
		case idea.Comment:
			fmt.Printf("comment %d on %s (%s)\n", v.ID, slug, v.Status)
		case idea.Reply:
			fmt.Printf("reply %d under comment %d on %s\n", v.ID, replyTo, slug)
		}
		if n := len(id.ForAgent()); n > 0 && !ideaDashboardUp(dir) {
			fmt.Printf("%d waiting on the agent - corgi idea answer %s, or corgi agent up and it answers on its own\n", n, slug)
		}
	},
}

func ideaDashboardUp(dir string) bool { return localLauncherURL() != "" }

func osUserName() string {
	for _, k := range []string{"CORGI_USER", "USER", "USERNAME"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return "someone"
}

func mimeFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".md", ".txt":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

func ideaStatusCommand(use, short, status string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <slug> <comment id>",
		Short: short,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			dir := mustAgentDir()
			n, err := strconv.Atoi(strings.TrimPrefix(args[1], "#"))
			if err != nil {
				exitWithError("idea", fmt.Errorf("a comment id is a number"), 2)
			}
			var c idea.Comment
			id, err := idea.Update(dir, args[0], func(id *idea.Idea) error {
				c, err = id.SetStatus(n, status, time.Now())
				return err
			})
			if err != nil {
				exitWithError("idea", err, 1)
			}
			if status == idea.StatusOpen {
				scheduleIdeaAnswer(dir, args[0])
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"comment": c, "open": id.Open()})
				return
			}
			fmt.Printf("comment %d on %s: %s\n", c.ID, args[0], c.Status)
		},
	}
}

var ideaAnswerCmd = &cobra.Command{
	Use:   "answer <slug>",
	Short: "One pass of the agent over the comments waiting on it: replies, and the page amended where asked",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		model, _ := cmd.Flags().GetString("model")
		kind, _ := cmd.Flags().GetString("kind")
		if kind != "" {
			if !harness.Known(kind) {
				exitWithError("idea", fmt.Errorf("--kind is one of %s", strings.Join(harness.Names(), ", ")), 2)
			}
			kindOverride = kind
		}
		did, err := answerIdea(dir, args[0], model)
		if err != nil {
			exitWithError("idea", err, 1)
		}
		id, _ := idea.Load(dir, args[0])
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"answered": did, "open": id.Open(), "counts": id.Counts()})
			return
		}
		if !did {
			fmt.Println("nothing waits on the agent")
			return
		}
		fmt.Printf("answered; %d open on %s\n", id.Open(), args[0])
	},
}

var ideaWatchCmd = &cobra.Command{
	Use:   "watch [slug]",
	Short: "Keep answering: every comment that arrives on any page (or this one) gets the agent's reply",
	Long: `Runs until stopped. The dashboard (corgi agent up) does this on its own for
every page; use watch on a machine without it, or to pin the answering to
one terminal you can see.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		model, _ := cmd.Flags().GetString("model")
		every, _ := cmd.Flags().GetDuration("every")
		if every < 2*time.Second {
			every = 5 * time.Second
		}
		only := ""
		if len(args) == 1 {
			only = args[0]
		}
		fmt.Println("watching the idea pages; ctrl-c stops")
		for {
			for _, m := range idea.List(dir) {
				if only != "" && m.Slug != only {
					continue
				}
				if !m.Auto && only == "" {
					continue
				}
				did, err := answerIdea(dir, m.Slug, model)
				switch {
				case err != nil:
					utils.Infof("%s: %v\n", m.Slug, err)
				case did:
					fmt.Printf("%s answered %s\n", time.Now().Format("15:04"), m.Slug)
				}
			}
			time.Sleep(every)
		}
	},
}

var ideaBuildCmd = &cobra.Command{
	Use:   "build <slug>",
	Short: "Hand the page to the agents: tasks on the board, a plan through them, the daemon starts building",
	Long: `  corgi idea build brewshift --workspace api
  corgi idea build brewshift --slots 2          two tasks side by side
  corgi idea build brewshift --no-run           tasks and plan only; corgi agent plan run P-n starts

The workspace is a folder corgi agent init ran in. Building runs through
the same unattended runs a ticket gets - a worktree each, a pull request
at the end - so the workspace must be watched with worktrees:
corgi agent watch enable --workspace api --isolate.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		model, _ := cmd.Flags().GetString("model")
		slots, _ := cmd.Flags().GetInt("slots")
		noRun, _ := cmd.Flags().GetBool("no-run")
		kind, _ := cmd.Flags().GetString("kind")
		if kind != "" {
			if !harness.Known(kind) {
				exitWithError("idea", fmt.Errorf("--kind is one of %s", strings.Join(harness.Names(), ", ")), 2)
			}
			kindOverride = kind
		}
		utils.Infof("planning the build…\n")
		res, err := buildIdea(dir, args[0], ws, model, slots, !noRun)
		if err != nil {
			exitWithError("idea", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(res)
			return
		}
		fmt.Printf("%s: %d task(s) on the board\n", res.Plan.Ref(), len(res.Tasks))
		printPlanTasks(res.Plan, res.Tasks)
		switch {
		case res.Running:
			fmt.Printf("running - corgi agent plan status %s follows\n", res.Plan.Ref())
		case res.Why != "":
			fmt.Printf("not running yet: %s\n", res.Why)
		default:
			fmt.Printf("corgi agent plan run %s starts it\n", res.Plan.Ref())
		}
	},
}

var ideaShareCmd = &cobra.Command{
	Use:   "share <slug> [--off] [--rotate]",
	Short: "The open link (anyone with it reads and comments); --off closes the page, --rotate gives it a new key",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		off, _ := cmd.Flags().GetBool("off")
		rotate, _ := cmd.Flags().GetBool("rotate")
		id, err := idea.Update(dir, args[0], func(id *idea.Idea) error {
			switch {
			case off:
				id.Meta.Key = ""
			case rotate || id.Meta.Key == "":
				id.Meta.Key = idea.NewKey()
			}
			return nil
		})
		if err != nil {
			exitWithError("idea", err, 1)
		}
		link := ideaLink(dir, id.Meta, false)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"open": id.Meta.Key != "", "link": link})
			return
		}
		if id.Meta.Key == "" {
			fmt.Printf("%s is closed: only the dashboard and paired phones read it now\n", args[0])
			return
		}
		if link == "" {
			fmt.Println("open; corgi agent up and the link appears here")
			return
		}
		fmt.Println(link)
	},
}

var ideaNotifyCmd = &cobra.Command{
	Use:   "notify [on|off]",
	Short: "Whether comments, answers and page changes ring (desktop and the notifyUrl, Telegram for most)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		if len(args) == 0 {
			state := "on"
			if !ideaNotifyOn(dir) {
				state = "off"
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"ideaNotify": state})
				return
			}
			fmt.Printf("idea notifications: %s\n", state)
			return
		}
		want := strings.ToLower(args[0])
		if want != "on" && want != "off" {
			exitWithError("idea", fmt.Errorf("on or off"), 2)
		}
		if err := writeUserConfigLine(agentUserConfigPath(dir), ideaNotifyLine, ideaNotifyKey+": "+want); err != nil {
			exitWithError("idea", err, 1)
		}
		fmt.Printf("idea notifications %s\n", want)
	},
}

var ideaRmCmd = &cobra.Command{
	Use:   "rm <slug>",
	Short: "Remove the page, its comments and files (board tasks stay)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		id, err := idea.Load(dir, args[0])
		if err != nil {
			exitWithError("idea", err, 2)
		}
		if err := os.RemoveAll(id.Dir); err != nil {
			exitWithError("idea", err, 1)
		}
		idea.Touch(dir)
		fmt.Printf("removed %s\n", args[0])
	},
}

func init() {
	ideaNewCmd.Flags().String("workspace", "", "the workspace it goes into (default: the one you are in; none is fine)")
	ideaNewCmd.Flags().String("model", "", "the writer's model: sonnet (default), opus, haiku; a codex model for a codex workspace")
	ideaNewCmd.Flags().String("kind", "", "run the writer under this agent: claude or codex (default: the workspace's)")
	ideaNewCmd.Flags().String("by", "", "who asked (default: cli)")
	ideaNewCmd.Flags().Bool("no-tasks", false, "write the page only; no tasks on the board yet")
	ideaNewCmd.Flags().Bool("open", false, "open the page in the browser when the dashboard runs")
	ideaOpenCmd.Flags().Bool("print", false, "print the link instead of opening it")
	ideaCommentCmd.Flags().String("quote", "", "the words on the page the comment is about")
	ideaCommentCmd.Flags().String("by", "", "your name on the comment (default: $USER)")
	ideaCommentCmd.Flags().StringArray("link", nil, "a link to attach (repeatable)")
	ideaCommentCmd.Flags().StringArray("file", nil, "a file to attach (repeatable)")
	ideaCommentCmd.Flags().Int("reply-to", 0, "reply under this comment instead of starting one")
	ideaAnswerCmd.Flags().String("model", "", "the answering model (default: the page's)")
	ideaAnswerCmd.Flags().String("kind", "", "claude or codex (default: the workspace's)")
	ideaWatchCmd.Flags().String("model", "", "the answering model (default: each page's)")
	ideaWatchCmd.Flags().Duration("every", 5*time.Second, "how often to look")
	ideaBuildCmd.Flags().String("workspace", "", "where it is built (default: the page's)")
	ideaBuildCmd.Flags().String("model", "", "the planner's model (default: the page's)")
	ideaBuildCmd.Flags().String("kind", "", "claude or codex (default: the workspace's)")
	ideaBuildCmd.Flags().Int("slots", 1, "how many tasks run at once")
	ideaBuildCmd.Flags().Bool("no-run", false, "tasks and plan only; nothing starts")
	ideaShareCmd.Flags().Bool("off", false, "close the page: the link stops working")
	ideaShareCmd.Flags().Bool("rotate", false, "a new key: the old link stops working")
	ideaCmd.AddCommand(ideaNewCmd, ideaListCmd, ideaOpenCmd, ideaPathCmd, ideaShowCmd, ideaCommentCmd,
		ideaStatusCommand("resolve", "Close a comment: it is settled", idea.StatusResolved),
		ideaStatusCommand("reopen", "Open a comment again: the agent takes another look", idea.StatusOpen),
		ideaAnswerCmd, ideaWatchCmd, ideaBuildCmd, ideaShareCmd, ideaNotifyCmd, ideaRmCmd)
	rootCmd.AddCommand(ideaCmd)
}

// ---- the agent answering on its own ---------------------------------------

// ideaAnswerDelay is how long a page waits for more comments before the
// agent reads them all at once; tests shorten it.
var ideaAnswerDelay = 12 * time.Second

var ideaAnswerTimers sync.Map

// ideaAnswerFailed remembers a page whose last answer run failed (no agent
// installed, a login lapsed) so it is tried again later, not every tick.
var ideaAnswerFailed sync.Map

const ideaRetryAfter = 15 * time.Minute

// scheduleIdeaAnswer has the agent read the page's comments soon, in this
// process, when the page is set to answer on its own and the dashboard
// (or watch) runs here. A comment arriving while one is pending just
// rides along: the timer starts over so a burst is one run.
func scheduleIdeaAnswer(dir, slug string) { scheduleIdea(dir, slug, true) }

func scheduleIdea(dir, slug string, restart bool) {
	if !ideaAutoAnswerHere {
		return
	}
	if at, ok := ideaAnswerFailed.Load(slug); ok && time.Since(at.(time.Time)) < ideaRetryAfter {
		return
	}
	id, err := idea.Load(dir, slug)
	if err != nil || !id.Meta.Auto || len(id.ForAgent()) == 0 {
		return
	}
	if _, pending := ideaAnswerTimers.Load(slug); pending && !restart {
		return
	}
	t := time.AfterFunc(ideaAnswerDelay, func() {
		ideaAnswerTimers.Delete(slug)
		_, err := answerIdea(dir, slug, "")
		switch {
		case err == nil:
			ideaAnswerFailed.Delete(slug)
		case strings.Contains(err.Error(), "already answering"):
		default:
			ideaAnswerFailed.Store(slug, time.Now())
			utils.Infof("idea %s: %v\n", slug, err)
			note := "I could not answer just now: " + err.Error()
			_, _ = idea.Update(dir, slug, func(id *idea.Idea) error {
				if n := len(id.Comments.Comments); n > 0 && id.Comments.Comments[n-1].Text == note {
					return nil
				}
				c, err := id.AddComment(idea.KindComment, idea.Agent, note, nil, nil, nil, time.Now())
				if err == nil {
					_, _ = id.SetStatus(c.ID, idea.StatusAnswered, time.Now())
				}
				return nil
			})
		}
	})
	if old, loaded := ideaAnswerTimers.LoadOrStore(slug, t); loaded {
		t.Stop()
		old.(*time.Timer).Reset(ideaAnswerDelay)
	}
}

// ideaAnswerLoop is the dashboard's eye on every page: a comment that
// arrived some other way (the CLI, an editor writing comments.json, a
// phone through another route) gets answered too.
func ideaAnswerLoop(ctx context.Context, dir string, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, m := range idea.List(dir) {
				if m.Auto && !m.Answering {
					scheduleIdea(dir, m.Slug, false)
				}
			}
		}
	}
}

// ideaAutoAnswerHere is set by the process that answers comments on its
// own: the dashboard server. The CLI leaves answering to it.
var ideaAutoAnswerHere = false

func ideaHTTPStatus(err error) int {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "no idea"), strings.HasPrefix(msg, "no comment"):
		return http.StatusNotFound
	case strings.Contains(msg, "already answering"):
		return http.StatusConflict
	}
	return http.StatusBadRequest
}
