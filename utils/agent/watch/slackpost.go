package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils/agent/sendgate"
)

type SlackTarget struct {
	Channel  string
	ThreadTS string
	As       string
}

type SlackPosted struct {
	Channel   string `json:"channel"`
	TS        string `json:"ts"`
	Permalink string `json:"permalink"`
	As        string `json:"as"`
}

type SlackPoster struct {
	User *slackAPI
	Bot  *slackAPI
	Team string
	Gate *sendgate.Gate
}

// Without a ledger on disk a poster still shares one budget per process.
var processGate = sendgate.InMemory()

func (p *SlackPoster) gate() *sendgate.Gate {
	if p.Gate != nil {
		return p.Gate
	}
	return processGate
}

func (p *SlackPoster) send(ctx context.Context, api *slackAPI, method, target, fingerprint string, body map[string]any, out any) error {
	if err := p.gate().Allow("slack", target, fingerprint); err != nil {
		return err
	}
	err := api.post(ctx, method, body, out)
	if errors.Is(err, errSlackRateLimited) {
		p.gate().Pause("slack", retryAfter(err))
	}
	return err
}

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

var ErrNoSlackToken = errors.New("no Slack token: corgi agent watch auth slack --token xoxp-… [--bot xoxb-…]")

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
	if err := p.send(ctx, api, "chat.postMessage", target.Channel+"/"+target.ThreadTS, text, body, &res); err != nil {
		return SlackPosted{}, err
	}
	return SlackPosted{Channel: res.Channel, TS: res.TS, As: as,
		Permalink: slackPermalink(p.Team, res.Channel, res.TS, target.ThreadTS)}, nil
}

func (p *SlackPoster) React(ctx context.Context, target SlackTarget, emoji string) error {
	api, _, err := p.voice(target.As)
	if err != nil {
		return err
	}
	if target.ThreadTS == "" {
		return errors.New("react: no message timestamp")
	}
	name := strings.Trim(emoji, ":")
	return p.send(ctx, api, "reactions.add", target.Channel+"/"+target.ThreadTS, "react:"+name, map[string]any{
		"channel": target.Channel, "timestamp": target.ThreadTS, "name": name,
	}, nil)
}

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
		// chat.postMessage takes a channel name; only a DM needs the list.
		if strings.HasPrefix(to, "#") && strings.Contains(err.Error(), "missing_scope") {
			return to, nil
		}
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
