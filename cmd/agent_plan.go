package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

const planTimeout = 4 * time.Minute

const planModel = "sonnet"

const planSoul = `You are a staff engineer breaking one goal into tasks for a team of coding agents. Each task is worked by one Claude Code session, alone, in its own git worktree of the repository, unattended, ending in a draft pull request. Tasks must be independent enough to run side by side unless you say one waits for another. Write between 2 and the given maximum tasks: each small enough for one session in one sitting, each with a title (one line, imperative, under 80 characters) and a body that says exactly what to change, where, and how the session knows it is done (the tests or checks to run). Say what NOT to touch when two tasks are near each other. Put shared groundwork first and let the others wait for it. Answer with JSON only, no prose and no code fence: {"summary": "one or two sentences of the approach", "tasks": [{"title": "...", "body": "...", "after": [1]}]} where "after" lists the 1-based numbers of the tasks this one waits for (omit or empty when none).`

type plannerTask struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	After []int  `json:"after,omitempty"`
}

type plannerAnswer struct {
	Summary string        `json:"summary"`
	Tasks   []plannerTask `json:"tasks"`
}

func repoPicture(root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository root: %s\n", root)
	if line := topLevelLine(root); line != "" {
		b.WriteString(line)
	}
	if line := trackedFilesLine(root); line != "" {
		b.WriteString(line)
	}
	for _, name := range []string{claudeMemoryFile, "README.md"} {
		b.WriteString(fileHead(root, name))
	}
	if b.Len() > 12000 {
		return b.String()[:12000]
	}
	return b.String()
}

func topLevelLine(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") && n != ".github" {
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 60 {
		names = append(names[:60], "…")
	}
	return fmt.Sprintf("Top level: %s\n", strings.Join(names, " "))
}

func trackedFilesLine(root string) string {
	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		return ""
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	byExt := map[string]int{}
	for _, f := range files {
		if ext := filepath.Ext(f); ext != "" {
			byExt[ext]++
		}
	}
	exts := make([]string, 0, len(byExt))
	for k := range byExt {
		exts = append(exts, k)
	}
	sort.Slice(exts, func(i, j int) bool { return byExt[exts[i]] > byExt[exts[j]] })
	if len(exts) > 8 {
		exts = exts[:8]
	}
	parts := make([]string, 0, len(exts))
	for _, e := range exts {
		parts = append(parts, fmt.Sprintf("%s %d", e, byExt[e]))
	}
	return fmt.Sprintf("Tracked files: %d (%s)\n", len(files), strings.Join(parts, ", "))
}

func fileHead(root, name string) string {
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	limit := 60
	if name == "README.md" {
		limit = 30
	}
	if len(lines) > limit {
		lines = lines[:limit]
	}
	return fmt.Sprintf("\n--- %s (head) ---\n%s\n", name, strings.Join(lines, "\n"))
}

func parsePlannerAnswer(text string, limit int) (plannerAnswer, error) {
	var ans plannerAnswer
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ans, fmt.Errorf("the planner did not answer with JSON: %s", clipTitle(strings.ReplaceAll(text, "\n", " "), 200))
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &ans); err != nil {
		return ans, fmt.Errorf("could not read the planner's answer: %v", err)
	}
	if len(ans.Tasks) == 0 {
		return ans, fmt.Errorf("the planner wrote no tasks")
	}
	if limit > 0 && len(ans.Tasks) > limit {
		ans.Tasks = ans.Tasks[:limit]
	}
	for i, t := range ans.Tasks {
		t.Title = strings.TrimSpace(t.Title)
		if t.Title == "" {
			return ans, fmt.Errorf("task %d has no title", i+1)
		}
		if len(t.Title) > 200 {
			t.Title = t.Title[:200]
		}
		t.After = validAfter(t.After, i+1, len(ans.Tasks))
		ans.Tasks[i] = t
	}
	return ans, nil
}

func validAfter(after []int, self, count int) []int {
	var out []int
	for _, n := range after {
		if n >= 1 && n <= count && n != self {
			out = append(out, n)
		}
	}
	return out
}

