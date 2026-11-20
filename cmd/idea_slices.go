package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/idea"
	"gopkg.in/yaml.v3"
)

const splitByFeature = "feature"

const sliceRule = ` How to cut the work: by feature, never by layer. A ticket is one thing a person can see or do when it is done, built end to end: the migration, the API, the screen, the background job and the tests that one feature needs, in every repository it reaches, all in the same ticket. One session carries one ticket on one branch across all of those repositories, so "add the migration", "build the endpoint" and "build the screen" are never three tickets: they are one. A ticket that moves only one layer (schema only, backend only, frontend only, tests only, docs only) is wrong unless the whole of the work lives in that one layer. Cut a groundwork ticket only for something two or more features need and none of them can own (a new service, a shared sign-in change), and let those features wait for it. Make the first ticket the thinnest slice that works end to end; the later ones widen it. Two tickets that would edit the same files wait for one another with "after"; tickets that do not may run side by side. Each ticket lists in "touches" the services or repositories it changes, by the names the repository picture uses, and in "checks" two to five things a tester can do to see it is done, one short sentence each.`

const ticketShape = `{"title": "...", "body": "...", "after": [1], "touches": ["api", "web"], "checks": ["..."]}`

const recutSoul = `You are a staff engineer re-cutting a page's tickets for a team of coding agents. The page and its current tickets are given. Keep everything the current tickets cover, lose nothing and add nothing, and cut it again.` + sliceRule + ` A ticket's title is what a teammate would say when picking it up: a verb, the thing, the person it serves, under 80 characters. Its body is two or three short paragraphs of prose: what to build and why it matters to the person using it, then what the person picking it up should know (where to start in each repository, the trap to avoid, what not to touch). Between 1 and 8 tickets. Answer with JSON only, no prose and no code fence: {"summary": "one or two sentences of the approach", "tasks": [` + ticketShape + `]} where "after" lists the 1-based numbers of the tickets this one waits for.`

type stackService struct {
	Name string
	Repo string
}

func stackServices(root string) []stackService {
	data, err := os.ReadFile(filepath.Join(root, composeFileName(root)))
	if err != nil {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil
	}
	var out []stackService
	for name, svc := range doc.Services {
		if svc.Path == "" {
			continue
		}
		abs := svc.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		repo := filepath.Base(abs)
		if repoRoot, ok := utils.RepoRootOf(abs); ok {
			repo = filepath.Base(repoRoot)
		}
		out = append(out, stackService{Name: name, Repo: repo})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func stackRepos(stack []stackService) []string {
	seen := map[string]bool{}
	var repos []string
	for _, s := range stack {
		if !seen[s.Repo] {
			seen[s.Repo] = true
			repos = append(repos, s.Repo)
		}
	}
	sort.Strings(repos)
	return repos
}

func stackPicture(root string) string {
	stack := stackServices(root)
	if len(stack) == 0 {
		return ""
	}
	byRepo := map[string][]string{}
	for _, s := range stack {
		byRepo[s.Repo] = append(byRepo[s.Repo], s.Name)
	}
	var b strings.Builder
	b.WriteString("Services by repository (one ticket runs on one branch across all of them):\n")
	for _, repo := range stackRepos(stack) {
		fmt.Fprintf(&b, "- %s: %s\n", repo, strings.Join(byRepo[repo], ", "))
	}
	return b.String()
}

func uniqueShortLines(items []string, most, longest int) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		item = clipTitle(strings.TrimSpace(strings.ReplaceAll(item, "\n", " ")), longest)
		key := strings.ToLower(item)
		if item == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
		if len(out) == most {
			break
		}
	}
	return out
}

func cleanTouches(touches []string) []string { return uniqueShortLines(touches, 8, 40) }

func cleanChecks(checks []string) []string { return uniqueShortLines(checks, 6, 240) }

func repoOfTouch(touch string, stack []stackService) string {
	touch = strings.ToLower(strings.TrimSpace(touch))
	for _, s := range stack {
		if strings.ToLower(s.Name) == touch || strings.ToLower(s.Repo) == touch {
			return s.Repo
		}
	}
	return ""
}

