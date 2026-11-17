package watch

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type SlackMessage struct {
	Channel string
	TS      string
	Team    string
}

var (
	slackTSPattern   = regexp.MustCompile(`^\d+\.\d+$`)
	slackArchivePath = regexp.MustCompile(`^/archives/([A-Za-z0-9]+)/p(\d{16})/?$`)
)

const slackMessageForms = "its link (https://<workspace>.slack.com/archives/<channel>/p<16 digits>) or its event key (slack:<channel>:<ts>)"

func ParseSlackMessage(ref string) (SlackMessage, error) {
	ref = strings.Trim(strings.TrimSpace(ref), "<>")
	if ref == "" {
		return SlackMessage{}, fmt.Errorf("no message: pass %s", slackMessageForms)
	}
	if strings.HasPrefix(ref, "slack:") {
		parts := strings.Split(ref, ":")
		if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || !slackTSPattern.MatchString(parts[2]) {
			return SlackMessage{}, fmt.Errorf("%q is not a slack event key: want slack:<channel>:<ts>, e.g. slack:C0RE:1726000400.000100", ref)
		}
		return SlackMessage{Channel: parts[1], TS: parts[2]}, nil
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return SlackMessage{}, fmt.Errorf("%q is not a Slack message: pass %s", ref, slackMessageForms)
	}
	host := strings.ToLower(u.Hostname())
	if host != "slack.com" && !strings.HasSuffix(host, ".slack.com") {
		return SlackMessage{}, fmt.Errorf("%q is not a Slack link: pass %s", ref, slackMessageForms)
	}
	m := slackArchivePath.FindStringSubmatch(u.Path)
	if m == nil {
		return SlackMessage{}, fmt.Errorf("%q is not a link to one message: use Copy link on the message, which gives https://<workspace>.slack.com/archives/<channel>/p<16 digits>", ref)
	}
	return SlackMessage{
		Channel: m[1],
		TS:      m[2][:10] + "." + m[2][10:],
		Team:    strings.TrimSuffix(strings.TrimSuffix(host, "slack.com"), "."),
	}, nil
}
