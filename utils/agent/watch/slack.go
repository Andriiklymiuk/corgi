package watch

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type SlackWatchConfig struct {
	Mentions       bool
	Channels       []string
	ReviewChannels []string
}

type Slack struct {
	api   *slackAPI
	cfg   SlackWatchConfig
	names map[string]string
}

func NewSlack(s Secrets, cfg SlackWatchConfig) *Slack {
	return &Slack{api: &slackAPI{Token: strings.TrimSpace(s.SlackUser)}, cfg: cfg, names: map[string]string{}}
}

func (s *Slack) Name() string { return "slack" }

func (s *Slack) Token() string { return s.api.Token }

type slackMessage struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
}

type slackConversation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	IsIM bool   `json:"is_im"`
	User string `json:"user"`
}

func (s *Slack) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	next := Cursor{}
	for k, v := range cursor {
		next[k] = v
	}
	if next["me"] == "" {
		var who struct {
			UserID string `json:"user_id"`
			Team   string `json:"team"`
		}
		if err := s.api.call(ctx, "auth.test", nil, &who); err != nil {
			return nil, next, err
		}
		next["me"], next["team"] = who.UserID, who.Team
	}
	me, team := next["me"], next["team"]

	convs, err := s.conversations(ctx)
	if err != nil {
		// A token with search:read alone still finds mentions; the list is
		// only needed for listened channels and direct messages.
		if len(s.cfg.Channels)+len(s.cfg.ReviewChannels) > 0 || !strings.Contains(err.Error(), "missing_scope") {
			if strings.Contains(err.Error(), "missing_scope") {
				err = fmt.Errorf("%w — listening to a channel needs channels:read and channels:history (groups:* for a private one)", err)
			}
			return nil, next, err
		}
		convs = nil
	}

	var events []Event
	if s.cfg.Mentions {
		found, err := s.mentions(ctx, next, me, team)
		if err != nil {
			return nil, next, err
		}
		events = append(events, found...)
	}

	for _, c := range s.channels(convs) {
		if c.IsIM {
			next["chan:@"+c.User] = c.ID
		} else {
			next["chan:#"+c.Name] = c.ID
		}
		found, err := s.history(ctx, next, c, me, team)
		if err != nil {
			return events, next, err
		}
		events = append(events, found...)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events, next, nil
}

func (s *Slack) channels(convs []slackConversation) []slackConversation {
	want := map[string]bool{}
	for _, name := range append(append([]string{}, s.cfg.Channels...), s.cfg.ReviewChannels...) {
		want[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "#"))] = true
	}
	var out []slackConversation
	for _, c := range convs {
		switch {
		case c.IsIM && s.cfg.Mentions:
			out = append(out, c)
		case !c.IsIM && want[strings.ToLower(c.Name)]:
			out = append(out, c)
		}
	}
	return out
}

