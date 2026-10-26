package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

type LeftKind string

const (
	LeftCI    LeftKind = "ci"
	LeftMerge LeftKind = "merge"
	LeftOther LeftKind = "other"
)

type LeftItem struct {
	Text   string    `json:"text"`
	Kind   LeftKind  `json:"kind"`
	DoneAt time.Time `json:"doneAt,omitzero"`
	DoneBy string    `json:"doneBy,omitempty"`
}

const (
	leftHeading  = "Left for you"
	leftMax      = 6
	leftClip     = 200
	leftKeepOpen = 14 * 24 * time.Hour
)

var (
	leftHeadings = regexp.MustCompile(`(?im)^\s*(?:#{1,4}\s*|\*\*)(?:left for you|what you do next|you do next|what you need to do|what is left for you|what you need to do next|next)\b[^\n]*$`)
	anyHeading   = regexp.MustCompile(`^\s*(?:#{1,4}\s|\*\*[^*]+\*\*\s*$)`)
	bulletLine   = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+(.*\S)`)
	nothingLeft  = regexp.MustCompile(`(?i)^(?:nothing|none|no action|you do nothing|i |i'm |i've |i'll |ci (?:is|was) not watched|corgi (?:will|watches))`)
	ciWords      = regexp.MustCompile(`\bCI\b|(?i:\bpipelines?\b|\bchecks? (?:go|goes|went|turn|pass|are green|is green))`)
	mergeWords   = regexp.MustCompile(`(?i)\bmerge\b`)
)

func LeftFor(output string) []LeftItem {
	locs := leftHeadings.FindAllStringIndex(output, -1)
	if len(locs) == 0 {
		return nil
	}
	rest := output[locs[len(locs)-1][1]:]
	var out []LeftItem
	inList := false
	for _, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "" {
			if inList {
				break
			}
			continue
		}
		if anyHeading.MatchString(line) || strings.HasPrefix(strings.TrimSpace(line), "===") {
			break
		}
		m := bulletLine.FindStringSubmatch(line)
		if m == nil {
			if inList && strings.HasPrefix(line, "  ") {
				continue
			}
			if inList {
				break
			}
			if text := strings.TrimSpace(line); !nothingLeft.MatchString(strings.ReplaceAll(text, "**", "")) {
				out = append(out, newLeft(text))
			}
			break
		}
		inList = true
		text := strings.TrimSpace(m[1])
		if nothingLeft.MatchString(strings.ReplaceAll(text, "**", "")) {
			continue
		}
		out = append(out, newLeft(text))
		if len(out) == leftMax {
			break
		}
	}
	return out
}

func newLeft(text string) LeftItem {
	text = strings.TrimSpace(strings.ReplaceAll(text, "**", ""))
	if r := []rune(text); len(r) > leftClip {
		text = string(r[:leftClip-1]) + "…"
	}
	return LeftItem{Text: text, Kind: leftKindOf(text)}
}

func leftKindOf(text string) LeftKind {
	switch {
	case mergeWords.MatchString(text):
		return LeftMerge
	case ciWords.MatchString(text):
		return LeftCI
	}
	return LeftOther
}

func (l *FixLog) SetLeft(key string, items []LeftItem) {
	if key == "" || len(items) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].Left = items
			_ = l.save()
			return
		}
	}
}

func (l *FixLog) TickLeft(workspace, link string, kinds []LeftKind, why string, at time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	ticked := 0
	for i := range l.Started {
		r := &l.Started[i]
		if r.Workspace != workspace || len(r.Left) == 0 || !r.worked(link) {
			continue
		}
		for j := range r.Left {
			if !r.Left[j].DoneAt.IsZero() || !hasKind(kinds, r.Left[j].Kind) {
				continue
			}
			r.Left[j].DoneAt, r.Left[j].DoneBy = at, why
			ticked++
		}
	}
	if ticked > 0 {
		_ = l.save()
	}
	return ticked
}

func hasKind(kinds []LeftKind, k LeftKind) bool {
	for _, want := range kinds {
		if want == k {
			return true
		}
	}
	return false
}

