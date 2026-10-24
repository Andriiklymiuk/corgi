// Package idea is one page per thing you want built, and the conversation
// around it. The page is markdown on disk; the conversation is comments
// anchored to a quote of the page (so a highlight survives the page being
// rewritten), with replies, a status each, and files beside them. People
// and agents both write in it; the agent's answers and the changes it made
// to the page are kept in the same file, so nothing said is lost.
//
//	<agentDir>/ideas/<slug>/idea.md        the page
//	<agentDir>/ideas/<slug>/meta.json      name, tags, workspace, key, tasks
//	<agentDir>/ideas/<slug>/comments.json  every comment, reply and status
//	<agentDir>/ideas/<slug>/files/         attachments
//	<agentDir>/ideas/changed               touched on every change, for streams
package idea

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	// Comment statuses, in the order a comment moves through them.
	StatusOpen     = "open"     // waits on the agent (or on a person, for a proposal)
	StatusAnswered = "answered" // the agent replied; the page did not change
	StatusFixed    = "fixed"    // the agent changed the page for it
	StatusResolved = "resolved" // a person closed it

	// Comment kinds.
	KindComment  = "comment"  // a person's remark on the page
	KindProposal = "proposal" // the agent's own suggestion, for a person to accept or dismiss

	// Idea statuses.
	Draft    = "draft"
	Building = "building"
	Built    = "built"

	// Agent is the author name the agent's replies and proposals carry.
	Agent = "agent"

	DocName      = "idea.md"
	MetaName     = "meta.json"
	CommentsName = "comments.json"
	FilesDir     = "files"
	ChangedName  = "changed"

	MaxDoc     = 400 << 10
	MaxComment = 8000
	MaxFile    = 8 << 20
)

// Meta is what the page is about, in a form a board or a list can show.
type Meta struct {
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Name      string   `json:"name,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Workspace string   `json:"workspace,omitempty"`
	Status    string   `json:"status"`
	By        string   `json:"by,omitempty"`
	Model     string   `json:"model,omitempty"`
	// Key is the share key in the open link; "" means the page is not open
	// and only a paired device or the dashboard may read it.
	Key string `json:"key,omitempty"`
	// Auto lets the agent answer new comments on its own.
	Auto bool `json:"auto"`
	// Tasks are the board tasks made from this page, in order.
	Tasks []int `json:"tasks,omitempty"`
	// Plan is the plan (P-n) that works through the tasks, once built.
	Plan      int       `json:"plan,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Answering is set while the agent is working through open comments.
	Answering bool `json:"answering,omitempty"`
}

// Anchor is where on the page a comment sits: the words highlighted, with
// a little of what came before and after so the spot is found again after
// the page changes. A comment with no anchor is about the page as a whole.
type Anchor struct {
	Quote   string `json:"quote"`
	Prefix  string `json:"prefix,omitempty"`
	Suffix  string `json:"suffix,omitempty"`
	Heading string `json:"heading,omitempty"`
}

type Attachment struct {
	Name string `json:"name"`
	Path string `json:"path"` // relative to the idea's directory
	Type string `json:"type,omitempty"`
	Size int64  `json:"size"`
}

type Reply struct {
	ID    int          `json:"id"`
	By    string       `json:"by"`
	Text  string       `json:"text"`
	At    time.Time    `json:"at"`
	Files []Attachment `json:"files,omitempty"`
	Links []string     `json:"links,omitempty"`
	// Changed says this reply came with a change to the page.
	Changed bool `json:"changed,omitempty"`
}

type Comment struct {
	ID      int          `json:"id"`
	Kind    string       `json:"kind"`
	By      string       `json:"by"`
	Text    string       `json:"text"`
	Anchor  *Anchor      `json:"anchor,omitempty"`
	Status  string       `json:"status"`
	At      time.Time    `json:"at"`
	Updated time.Time    `json:"updatedAt"`
	Files   []Attachment `json:"files,omitempty"`
	Links   []string     `json:"links,omitempty"`
	Replies []Reply      `json:"replies,omitempty"`
}

type Comments struct {
	Next     int       `json:"next"`
	Comments []Comment `json:"comments"`
}

// Idea is one page with everything around it, loaded.
type Idea struct {
	Dir      string
	Meta     Meta
	Doc      string
	Comments Comments
}

var mu sync.Mutex

func Root(agentDir string) string { return filepath.Join(agentDir, "ideas") }

func Dir(agentDir, slug string) string { return filepath.Join(Root(agentDir), slug) }

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidSlug says whether s can name an idea's directory.
func ValidSlug(s string) bool { return slugPattern.MatchString(s) }