func reposOfTicket(t plannerTask, stack []stackService) []string {
	seen := map[string]bool{}
	var repos []string
	for _, touch := range t.Touches {
		if repo := repoOfTouch(touch, stack); repo != "" && !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	return repos
}

var layerTitle = regexp.MustCompile(`(?i)^\s*(\[(be|fe|db|api|ui|backend|frontend)\]|(backend|back-end|frontend|front-end|database|db|api|ui|mobile|web)\s*[:\-]\s|(add|create|write|run)\s+(the\s+|a\s+)?((db|database|sql|schema)\s+)?migrations?\b|(add|write)\s+(the\s+)?((unit|e2e|integration|end-to-end)\s+)?tests\b)`)

func layerCut(tickets []plannerTask, stack []stackService) string {
	if len(tickets) < 2 {
		return ""
	}
	if why := layerCutByRepo(tickets, stack); why != "" {
		return why
	}
	var named []string
	for i, t := range tickets {
		if layerTitle.MatchString(t.Title) {
			named = append(named, fmt.Sprintf("%d", i+1))
		}
	}
	if len(named) >= 2 {
		return "tickets " + strings.Join(named, ", ") + " are named after a layer (a migration, a backend, a frontend, tests), not after what a person gets"
	}
	return ""
}

func layerCutByRepo(tickets []plannerTask, stack []stackService) string {
	if len(stackRepos(stack)) < 2 {
		return ""
	}
	byRepo := map[string][]string{}
	for i, t := range tickets {
		repos := reposOfTicket(t, stack)
		if len(repos) != 1 {
			return ""
		}
		byRepo[repos[0]] = append(byRepo[repos[0]], fmt.Sprintf("%d", i+1))
	}
	if len(byRepo) < 2 {
		return ""
	}
	var parts []string
	for _, repo := range stackRepos(stack) {
		if nums := byRepo[repo]; len(nums) > 0 {
			parts = append(parts, repo+": "+strings.Join(nums, ", "))
		}
	}
	return "every ticket stays inside one repository (" + strings.Join(parts, " · ") + "), so none of them is a whole feature"
}

func plannerTasksOf(tickets []idea.Ticket) []plannerTask {
	var out []plannerTask
	for _, t := range tickets {
		out = append(out, plannerTask{Title: t.Title, Body: t.Body, After: t.After, Touches: t.Touches, Checks: t.Checks})
	}
	return out
}

func splitWarning(tickets []idea.Ticket, root string) string {
	if root == "" {
		return layerCut(plannerTasksOf(tickets), nil)
	}
	return layerCut(plannerTasksOf(tickets), stackServices(root))
}

func splitMark(tickets []idea.Ticket, root string) string {
	if len(tickets) == 0 || splitWarning(tickets, root) != "" {
		return ""
	}
	return splitByFeature
}

func ticketsOnBoard(tickets []idea.Ticket) bool {
	for _, t := range tickets {
		if t.Ref != "" {
			return true
		}
	}
	return false
}

func needsRecut(m idea.Meta) bool {
	return len(m.Tickets) > 0 && m.SplitBy != splitByFeature && !ticketsOnBoard(m.Tickets)
}

func recutByFeature(id *idea.Idea, root, model string) (plannerAnswer, error) {
	stack := stackServices(root)
	prompt := fmt.Sprintf("%s\n--- The page ---\n%s\n\n--- The tickets now ---\n%s", repoPicture(root), id.Doc, ticketsText(id.Meta.Tickets))
	ans, err := askRecut(root, model, prompt)
	if err != nil {
		return ans, err
	}
	why := layerCut(ans.Tasks, stack)
	if why == "" {
		return ans, nil
	}
	again, err := askRecut(root, model, prompt+"\n\nYour last cut was refused: "+why+". Merge the layers of each feature into one ticket and answer again.")
	if err != nil {
		return ans, nil
	}
	return again, nil
}

func recutIdea(dir string, id *idea.Idea) (*idea.Idea, error) {
	if len(id.Meta.Tickets) == 0 {
		return nil, fmt.Errorf("no split yet - corgi idea answer %s writes one", id.Meta.Slug)
	}
	if ticketsOnBoard(id.Meta.Tickets) {
		return nil, fmt.Errorf("tickets of %s are already on the board or in a tracker; edit those there", id.Meta.Slug)
	}
	root := ideaWorkspaceRoot(id.Meta.Workspace)
	runIn := root
	if runIn == "" {
		runIn = mustCwd()
	}
	ans, err := recutByFeature(id, runIn, firstNonEmpty(id.Meta.Model, planModel))
	if err != nil {
		return nil, err
	}
	return idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error {
		id.ChangeBy, id.ChangeNote = idea.Agent, "tickets cut again by feature"
		id.Meta.Tickets = ticketsFrom(ans.Tasks)
		id.Meta.SplitBy = splitMark(id.Meta.Tickets, root)
		return nil
	})
}

func askRecut(root, model, prompt string) (plannerAnswer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ideaTimeout)
	defer cancel()
	out, err := runIdeaPrint(ctx, root, model, recutSoul, prompt)
	if err != nil {
		return plannerAnswer{}, err
	}
	return parsePlannerAnswer(out, 8)
}

func sliceBody(body string, touches, checks []string) string {
	body = strings.TrimSpace(body)
	var b strings.Builder
	b.WriteString(body)
	if len(touches) > 1 {
		fmt.Fprintf(&b, "\n\nTouches: %s. This is one feature, end to end: make every change it needs in each of them on this one branch, and open one pull request per repository you changed, each linking the others.", strings.Join(touches, ", "))
	} else if len(touches) == 1 {
		fmt.Fprintf(&b, "\n\nTouches: %s.", touches[0])
	}
	if len(checks) > 0 {
		b.WriteString("\n\nDone when:\n")
		for _, c := range checks {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	return strings.TrimSpace(b.String())
}
