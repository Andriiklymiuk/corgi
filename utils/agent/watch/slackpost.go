// Speaking in Slack, as the person or as their app. The voice is a choice
// with a safe default: an unattended run speaks as the app unless the
// workspace says otherwise, because a machine posting under someone's own
// name is the mistake that cannot be taken back.
package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SlackTarget is where a message goes and in whose voice.
type SlackTarget struct {
	Channel  string
	ThreadTS string
	As       string // "me", "bot", or empty for the default
}

// SlackPosted is what was said and where to read it.
type SlackPosted struct {
	Channel   string `json:"channel"`
	TS        string `json:"ts"`
	Permalink string `json:"permalink"`
	As        string `json:"as"`
}

// SlackPoster holds both voices; either may be absent.
type SlackPoster struct {
	User *slackAPI
	Bot  *slackAPI
	Team string
}

// NewSlackPoster builds the poster from whatever tokens are stored.
func NewSlackPoster(s Secrets) *SlackPoster {
	p := &SlackPoster{}
	if t := strings.TrimSpace(s.SlackUser); t != "" {
		p.User = &slackAPI{Token: t}
	}
	if t := strings.TrimSpace(s.SlackBot); t != "" {
		p.Bot = &slackAPI{Token: t}
	}
	return p
}

// ErrNoSlackToken is posting with nothing to post with.
var ErrNoSlackToken = errors.New("no Slack token: corgi agent watch auth slack --token xoxp-… [--bot xoxb-…]")

// voice picks the api for a target: what was asked for, else the bot, else
// the user. Asking for a voice with no token is an error, never a silent
// fall through to the other one.
func (p *SlackPoster) voice(as string) (*slackAPI, string, error) {
	switch strings.TrimSpace(as) {
	case "me":
		if p.User == nil {
			return nil, "", fmt.Errorf("speaking as you needs the user token: %w", ErrNoSlackToken)
		}
		return p.User, "me", nil
	case "bot":
		if p.Bot == nil {
			return nil, "", fmt.Errorf("speaking as the app needs a bot token: %w", ErrNoSlackToken)
		}
		return p.Bot, "bot", nil
	}
	if p.Bot != nil {
		return p.Bot, "bot", nil
	}
	if p.User != nil {
		return p.User, "me", nil
	}
	return nil, "", ErrNoSlackToken
}

// Post says one thing in a channel, in a thread when the target names one.
func (p *SlackPoster) Post(ctx context.Context, target SlackTarget, text string) (SlackPosted, error) {
	api, as, err := p.voice(target.As)
	if err != nil {
		return SlackPosted{}, err
	}
	body := map[string]any{"channel": target.Channel, "text": text}
	if target.ThreadTS != "" {
		body["thread_ts"] = target.ThreadTS
	}
	var res struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	if err := api.post(ctx, "chat.postMessage", body, &res); err != nil {
		return SlackPosted{}, err
	}
	return SlackPosted{Channel: res.Channel, TS: res.TS, As: as,
		Permalink: slackPermalink(p.Team, res.Channel, res.TS, target.ThreadTS)}, nil
}

// React puts one emoji on the message the target names.
func (p *SlackPoster) React(ctx context.Context, target SlackTarget, emoji string) error {
	api, _, err := p.voice(target.As)
	if err != nil {
		return err
	}
	if target.ThreadTS == "" {
		return errors.New("react: no message timestamp")
	}
	return api.post(ctx, "reactions.add", map[string]any{
		"channel": target.Channel, "timestamp": target.ThreadTS, "name": strings.Trim(emoji, ":"),
	}, nil)
}

// Resolve turns "#name" or "@handle" into a channel id; an id is itself.
func (p *SlackPoster) Resolve(ctx context.Context, to string) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", errors.New("no target: --to '#channel' or postTo in the workspace's chat config")
	}
	if !strings.HasPrefix(to, "#") && !strings.HasPrefix(to, "@") {
		return to, nil
	}
	api := p.User
	if api == nil {
		api = p.Bot
	}
	if api == nil {
		return "", ErrNoSlackToken
	}
	s := &Slack{api: api, names: map[string]string{}}
	convs, err := s.conversations(ctx)
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(to, "#"), "@"))
	for _, c := range convs {
		if !c.IsIM && strings.ToLower(c.Name) == want {
			return c.ID, nil
		}
	}
	if strings.HasPrefix(to, "@") {
		for _, c := range convs {
			if c.IsIM && strings.ToLower(s.handle(ctx, c.User)) == "@"+want {
				return c.ID, nil
			}
		}
	}
	return "", fmt.Errorf("no channel or conversation called %s that this token can see", to)
}