type LeftRow struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Ref       string    `json:"ref,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Text      string    `json:"text"`
	Kind      LeftKind  `json:"kind"`
	At        time.Time `json:"at"`
	DoneAt    time.Time `json:"doneAt,omitzero"`
	DoneBy    string    `json:"doneBy,omitempty"`
}

func leftID(key string, i int) string { return key + "#" + strconv.Itoa(i) }

func LeftRows(records []FixRecord, ticked map[string]LeftTick, since, now time.Time) []LeftRow {
	var out []LeftRow
	for _, r := range records {
		if len(r.Left) == 0 || now.Sub(r.FinishedAt) > leftKeepOpen {
			continue
		}
		url := r.URL
		if len(r.PRs) > 0 {
			url = r.PRs[0]
		}
		for i, item := range r.Left {
			row := LeftRow{ID: leftID(r.Key, i), Workspace: r.Workspace, Ref: r.Ref, Title: r.Title, URL: url,
				Text: item.Text, Kind: item.Kind, At: r.FinishedAt, DoneAt: item.DoneAt, DoneBy: item.DoneBy}
			if t, ok := ticked[row.ID]; ok && row.DoneAt.IsZero() {
				row.DoneAt, row.DoneBy = t.At, t.By
			}
			if !row.DoneAt.IsZero() && row.DoneAt.Before(since) {
				continue
			}
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

func OpenLeft(rows []LeftRow) []LeftRow {
	var open []LeftRow
	for _, r := range rows {
		if r.DoneAt.IsZero() {
			open = append(open, r)
		}
	}
	return open
}

type LeftTick struct {
	At time.Time `json:"at"`
	By string    `json:"by"`
}

type LeftTicks struct {
	mu    sync.Mutex
	path  string
	Ticks map[string]LeftTick `json:"ticks"`
}

func LoadLeftTicks(agentDir string) *LeftTicks {
	t := &LeftTicks{path: filepath.Join(agentDir, "watch", "left-ticked.json"), Ticks: map[string]LeftTick{}}
	if data, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(data, t)
	}
	if t.Ticks == nil {
		t.Ticks = map[string]LeftTick{}
	}
	return t
}

func (t *LeftTicks) Tick(id, by string, at time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Ticks[id] = LeftTick{At: at, By: by}
	for k, v := range t.Ticks {
		if at.Sub(v.At) > 2*leftKeepOpen {
			delete(t.Ticks, k)
		}
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(t.path, data, 0o600)
}

func (t *LeftTicks) All() map[string]LeftTick {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]LeftTick, len(t.Ticks))
	for k, v := range t.Ticks {
		out[k] = v
	}
	return out
}

func LeftInstruction() string {
	return "If something is left for a person, end with a `## " + leftHeading + "` heading and one `- ` line per item, " +
		"each one thing a person must do; leave the heading out when nothing is. Do not list what corgi does on its own " +
		"(watching CI, starting a run when it goes red).\n"
}

var runHeader = regexp.MustCompile(`(?m)^=== \d{4}-\d{2}-\d{2}T\S+ `)

func LastRunOf(log string) string {
	locs := runHeader.FindAllStringIndex(log, -1)
	if len(locs) == 0 {
		return log
	}
	return log[locs[len(locs)-1][0]:]
}

func (l *FixLog) Backfill(logOf func(key string) string, pulls map[string]PullStatus, now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	filled := 0
	for i := range l.Started {
		r := &l.Started[i]
		if !r.Done() || r.Error != "" || len(r.Left) > 0 || len(r.Gaps) > 0 || now.Sub(r.FinishedAt) > leftKeepOpen {
			continue
		}
		run := LastRunOf(logOf(r.Key))
		if run == "" {
			continue
		}
		r.Left, r.Gaps = LeftFor(run), GapsIn(run)
		for _, link := range append([]string{r.URL}, r.PRs...) {
			p, ok := pulls[PullRef(link)]
			if !ok {
				continue
			}
			for j := range r.Left {
				item := &r.Left[j]
				switch {
				case p.State == "merged" && (item.Kind == LeftCI || item.Kind == LeftMerge):
					item.DoneAt, item.DoneBy = p.At, "merged"
				case p.Checks == "passing" && item.Kind == LeftCI:
					item.DoneAt, item.DoneBy = p.At, "checks ✓"
				}
			}
		}
		if len(r.Left) > 0 || len(r.Gaps) > 0 {
			filled++
		}
	}
	if filled > 0 {
		_ = l.save()
	}
	return filled
}