// Slugify is the directory name for a title: lowercase words joined by
// dashes, at most 48 characters, never empty.
func Slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) && r < 128, unicode.IsDigit(r) && r < 128:
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "idea"
	}
	return s
}

// NewKey is a share key: what the open link carries.
func NewKey() string {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Create writes a new idea. The slug is taken from title when slug is "",
// and made unique with a number when it is taken.
func Create(agentDir, slug, title, doc string, meta Meta, now time.Time) (*Idea, error) {
	mu.Lock()
	defer mu.Unlock()
	if slug == "" {
		slug = Slugify(firstNonEmpty(meta.Name, title))
	}
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("%q cannot name an idea: lowercase letters, digits and dashes", slug)
	}
	base := slug
	for n := 2; ; n++ {
		if _, err := os.Stat(Dir(agentDir, slug)); os.IsNotExist(err) {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("an idea needs a title")
	}
	if len(doc) > MaxDoc {
		return nil, fmt.Errorf("the page is too long")
	}
	meta.Slug = slug
	meta.Title = strings.TrimSpace(title)
	if meta.Status == "" {
		meta.Status = Draft
	}
	if meta.Key == "" {
		meta.Key = NewKey()
	}
	meta.CreatedAt, meta.UpdatedAt = now, now
	dir := Dir(agentDir, slug)
	if err := os.MkdirAll(filepath.Join(dir, FilesDir), 0o700); err != nil {
		return nil, err
	}
	id := &Idea{Dir: dir, Meta: meta, Doc: doc, Comments: Comments{Next: 1, Comments: []Comment{}}}
	if err := id.saveLocked(agentDir); err != nil {
		return nil, err
	}
	return id, nil
}

// Load reads one idea by slug.
func Load(agentDir, slug string) (*Idea, error) {
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("no idea %q", slug)
	}
	dir := Dir(agentDir, slug)
	raw, err := os.ReadFile(filepath.Join(dir, MetaName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no idea %q - corgi idea list", slug)
		}
		return nil, err
	}
	id := &Idea{Dir: dir}
	if err := json.Unmarshal(raw, &id.Meta); err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Join(dir, MetaName), err)
	}
	id.Meta.Slug = slug
	if doc, err := os.ReadFile(filepath.Join(dir, DocName)); err == nil {
		id.Doc = string(doc)
	}
	id.Comments = Comments{Next: 1, Comments: []Comment{}}
	if raw, err := os.ReadFile(filepath.Join(dir, CommentsName)); err == nil {
		_ = json.Unmarshal(raw, &id.Comments)
	}
	if id.Comments.Comments == nil {
		id.Comments.Comments = []Comment{}
	}
	if id.Comments.Next < 1 {
		id.Comments.Next = 1
	}
	for _, c := range id.Comments.Comments {
		if c.ID >= id.Comments.Next {
			id.Comments.Next = c.ID + 1
		}
		for _, r := range c.Replies {
			if r.ID >= id.Comments.Next {
				id.Comments.Next = r.ID + 1
			}
		}
	}
	return id, nil
}

