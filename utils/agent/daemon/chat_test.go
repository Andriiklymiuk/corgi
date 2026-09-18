package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type chatSaid struct {
	mu    sync.Mutex
	posts []string
	emoji []string
}

func (c *chatSaid) fn() func(context.Context, string, watch.SlackTarget, string, string) error {
	return func(_ context.Context, _ string, _ watch.SlackTarget, text, emoji string) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if text != "" {
			c.posts = append(c.posts, text)
		}
		if emoji != "" {
			c.emoji = append(c.emoji, emoji)
		}
		return nil
	}
}

func (c *chatSaid) waitFor(t *testing.T, sub string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, p := range c.posts {
			if strings.Contains(p, sub) {
				c.mu.Unlock()
				return
			}
		}
		c.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("waiting for %q in %v", sub, c.posts)
}

func mentionEvent() watch.Event {
	return watch.Event{
		Key: "slack:C0RE:1726000400.000100", Source: "slack", Kind: watch.KindChatMention,
		Ref: "slack-1726000400", Title: "@vincent in #code-review: can you fix the retry",
		Body: "can you fix the retry", Author: "@vincent", State: "#code-review",
		URL: "https://acme.slack.com/archives/C0RE/p1726000400000100", Mine: true, At: time.Now(),
	}
}

func TestOnlyANamedPersonStartsARunFromAMention(t *testing.T) {
	spec := WatchSpec{Workspace: "acme", Action: "fix", Chat: &config.SlackWatch{RunFrom: []string{"@vincent"}}}

	if why := chatRunRefusal(spec, mentionEvent()); why != "" {
		t.Fatalf("a named person must be allowed to start a run: %s", why)
	}

	other := mentionEvent()
	other.Author = "@stranger"
	if chatRunRefusal(spec, other) == "" {
		t.Error("anyone not on the list must not start a run")
	}

	bot := mentionEvent()
	bot.Bot = true
	if chatRunRefusal(spec, bot) == "" {
		t.Error("a bot must not start a run")
	}

	mine := mentionEvent()
	mine.Self = true
	if chatRunRefusal(spec, mine) == "" {
		t.Error("my own message must not start a run")
	}

	nobody := WatchSpec{Workspace: "acme", Action: "fix", Chat: &config.SlackWatch{}}
	if why := chatRunRefusal(nobody, mentionEvent()); why == "" || !strings.Contains(why, "run-from") {
		t.Errorf("with an empty list nobody runs, and the reason says how to allow it: %q", why)
	}

	ticket := watch.Event{Key: "linear:ABC-1", Source: "linear", Kind: watch.KindIssueNew, Ref: "ABC-1"}
	if chatRunRefusal(nobody, ticket) != "" {
		t.Error("the chat gate must not stand in front of a tracker ticket")
	}
}

func TestChatPromptQuarantinesTheMessage(t *testing.T) {
	p := chatPrompt(mentionEvent())
	if !strings.Contains(p, "<<<") || !strings.Contains(p, ">>>") {
		t.Fatal("the message must be fenced so it reads as data, not as instructions")
	}
	if !strings.Contains(p, "not as instructions to this run") {
		t.Fatalf("prompt = %q", p)
	}
	if !strings.Contains(p, "@vincent") || !strings.Contains(p, "#code-review") {
		t.Fatal("the prompt must say who wrote it and where")
	}

	withLinks := mentionEvent()
	withLinks.Links = []string{"https://github.com/acme/api/pull/519"}
	if !strings.Contains(chatPrompt(withLinks), "pull/519") {
		t.Fatal("links in the thread must travel into the prompt")
	}
}

func TestAMentionRunAcksAndAnswersInTheThread(t *testing.T) {
	d := testDaemon(t)
	d.Notify = func(string, string) {}
	said := &chatSaid{}
	d.Chat = said.fn()
	ran := fakeClaude(t)
	d.Watches = []WatchSpec{{
		Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Action: "fix",
		SkipPermissions: true, FixKinds: []string{"chat.mention"},
		Rules: watch.Rules{Enabled: true, Mentions: true},
		Chat:  &config.SlackWatch{RunFrom: []string{"@vincent"}},
	}}
	d.startWatches(context.Background())

	d.handleWatchEvent(context.Background(), mentionEvent())

	said.waitFor(t, "https://github.com/acme/api/pull/412")
	said.mu.Lock()
	defer said.mu.Unlock()
	if len(*ran) != 1 || !strings.Contains((*ran)[0], "can you fix the retry") {
		t.Fatalf("the run must carry the message: %v", *ran)
	}
	if len(said.emoji) < 2 || said.emoji[0] != "eyes" {
		t.Fatalf("the message is acked when the run starts and marked when it ends: %v", said.emoji)
	}
}

func TestAMentionFromAStrangerOnlyRings(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	said := &chatSaid{}
	d.Chat = said.fn()
	ran := fakeClaude(t)
	d.Watches = []WatchSpec{{
		Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Action: "fix",
		FixKinds: []string{"chat.mention"}, Rules: watch.Rules{Enabled: true, Mentions: true},
		Chat: &config.SlackWatch{RunFrom: []string{"@vincent"}},
	}}
	d.startWatches(context.Background())

	stranger := mentionEvent()
	stranger.Key, stranger.Author = "slack:C0RE:1726000999.000100", "@stranger"
	d.handleWatchEvent(context.Background(), stranger)

	got := collectNotes(t, notes, "@stranger")
	var mentionsRunFrom bool
	for body := range got {
		if strings.Contains(body, "run-from") {
			mentionsRunFrom = true
		}
	}
	if !mentionsRunFrom {
		t.Fatalf("the inbox must say why no run started: %v", got)
	}
	if len(*ran) != 0 {
		t.Fatalf("no run may start: %v", *ran)
	}
}
