package watch

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	slackReviewThreadPrefix   = "rt:"
	slackReviewThreadTurn     = "rt-turn"
	slackReviewThreadLife     = 14 * 24 * time.Hour
	slackReviewThreadsPerPoll = 4
)

type ReviewThreadReader interface {
	AwaitsMe(ctx context.Context, channel, ts string) bool
}

func (s *Slack) followsReviews() bool { return len(s.cfg.Recheck) > 0 }

func (s *Slack) trackReviewPost(cursor Cursor, c slackConversation, m slackMessage, me string) {
	root := m.ThreadTS == "" || m.ThreadTS == m.TS
	if !s.followsReviews() || !root || !s.isReviewChannel(c) || m.User == me || m.BotID != "" {
		return
	}
	if len(PullLinks(m.Text)) == 0 {
		return
	}
	cursor[slackReviewThreadPrefix+c.ID+":"+m.TS] = m.TS
}

func (s *Slack) thread(ctx context.Context, channel, ts string) ([]slackMessage, error) {
	var res struct {
		Messages []slackMessage `json:"messages"`
	}
	params := url.Values{"channel": {channel}, "ts": {ts}, "limit": {"200"}}
	if err := s.api.call(ctx, "conversations.replies", params, &res); err != nil {
		return nil, err
	}
	return res.Messages, nil
}

func (s *Slack) reviewFollowUps(ctx context.Context, cursor Cursor, me, team string, budget int) []Event {
	if !s.followsReviews() {
		return nil
	}
	var keys []string
	for k := range cursor {
		if !strings.HasPrefix(k, slackReviewThreadPrefix) {
			continue
		}
		_, ts, _ := strings.Cut(strings.TrimPrefix(k, slackReviewThreadPrefix), ":")
		if s.clock().Sub(slackTSTime(ts)) > slackReviewThreadLife {
			delete(cursor, k)
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	start := 0
	for i, k := range keys {
		if k > cursor[slackReviewThreadTurn] {
			start = i
			break
		}
	}
	var events []Event
	for n := 0; n < len(keys) && n < slackReviewThreadsPerPoll; n++ {
		if s.api.calls >= budget {
			break
		}
		key := keys[(start+n)%len(keys)]
		cursor[slackReviewThreadTurn] = key
		channel, ts, _ := strings.Cut(strings.TrimPrefix(key, slackReviewThreadPrefix), ":")
		msgs, err := s.thread(ctx, channel, ts)
		if err != nil {
			break
		}
		reply, closed := s.askedToLookAgain(msgs, me, cursor[key])
		if closed {
			delete(cursor, key)
			continue
		}
		if reply.TS == "" {
			continue
		}
		cursor[key] = reply.TS
		c := slackConversation{ID: channel, Name: channelNameIn(cursor, channel)}
		events = append(events, s.event(ctx, cursor, reply, c, me, team, KindChatMessage))
	}
	return events
}

func (s *Slack) askedToLookAgain(msgs []slackMessage, me, seen string) (reply slackMessage, closed bool) {
	mine := -1
	for i := 1; i < len(msgs); i++ {
		if msgs[i].User == me {
			mine = i
		}
	}
	if mine < 0 {
		return slackMessage{}, false
	}
	if saysWord(msgs[mine].Text, []string{s.cfg.ApprovedWord}) {
		return slackMessage{}, true
	}
	for _, m := range msgs[mine+1:] {
		if m.User == me || m.BotID != "" || !slackTSNewer(m.TS, seen) {
			continue
		}
		if saysWord(m.Text, s.cfg.Recheck) {
			reply = m
		}
	}
	return reply, false
}

func (s *Slack) AwaitsMe(ctx context.Context, channel, ts string) bool {
	if s.me == "" {
		var who struct {
			UserID string `json:"user_id"`
		}
		if err := s.api.call(ctx, "auth.test", nil, &who); err != nil {
			return false
		}
		s.me = who.UserID
	}
	msgs, err := s.thread(ctx, channel, ts)
	if err != nil || len(msgs) < 2 {
		return false
	}
	last := msgs[len(msgs)-1]
	return last.User != s.me && last.BotID == "" && !saysWord(last.Text, []string{s.cfg.ApprovedWord})
}

func saysWord(text string, words []string) bool {
	for _, w := range words {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		if regexp.MustCompile(`(?i)(^|[^\p{L}])` + regexp.QuoteMeta(w) + `($|[^\p{L}])`).MatchString(text) {
			return true
		}
	}
	return false
}

func channelNameIn(cursor Cursor, id string) string {
	for k, v := range cursor {
		if v == id && strings.HasPrefix(k, "chan:#") {
			return strings.TrimPrefix(k, "chan:#")
		}
	}
	return ""
}