// List is every idea's meta, newest change first.
func List(agentDir string) []Meta {
	entries, err := os.ReadDir(Root(agentDir))
	if err != nil {
		return nil
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() || !ValidSlug(e.Name()) {
			continue
		}
		id, err := Load(agentDir, e.Name())
		if err != nil {
			continue
		}
		out = append(out, id.Meta)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Save writes meta, page and comments, and marks the change.
func (id *Idea) Save(agentDir string) error {
	mu.Lock()
	defer mu.Unlock()
	return id.saveLocked(agentDir)
}

func (id *Idea) saveLocked(agentDir string) error {
	if err := os.MkdirAll(id.Dir, 0o700); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(id.Meta, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(id.Dir, MetaName), meta, 0o600); err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(id.Dir, DocName), []byte(id.Doc), 0o600); err != nil {
		return err
	}
	comments, err := json.MarshalIndent(id.Comments, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(id.Dir, CommentsName), comments, 0o600); err != nil {
		return err
	}
	Touch(agentDir)
	return nil
}

// Touch marks that some idea changed, for whoever streams changes.
func Touch(agentDir string) {
	_ = os.MkdirAll(Root(agentDir), 0o700)
	_ = os.WriteFile(filepath.Join(Root(agentDir), ChangedName), []byte(time.Now().Format(time.RFC3339Nano)+"\n"), 0o600)
}

// Update loads, changes and saves one idea under the lock, so two writers
// (a phone and the agent) never lose each other's comments.
func Update(agentDir, slug string, fn func(*Idea) error) (*Idea, error) {
	mu.Lock()
	defer mu.Unlock()
	id, err := Load(agentDir, slug)
	if err != nil {
		return nil, err
	}
	if err := fn(id); err != nil {
		return nil, err
	}
	id.Meta.UpdatedAt = time.Now()
	return id, id.saveLocked(agentDir)
}

// SetDoc replaces the page; a by of Agent means the agent rewrote it.
func (id *Idea) SetDoc(doc string, now time.Time) error {
	if len(doc) > MaxDoc {
		return fmt.Errorf("the page is too long")
	}
	id.Doc = doc
	id.Meta.UpdatedAt = now
	return nil
}

// AddComment adds a comment by a person (kind comment) or the agent (kind
// proposal). An anchor whose quote is empty is dropped: the comment is
// about the page as a whole.
func (id *Idea) AddComment(kind, by, text string, anchor *Anchor, files []Attachment, links []string, now time.Time) (Comment, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 && len(links) == 0 {
		return Comment{}, fmt.Errorf("a comment needs some words")
	}
	if len(text) > MaxComment {
		return Comment{}, fmt.Errorf("a comment is under %d characters", MaxComment)
	}
	if kind != KindProposal {
		kind = KindComment
	}
	if anchor != nil {
		anchor.Quote = strings.TrimSpace(anchor.Quote)
		if anchor.Quote == "" {
			anchor = nil
		} else {
			anchor.Quote = clip(anchor.Quote, 600)
			anchor.Prefix = clip(anchor.Prefix, 80)
			anchor.Suffix = clip(anchor.Suffix, 80)
			anchor.Heading = clip(anchor.Heading, 120)
		}
	}
	c := Comment{ID: id.Comments.Next, Kind: kind, By: author(by), Text: text, Anchor: anchor, Status: StatusOpen,
		At: now, Updated: now, Files: files, Links: cleanLinks(links)}
	id.Comments.Next++
	id.Comments.Comments = append(id.Comments.Comments, c)
	id.Meta.UpdatedAt = now
	return c, nil
}

// Reply adds a reply under a comment. The agent's reply moves an open
// comment to answered, or to fixed when changed is set; a person's reply
// to a resolved or answered comment reopens it, since there is more to say.
func (id *Idea) Reply(commentID int, by, text string, changed bool, files []Attachment, links []string, now time.Time) (Reply, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 && len(links) == 0 {
		return Reply{}, fmt.Errorf("a reply needs some words")
	}
	if len(text) > MaxComment {
		return Reply{}, fmt.Errorf("a reply is under %d characters", MaxComment)
	}
	c := id.find(commentID)
	if c == nil {
		return Reply{}, fmt.Errorf("no comment %d", commentID)
	}
	r := Reply{ID: id.Comments.Next, By: author(by), Text: text, At: now, Changed: changed, Files: files, Links: cleanLinks(links)}
	id.Comments.Next++
	c.Replies = append(c.Replies, r)
	c.Updated = now
	switch {
	case r.By == Agent && changed:
		c.Status = StatusFixed
	case r.By == Agent && c.Status == StatusOpen:
		c.Status = StatusAnswered
	case r.By != Agent && c.Status != StatusOpen:
		c.Status = StatusOpen
	}
	id.Meta.UpdatedAt = now
	return r, nil
}

// SetStatus moves a comment: a person resolves or reopens, the agent
// answers or fixes.
func (id *Idea) SetStatus(commentID int, status string, now time.Time) (Comment, error) {
	switch status {
	case StatusOpen, StatusAnswered, StatusFixed, StatusResolved:
	default:
		return Comment{}, fmt.Errorf("a status is open, answered, fixed or resolved")
	}
	c := id.find(commentID)
	if c == nil {
		return Comment{}, fmt.Errorf("no comment %d", commentID)
	}
	c.Status = status
	c.Updated = now
	id.Meta.UpdatedAt = now
	return *c, nil
}

func (id *Idea) find(commentID int) *Comment {
	for i := range id.Comments.Comments {
		if id.Comments.Comments[i].ID == commentID {
			return &id.Comments.Comments[i]
		}
	}
	return nil
}

// Find is the comment with that id.
func (id *Idea) Find(commentID int) (Comment, bool) {
	c := id.find(commentID)
	if c == nil {
		return Comment{}, false
	}
	return *c, true
}

// ForAgent is what waits on the agent: open comments by people, and
// proposals a person accepted (replied to while open). A proposal nobody
// answered is the agent's own, so it waits on a person instead.
func (id *Idea) ForAgent() []Comment {
	var out []Comment
	for _, c := range id.Comments.Comments {
		if c.Status != StatusOpen {
			continue
		}
		if c.Kind == KindProposal {
			accepted := false
			for _, r := range c.Replies {
				if r.By != Agent {
					accepted = true
				}
			}
			if !accepted {
				continue
			}
		}
		if last := lastAuthor(c); last == Agent {
			continue
		}
		out = append(out, c)
	}
	return out
}

func lastAuthor(c Comment) string {
	if len(c.Replies) == 0 {
		return c.By
	}
	return c.Replies[len(c.Replies)-1].By
}

// Counts is how many comments stand in each status.
func (id *Idea) Counts() map[string]int {
	out := map[string]int{StatusOpen: 0, StatusAnswered: 0, StatusFixed: 0, StatusResolved: 0}
	for _, c := range id.Comments.Comments {
		out[c.Status]++
	}
	return out
}

// Open is how many comments wait on someone: open ones, plus answered and
// fixed ones a person has not closed.
func (id *Idea) Open() int {
	n := 0
	for _, c := range id.Comments.Comments {
		if c.Status != StatusResolved {
			n++
		}
	}
	return n
}

var fileNamePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// AddFile stores an attachment beside the page and says how to refer to it.
func (id *Idea) AddFile(name string, data []byte, mime string) (Attachment, error) {
	if len(data) == 0 {
		return Attachment{}, fmt.Errorf("the file is empty")
	}
	if len(data) > MaxFile {
		return Attachment{}, fmt.Errorf("a file is under %d MB", MaxFile>>20)
	}
	base := fileNamePattern.ReplaceAllString(filepath.Base(strings.TrimSpace(name)), "-")
	base = strings.Trim(base, "-.")
	if base == "" {
		base = "file"
	}
	if len(base) > 80 {
		base = base[len(base)-80:]
	}
	stamp := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), base)
	rel := filepath.Join(FilesDir, stamp)
	if err := os.MkdirAll(filepath.Join(id.Dir, FilesDir), 0o700); err != nil {
		return Attachment{}, err
	}
	if err := atomicfile.Write(filepath.Join(id.Dir, rel), data, 0o600); err != nil {
		return Attachment{}, err
	}
	return Attachment{Name: base, Path: filepath.ToSlash(rel), Type: mime, Size: int64(len(data))}, nil
}