func writePlanFile(dir string, p watch.Plan, tasks []watch.Task) string {
	path := filepath.Join(dir, "plans", fmt.Sprintf("%s.md", p.Ref()))
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s - %s\n\n", p.Ref(), p.Goal)
	fmt.Fprintf(&b, "Workspace: %s · planned %s · %d slot(s)\n\n", p.Workspace, p.CreatedAt.Local().Format("2006-01-02 15:04"), p.Slots)
	if p.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", p.Summary)
	}
	b.WriteString("## Tasks\n\n")
	byID := map[int]watch.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	for _, pt := range p.Tasks {
		t := byID[pt.ID]
		fmt.Fprintf(&b, "### %s - %s\n\n", t.Ref(), t.Title)
		if len(pt.After) > 0 {
			var refs []string
			for _, id := range pt.After {
				refs = append(refs, fmt.Sprintf("TASK-%d", id))
			}
			fmt.Fprintf(&b, "After: %s\n\n", strings.Join(refs, ", "))
		}
		fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(t.Body))
	}
	b.WriteString("## Decisions\n\n(what changed along the way - add lines here)\n")
	_ = os.WriteFile(path, []byte(b.String()), 0o600)
	return path
}

func nudgePlans(dir string) bool {
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil || !info.Commands {
		return false
	}
	if _, err := command.Write(dir, command.Command{Action: command.ActionPlan, Source: "cli"}); err != nil {
		return false
	}
	daemon.Nudge(info)
	return true
}

func planWatchReady(dir, ws string, slots int) string {
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		return err.Error()
	}
	for _, s := range specs {
		if s.Workspace != ws {
			continue
		}
		if slots > 1 && !s.Isolate {
			return fmt.Sprintf("%d slots need worktrees: corgi agent watch enable --workspace %s --isolate, then corgi agent restart", slots, ws)
		}
		return ""
	}
	return fmt.Sprintf("%s is not watched - the daemon runs plan tasks the way it runs fixes: corgi agent watch enable --workspace %s --isolate, then corgi agent restart", ws, ws)
}