func (s *Slack) conversations(ctx context.Context) ([]slackConversation, error) {
	var all []slackConversation
	cursor := ""
	for {
		params := url.Values{
			"types":            {"public_channel,private_channel,im,mpim"},
			"limit":            {"200"},
			"exclude_archived": {"true"},
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		var page struct {
			Channels []slackConversation `json:"channels"`
			Meta     struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := s.api.call(ctx, "conversations.list", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Channels...)
		if cursor = page.Meta.NextCursor; cursor == "" {
			return all, nil
		}
	}
}

type slackMatch struct {
	TS        string `json:"ts"`
	Text      string `json:"text"`
	User      string `json:"user"`
	Username  string `json:"username"`
	BotID     string `json:"bot_id"`
	Subtype   string `json:"subtype"`
	Permalink string `json:"permalink"`
	Channel   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
}

func (s *Slack) mentions(ctx context.Context, cursor Cursor, me, team string) ([]Event, error) {
	bookmark := cursor["search"]
	var matches []slackMatch
	for page := 1; page <= 5; page++ {
		var res struct {
			Messages struct {
				Matches []slackMatch `json:"matches"`
				Paging  struct {
					Page  int `json:"page"`
					Pages int `json:"pages"`
				} `json:"paging"`
			} `json:"messages"`
		}
		params := url.Values{
			"query":    {"<@" + me + ">"},
			"sort":     {"timestamp"},
			"sort_dir": {"desc"},
			"count":    {"50"},
			"page":     {strconv.Itoa(page)},
		}
		if err := s.api.call(ctx, "search.messages", params, &res); err != nil {
			return nil, err
		}
		done := false
		for _, m := range res.Messages.Matches {
			if bookmark != "" && !slackTSNewer(m.TS, bookmark) {
				done = true
				break
			}
			matches = append(matches, m)
		}
		if done || res.Messages.Paging.Page >= res.Messages.Paging.Pages {
			break
		}
	}

	newest := bookmark
	var events []Event
	for _, m := range matches {
		if slackTSNewer(m.TS, newest) {
			newest = m.TS
		}
		events = append(events, s.event(ctx, cursor,
			slackMessage{User: m.User, BotID: m.BotID, Subtype: m.Subtype, Text: m.Text,
				TS: m.TS, ThreadTS: threadFromPermalink(m.Permalink)},
			slackConversation{ID: m.Channel.ID, Name: m.Channel.Name}, me, team, KindChatMention))
	}
	cursor["search"] = newest
	if bookmark == "" {
		return nil, nil
	}
	return events, nil
}

func (s *Slack) history(ctx context.Context, cursor Cursor, c slackConversation, me, team string) ([]Event, error) {
	key := "hist:" + c.ID
	bookmark := cursor[key]
	params := url.Values{"channel": {c.ID}, "limit": {"200"}}
	if bookmark != "" {
		params.Set("oldest", bookmark)
	}
	var page struct {
		Messages []slackMessage `json:"messages"`
	}
	if err := s.api.call(ctx, "conversations.history", params, &page); err != nil {
		return nil, err
	}
	newest := bookmark
	var events []Event
	for _, m := range page.Messages {
		if m.TS == bookmark {
			continue
		}
		if slackTSNewer(m.TS, newest) {
			newest = m.TS
		}
		kind := KindChatMessage
		if c.IsIM || strings.Contains(m.Text, "<@"+me+">") {
			kind = KindChatMention
		}
		events = append(events, s.event(ctx, cursor, m, c, me, team, kind))
	}
	cursor[key] = newest
	if bookmark == "" {
		return nil, nil
	}
	return events, nil
}

func (s *Slack) event(ctx context.Context, cursor Cursor, m slackMessage, c slackConversation, me, team string, kind Kind) Event {
	author := s.handle(ctx, m.User)
	where := "#" + c.Name
	if c.IsIM {
		where = "DM"
	}
	body := s.render(ctx, m.Text)
	links := PullLinks(body)
	parentTS := m.ThreadTS
	if parentTS != "" && parentTS != m.TS {
		if parent, ok := s.parent(ctx, c.ID, parentTS); ok {
			parentBody := s.render(ctx, parent.Text)
			body = "In reply to " + s.handle(ctx, parent.User) + ": " + parentBody + "\n\n" + body
			links = append(links, PullLinks(parentBody)...)
		}
	}
	links = uniqueLinks(links)

	key := "slack:" + c.ID + ":" + m.TS
	if parentTS != "" && parentTS != m.TS {
		cursor["thread:"+key] = parentTS
	}
	title := author + " in " + where
	if c.IsIM {
		title = "DM from " + author
	}
	if first := firstTextLine(body); first != "" {
		title += ": " + first
	}
	if kind == KindChatMessage && s.isReviewChannel(c) && len(links) > 0 {
		kind = KindReviewRequested
	}
	return Event{
		Key:    key,
		Source: "slack",
		Kind:   kind,
		Ref:    "slack-" + tsSeconds(m.TS),
		Title:  title,
		Body:   body,
		URL:    slackPermalink(team, c.ID, m.TS, parentTS),
		Author: author,
		State:  where,
		Links:  links,
		Self:   m.User != "" && m.User == me,
		Bot:    m.BotID != "" || m.Subtype == "bot_message",
		Mine:   kind == KindChatMention || kind == KindReviewRequested,
		At:     slackTSTime(m.TS),
	}
}

func (s *Slack) isReviewChannel(c slackConversation) bool {
	for _, name := range s.cfg.ReviewChannels {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(name), "#"), c.Name) {
			return true
		}
	}
	return false
}

func (s *Slack) parent(ctx context.Context, channel, ts string) (slackMessage, bool) {
	var res struct {
		Messages []slackMessage `json:"messages"`
	}
	params := url.Values{"channel": {channel}, "ts": {ts}, "limit": {"1"}}
	if err := s.api.call(ctx, "conversations.replies", params, &res); err != nil || len(res.Messages) == 0 {
		return slackMessage{}, false
	}
	return res.Messages[0], true
}

func (s *Slack) handle(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	if got, ok := s.names[id]; ok {
		return got
	}
	var res struct {
		User struct {
			Name     string `json:"name"`
			RealName string `json:"real_name"`
		} `json:"user"`
	}
	name := id
	if err := s.api.call(ctx, "users.info", url.Values{"user": {id}}, &res); err == nil && res.User.Name != "" {
		name = res.User.Name
	}
	s.names[id] = "@" + name
	return s.names[id]
}

var (
	slackUserRef = regexp.MustCompile(`<@([UW][A-Z0-9]+)(\|[^>]*)?>`)
	slackLinkRef = regexp.MustCompile(`<(https?://[^|>]+)(\|[^>]*)?>`)
)

func (s *Slack) render(ctx context.Context, text string) string {
	out := slackUserRef.ReplaceAllStringFunc(text, func(m string) string {
		return s.handle(ctx, slackUserRef.FindStringSubmatch(m)[1])
	})
	out = slackLinkRef.ReplaceAllString(out, "$1")
	out = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(out)
	return strings.TrimSpace(out)
}

func threadFromPermalink(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Query().Get("thread_ts")
}

func tsSeconds(ts string) string {
	secs, _, _ := strings.Cut(ts, ".")
	return secs
}

func firstTextLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func uniqueLinks(links []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range links {
		if l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}