// FilePath is the attachment's path on disk, or "" when the name is not
// one of this idea's files (so a request cannot read elsewhere).
func (id *Idea) FilePath(name string) string {
	name = filepath.Base(name)
	if name == "." || name == "" || strings.HasPrefix(name, ".") {
		return ""
	}
	p := filepath.Join(id.Dir, FilesDir, name)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// Transcript is the comments as the agent reads them: one block per
// comment with its spot on the page, its replies and its status.
func Transcript(cs []Comment) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "### Comment %d (%s, by %s, %s)\n", c.ID, c.Kind, c.By, c.Status)
		if c.Anchor != nil {
			if c.Anchor.Heading != "" {
				fmt.Fprintf(&b, "Under: %s\n", c.Anchor.Heading)
			}
			fmt.Fprintf(&b, "Highlighted: %q\n", c.Anchor.Quote)
		} else {
			b.WriteString("About: the whole page\n")
		}
		fmt.Fprintf(&b, "%s\n", c.Text)
		for _, l := range c.Links {
			fmt.Fprintf(&b, "Link: %s\n", l)
		}
		for _, f := range c.Files {
			fmt.Fprintf(&b, "Attached: %s (%s, %d bytes)\n", f.Name, f.Type, f.Size)
		}
		for _, r := range c.Replies {
			fmt.Fprintf(&b, "- %s replied: %s\n", r.By, r.Text)
			for _, l := range r.Links {
				fmt.Fprintf(&b, "  Link: %s\n", l)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// HeadingFor is the nearest markdown heading above the first place quote
// appears in doc, so a comment can say which section it is in.
func HeadingFor(doc, quote string) string {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return ""
	}
	at := strings.Index(doc, quote)
	if at < 0 {
		return ""
	}
	heading := ""
	for _, line := range strings.Split(doc[:at], "\n") {
		if strings.HasPrefix(line, "#") {
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return heading
}

func author(by string) string {
	by = strings.TrimSpace(by)
	if by == "" {
		return "someone"
	}
	return clip(by, 60)
}

func cleanLinks(links []string) []string {
	var out []string
	for _, l := range links {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			out = append(out, clip(l, 2000))
		}
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