var agentPlanCmd = &cobra.Command{
	Use:   "plan \"<goal>\"",
	Short: "A planner breaks a goal into tasks on the board; the daemon works through them, a worktree each",
	Long: `One goal, handed to a planner (a short claude run on this machine, sonnet by
default) that writes 2-6 tasks on the board - what to change, where, how a
session knows it is done, which tasks wait for which. Nothing runs until you
say so; the tasks sit in Todo for you to read, edit (corgi agent task edit) or
remove.

  corgi agent plan "add rate limits to the public API"
  corgi agent plan "…" --workspace api --limit 4
  corgi agent plan "…" --run --slots 2            start at once, two tasks side by side

Running (corgi agent plan run P-1): the daemon starts each task when its turn
comes - the same unattended run a ticket gets, in a worktree of its own, the
workspace's caps, quiet hours and breaker respected - and the next when one
ends. A task that is Review (a pull request is up) or Done lets the ones after
it start. A run that failed leaves its task for you, unless the plan has --retries: then
it starts again with the reason, that many times.
The kanban shows the tasks; corgi agent plan status shows the plan.

The workspace must be watched (corgi agent watch enable --isolate) for the
daemon to run anything.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		goal := strings.TrimSpace(strings.Join(args, " "))
		if goal == "" {
			_ = cmd.Help()
			return
		}
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		model, _ := cmd.Flags().GetString("model")
		limit, _ := cmd.Flags().GetInt("max")
		slots, _ := cmd.Flags().GetInt("slots")
		run, _ := cmd.Flags().GetBool("run")
		retries, _ := cmd.Flags().GetInt("retries")
		if ws == "" {
			if cwd, err := os.Getwd(); err == nil {
				_, ws = workspaceRootFor(cwd)
			}
		}
		if ws == "" {
			exitWithError("agent_plan", fmt.Errorf("say which workspace: --workspace <id>, or run inside one"), 2)
		}
		root, err := workspaceRoot(ws)
		if err != nil {
			exitWithError("agent_plan", err, 2)
		}
		if limit < 2 || limit > 12 {
			limit = 6
		}
		if slots < 1 {
			slots = 1
		}
		if run {
			if why := planWatchReady(dir, ws, slots); why != "" {
				exitWithError("agent_plan", fmt.Errorf("%s", why), 2)
			}
		}
		if model == "" {
			model = planModel
		}
		open := watch.LoadTasks(dir)
		var openTitles []string
		for _, t := range open.Tasks {
			if t.Workspace == ws && (t.State == "Todo" || t.State == "Doing") {
				openTitles = append(openTitles, t.Ref()+" "+t.Title)
			}
		}
		prompt := fmt.Sprintf("Goal: %s\n\nAt most %d tasks.\n\n%s", goal, limit, repoPicture(root))
		if len(openTitles) > 0 {
			prompt += "\nTasks already on the board (do not repeat them): " + strings.Join(openTitles, "; ") + "\n"
		}
		utils.Infof("planning with %s…\n", model)
		ctx, cancel := context.WithTimeout(context.Background(), planTimeout)
		defer cancel()
		text, err := runClaudePrint(ctx, model, planSoul, prompt)
		if err != nil {
			exitWithError("agent_plan", err, 1)
		}
		ans, err := parsePlannerAnswer(text, limit)
		if err != nil {
			exitWithError("agent_plan", err, 1)
		}
		now := time.Now()
		var made []watch.Task
		var planTasks []watch.PlanTask
		for _, pt := range ans.Tasks {
			t, err := open.Add(pt.Title, pt.Body, ws, "plan", now)
			if err != nil {
				exitWithError("agent_plan", err, 1)
			}
			made = append(made, t)
		}
		for i, pt := range ans.Tasks {
			var after []int
			for _, n := range pt.After {
				after = append(after, made[n-1].ID)
			}
			planTasks = append(planTasks, watch.PlanTask{ID: made[i].ID, After: after})
		}
		plans := watch.LoadPlans(dir)
		p, err := plans.Add(watch.Plan{Goal: goal, Summary: ans.Summary, Workspace: ws, Model: model, By: "cli", Tasks: planTasks, Slots: slots}, now)
		if err != nil {
			exitWithError("agent_plan", err, 1)
		}
		if retries > 0 {
			p, _ = plans.Update(p.ID, now, func(p *watch.Plan) { p.Retries = clampRetries(retries) })
		}
		path := writePlanFile(dir, p, made)
		nudgeDaemon(dir)
		if run {
			p, _ = plans.SetState(p.ID, watch.PlanRunning, time.Now())
			nudgePlans(dir)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"plan": p, "tasks": made, "file": path})
			return
		}
		fmt.Printf("%s in %s: %s\n", p.Ref(), ws, goal)
		if ans.Summary != "" {
			fmt.Printf("  %s\n", ans.Summary)
		}
		printPlanTasks(p, made)
		fmt.Printf("  plan: %s\n", path)
		if run {
			fmt.Printf("running - %d at a time; corgi agent plan status %s\n", slots, p.Ref())
		} else {
			fmt.Printf("read them, then: corgi agent plan run %s [--slots N]\n", p.Ref())
		}
	},
}

func printPlanTasks(p watch.Plan, tasks []watch.Task) {
	byID := map[int]watch.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	for _, pt := range p.Tasks {
		t, ok := byID[pt.ID]
		if !ok {
			fmt.Printf("  TASK-%d  (gone from the board)\n", pt.ID)
			continue
		}
		after := ""
		if len(pt.After) > 0 {
			var refs []string
			for _, id := range pt.After {
				refs = append(refs, fmt.Sprintf("TASK-%d", id))
			}
			after = " · after " + strings.Join(refs, ", ")
		}
		fmt.Printf("  %-8s %-8s %s%s\n", t.Ref(), t.State, t.Title, after)
	}
}

var agentPlanRunCmd = &cobra.Command{
	Use:   "run <P-n>",
	Short: "Start (or resume) working through a plan's tasks",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		plans := watch.LoadPlans(dir)
		p, ok := plans.Find(args[0])
		if !ok {
			exitWithError("agent_plan", fmt.Errorf("no plan %s - corgi agent plan status lists them", args[0]), 2)
		}
		slots, _ := cmd.Flags().GetInt("slots")
		if slots > 0 {
			p, _ = plans.Update(p.ID, time.Now(), func(p *watch.Plan) { p.Slots = slots })
		}
		if cmd.Flags().Changed("retries") {
			retries, _ := cmd.Flags().GetInt("retries")
			p, _ = plans.Update(p.ID, time.Now(), func(p *watch.Plan) { p.Retries = clampRetries(retries) })
		}
		if why := planWatchReady(dir, p.Workspace, p.Slots); why != "" {
			exitWithError("agent_plan", fmt.Errorf("%s", why), 2)
		}
		p, _ = plans.SetState(p.ID, watch.PlanRunning, time.Now())
		if !nudgePlans(dir) {
			exitWithError("agent_plan", fmt.Errorf("the daemon is not running - corgi agent up, then corgi agent plan run %s again", p.Ref()), 1)
		}
		fmt.Printf("%s running - %d task(s) at a time; the kanban shows them, corgi agent plan status %s follows\n", p.Ref(), p.Slots, p.Ref())
	},
}

var agentPlanStopCmd = &cobra.Command{
	Use:   "stop <P-n>",
	Short: "Stop a plan: nothing more starts; its Todo tasks go to Canceled, a run already going finishes",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		plans := watch.LoadPlans(dir)
		p, ok := plans.Find(args[0])
		if !ok {
			exitWithError("agent_plan", fmt.Errorf("no plan %s", args[0]), 2)
		}
		tasks := watch.LoadTasks(dir)
		n := 0
		for _, id := range p.TaskIDs() {
			if t, ok := tasks.Find(fmt.Sprintf("task:%d", id)); ok && t.State == "Todo" {
				if _, err := tasks.Move(t.Ref(), "Canceled", time.Now()); err == nil {
					n++
				}
			}
		}
		_, _ = plans.SetState(p.ID, watch.PlanStopped, time.Now())
		nudgeDaemon(dir)
		fmt.Printf("%s stopped; %d task(s) canceled\n", p.Ref(), n)
	},
}

var agentPlanStatusCmd = &cobra.Command{
	Use:   "status [P-n]",
	Short: "Every plan and where its tasks stand",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		plans := watch.LoadPlans(dir)
		tasks := watch.LoadTasks(dir)
		list := plans.All()
		if len(args) == 1 {
			p, ok := plans.Find(args[0])
			if !ok {
				exitWithError("agent_plan", fmt.Errorf("no plan %s", args[0]), 2)
			}
			list = []watch.Plan{p}
		}
		if utils.JSONOutput {
			out := make([]map[string]any, 0, len(list))
			for _, p := range list {
				pr := p.Progress(tasks)
				out = append(out, map[string]any{"plan": p, "todo": pr.Todo, "doing": pr.Doing, "review": pr.Review, "done": pr.Done, "canceled": pr.Canceled})
			}
			utils.PrintJSON(map[string]any{"plans": out})
			return
		}
		if len(list) == 0 {
			fmt.Println("no plans yet - corgi agent plan \"<goal>\"")
			return
		}
		for _, p := range list {
			pr := p.Progress(tasks)
			fmt.Printf("%s  %-8s %s - %s\n", p.Ref(), p.State, p.Workspace, p.Goal)
			fmt.Printf("  %d todo · %d doing · %d review · %d done · %d canceled · %d slot(s)\n", pr.Todo, pr.Doing, pr.Review, pr.Done, pr.Canceled, p.Slots)
			var its []watch.Task
			for _, id := range p.TaskIDs() {
				if t, ok := tasks.Find(fmt.Sprintf("task:%d", id)); ok {
					its = append(its, t)
				}
			}
			printPlanTasks(p, its)
		}
	},
}

// A loop with no cap is how a factory burns a night: three more tries is plenty.
func clampRetries(n int) int {
	return max(0, min(n, 3))
}

func planGraphText(p watch.Plan, log *watch.TaskLog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s · %d at a time", p.Ref(), p.State, clipPlanGoal(p.Goal), p.Slots)
	if p.Retries > 0 {
		fmt.Fprintf(&b, " · %d retries", p.Retries)
	}
	b.WriteString("\n")
	waiting := map[int]bool{}
	for _, t := range p.Tasks {
		for _, a := range t.After {
			waiting[a] = true
		}
	}
	for _, t := range p.Tasks {
		title, state := planNode(t.ID, log)
		arrow := "start"
		if len(t.After) > 0 {
			var refs []string
			for _, a := range t.After {
				refs = append(refs, fmt.Sprintf("TASK-%d", a))
			}
			arrow = strings.Join(refs, " + ")
		}
		end := ""
		if !waiting[t.ID] {
			end = " → end"
		}
		fmt.Fprintf(&b, "  %s → TASK-%d [%s] %s%s\n", arrow, t.ID, state, title, end)
	}
	return b.String()
}

func planGraphDOT(p watch.Plan, log *watch.TaskLog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "digraph %q {\n  rankdir=LR;\n  node [shape=box];\n", p.Ref())
	for _, t := range p.Tasks {
		title, state := planNode(t.ID, log)
		fmt.Fprintf(&b, "  t%d [label=%q];\n", t.ID, fmt.Sprintf("TASK-%d %s\n%s", t.ID, state, title))
		for _, a := range t.After {
			fmt.Fprintf(&b, "  t%d -> t%d;\n", a, t.ID)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func planNode(id int, log *watch.TaskLog) (string, string) {
	t, ok := log.Find(fmt.Sprintf("task:%d", id))
	if !ok {
		return "(gone from the board)", "gone"
	}
	return t.Title, t.State
}

func clipPlanGoal(goal string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(goal), "\n")
	if len(line) > 70 {
		return line[:69] + "…"
	}
	return line
}

var agentPlanGraphCmd = &cobra.Command{
	Use:   "graph <P-n> [--dot]",
	Short: "Which task waits for which, and where each stands; --dot for Graphviz",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		p, ok := watch.LoadPlans(dir).Find(args[0])
		if !ok {
			exitWithError("agent_plan", fmt.Errorf("no plan %s - corgi agent plan status lists them", args[0]), 2)
		}
		log := watch.LoadTasks(dir)
		if dot, _ := cmd.Flags().GetBool("dot"); dot {
			fmt.Print(planGraphDOT(p, log))
			return
		}
		fmt.Print(planGraphText(p, log))
	},
}

func init() {
	agentPlanCmd.Flags().String("workspace", "", "the workspace the plan is for (default: the one you are in)")
	agentPlanCmd.Flags().String("model", "", "the planner's model: sonnet (default), opus, haiku")
	agentPlanCmd.Flags().Int("max", 6, "at most this many tasks (2-12)")
	agentPlanCmd.Flags().Int("slots", 1, "how many tasks run at once; more than one needs the workspace watched with --isolate")
	agentPlanCmd.Flags().Bool("run", false, "start working through the tasks at once")
	agentPlanCmd.Flags().Int("retries", 0, "start a failed task again this many times, at most 3")
	agentPlanRunCmd.Flags().Int("slots", 0, "how many tasks run at once (keeps the plan's when 0)")
	agentPlanRunCmd.Flags().Int("retries", 0, "start a failed task again this many times, at most 3")
	agentPlanGraphCmd.Flags().Bool("dot", false, "print Graphviz DOT")
	agentPlanCmd.AddCommand(agentPlanRunCmd, agentPlanStopCmd, agentPlanStatusCmd, agentPlanGraphCmd)
	agentCmd.AddCommand(agentPlanCmd)
}

func launchPlansHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plans := watch.LoadPlans(dir)
	tasks := watch.LoadTasks(dir)
	switch r.Method {
	case http.MethodGet:
		writeLaunchJSON(w, map[string]any{"plans": planRows(plans, tasks)})
	case http.MethodPost:
		changePlan(w, r, dir, plans, tasks)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET or POST /launch/plans")
	}
}

func planTasks(p watch.Plan, tasks *watch.TaskLog) []watch.Task {
	its := []watch.Task{}
	for _, id := range p.TaskIDs() {
		if t, ok := tasks.Find(fmt.Sprintf("task:%d", id)); ok {
			its = append(its, t)
		}
	}
	return its
}

func planRows(plans *watch.PlanLog, tasks *watch.TaskLog) []map[string]any {
	out := make([]map[string]any, 0)
	for _, p := range plans.All() {
		pr := p.Progress(tasks)
		out = append(out, map[string]any{"plan": p, "tasks": planTasks(p, tasks), "todo": pr.Todo, "doing": pr.Doing, "review": pr.Review, "done": pr.Done, "canceled": pr.Canceled})
	}
	return out
}

func changePlan(w http.ResponseWriter, r *http.Request, dir string, plans *watch.PlanLog, tasks *watch.TaskLog) {
	var req struct {
		Plan  string `json:"plan"`
		Do    string `json:"do"`
		Slots int    `json:"slots"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "body: {plan, do: run|stop, slots?}")
		return
	}
	p, ok := plans.Find(req.Plan)
	if !ok {
		writeLaunchError(w, http.StatusNotFound, "no plan "+req.Plan)
		return
	}
	now := time.Now()
	switch req.Do {
	case "run":
		if req.Slots > 0 {
			p, _ = plans.Update(p.ID, now, func(p *watch.Plan) { p.Slots = req.Slots })
		}
		if why := planWatchReady(dir, p.Workspace, p.Slots); why != "" {
			writeLaunchError(w, http.StatusConflict, why)
			return
		}
		p, _ = plans.SetState(p.ID, watch.PlanRunning, now)
		nudgePlans(dir)
	case "stop":
		p = stopPlan(dir, plans, tasks, p, now)
	default:
		writeLaunchError(w, http.StatusBadRequest, "do: run or stop")
		return
	}
	writeLaunchJSON(w, map[string]any{"plan": p})
}

func stopPlan(dir string, plans *watch.PlanLog, tasks *watch.TaskLog, p watch.Plan, now time.Time) watch.Plan {
	for _, t := range planTasks(p, tasks) {
		if t.State == "Todo" {
			_, _ = tasks.Move(t.Ref(), "Canceled", now)
		}
	}
	p, _ = plans.SetState(p.ID, watch.PlanStopped, now)
	nudgeDaemon(dir)
	return p
}
