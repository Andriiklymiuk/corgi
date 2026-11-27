package watch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBulletLinesBecomeARealSlackList(t *testing.T) {
	text := PullLines("[ABC-12] Phone field", []string{
		"https://github.com/acme/api/pull/5",
		"https://github.com/acme/web/pull/9",
	}) + "\n\nMerge the api first."
	raw, _ := json.Marshal(slackBulletListBlocks(text))
	want := `[{"elements":[` +
		`{"elements":[{"text":"[ABC-12] Phone field","type":"text"}],"type":"rich_text_section"},` +
		`{"elements":[` +
		`{"elements":[{"text":"api: ","type":"text"},{"type":"link","url":"https://github.com/acme/api/pull/5"}],"type":"rich_text_section"},` +
		`{"elements":[{"text":"web: ","type":"text"},{"type":"link","url":"https://github.com/acme/web/pull/9"}],"type":"rich_text_section"}` +
		`],"indent":0,"style":"bullet","type":"rich_text_list"},` +
		`{"elements":[{"text":"\nMerge the api first.","type":"text"}],"type":"rich_text_section"}` +
		`],"type":"rich_text"}]`
	if string(raw) != want {
		t.Fatalf("got:\n%s", raw)
	}
}

func TestATextWithoutDotsOrWithMarkupStaysPlain(t *testing.T) {
	for _, text := range []string{
		"[ABC-12] Phone field\nhttps://github.com/acme/api/pull/5",
		"Opened\n• api: https://github.com/acme/api/pull/5\n• cc <@U123>",
		"• *bold* one\n• two",
		"• done :tada:",
		"• ",
	} {
		if blocks := slackBulletListBlocks(text); blocks != nil {
			t.Fatalf("%q must post as plain text, got %+v", text, blocks)
		}
	}
}

func TestALinkKeepsItsClosingPunctuationOutside(t *testing.T) {
	raw, _ := json.Marshal(richTextElements("see https://github.com/acme/api/pull/5), then merge."))
	want := `[{"text":"see ","type":"text"},{"type":"link","url":"https://github.com/acme/api/pull/5"},{"text":"), then merge.","type":"text"}]`
	if string(raw) != want {
		t.Fatalf("got:\n%s", raw)
	}
}

func TestSlackPosterSendsTheListAndFallsBackToPlainText(t *testing.T) {
	text := "Opened\n• api: https://github.com/acme/api/pull/5\n• web: https://github.com/acme/web/pull/9"
	refuse := true
	srv, calls := fakeSlack(t, func(string) string {
		if refuse {
			refuse = false
			return `{"ok":false,"error":"invalid_blocks"}`
		}
		return `{"ok":true,"channel":"C0RE","ts":"1726000900.000100"}`
	})
	p := posterAt(srv, Secrets{SlackBot: "xoxb-app"})

	got, err := p.Post(context.Background(), SlackTarget{Channel: "C0RE"}, text)
	if err != nil || got.TS == "" {
		t.Fatalf("a refused list must still post as plain text: %+v %v", got, err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d", len(*calls))
	}
	first, second := (*calls)[0].body, (*calls)[1].body
	if blocks, _ := first["blocks"].([]any); len(blocks) != 1 || first["text"] != text {
		t.Fatalf("the list goes as one rich text block beside the text: %+v", first)
	}
	if _, has := second["blocks"]; has || second["text"] != text {
		t.Fatalf("the second try is the text alone: %+v", second)
	}

	if _, err := p.Update(context.Background(), SlackMessage{Channel: "C0RE", TS: "1726000900.000100"}, "", text+"\n• app: https://github.com/acme/app/pull/2"); err != nil {
		t.Fatal(err)
	}
	edit, _ := json.Marshal((*calls)[2].body["blocks"])
	if !strings.Contains(string(edit), "rich_text_list") || !strings.Contains(string(edit), "acme/app/pull/2") {
		t.Fatalf("an edit carries the new list: %s", edit)
	}
}
