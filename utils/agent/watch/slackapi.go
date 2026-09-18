package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errSlackRateLimited = errors.New("slack: rate limited")

type slackAPI struct {
	Token  string
	Client *http.Client
	URL    string
}

func (a *slackAPI) base() string {
	if a.URL != "" {
		return strings.TrimRight(a.URL, "/")
	}
	return "https://slack.com/api"
}

func (a *slackAPI) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (a *slackAPI) call(ctx context.Context, method string, params url.Values, out any) error {
	target := a.base() + "/" + method
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	return a.do(req, method, out)
}

func (a *slackAPI) post(ctx context.Context, method string, body map[string]any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base()+"/"+method, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return a.do(req, method, out)
}

func (a *slackAPI) do(req *http.Request, method string, out any) error {
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("slack %s: %w (retry after %s)", method, errSlackRateLimited, resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("slack %s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	if !envelope.OK {
		return fmt.Errorf("slack %s: %s", method, firstNonBlank(envelope.Error, "not ok"))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("slack %s: %v", method, err)
	}
	return nil
}

func firstNonBlank(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func slackPermalink(team, channel, ts, threadTS string) string {
	link := "https://" + team + ".slack.com/archives/" + channel + "/p" + strings.Replace(ts, ".", "", 1)
	if threadTS != "" && threadTS != ts {
		link += "?thread_ts=" + threadTS + "&cid=" + channel
	}
	return link
}

func slackTSTime(ts string) time.Time {
	secs, _, _ := strings.Cut(ts, ".")
	n, err := strconv.ParseInt(secs, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}

func slackTSNewer(a, b string) bool {
	af, aerr := strconv.ParseFloat(a, 64)
	bf, berr := strconv.ParseFloat(b, 64)
	if aerr != nil || berr != nil {
		return a > b
	}
	return af > bf
}
