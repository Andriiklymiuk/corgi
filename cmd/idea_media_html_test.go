package cmd

import (
	"strings"
	"testing"
)

func TestAReadmesHTMLPicturesShowOnThePage(t *testing.T) {
	md := "<div align=\"center\">\n  <img width=\"300\" src=\"./resources/corgi.png\" alt=\"corgi\">\n\n# CORGI\n\n[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)\n</div>\n\n" +
		"<p align=\"center\"><img src=\"docs/media/run.gif\" width=\"900\" alt=\"corgi run\"></p>\n<p align=\"center\"><sub>One command.</sub></p>\n\n" +
		"<a href=\"https://sonarcloud.io/x\"><img src=\"https://sonarcloud.io/badge.svg\" alt=\"Quality Gate\"></a>\n\n" +
		"```html\n<img src=\"keep.png\">\n```\n\nInline `<img src=\"code.png\">` stays.\n"
	out := renderIdeaHTML(md)
	for _, want := range []string{
		`<img data-file="corgi.png" alt="corgi" loading="lazy" style="width:300px" />`,
		`<img data-file="run.gif" alt="corgi run" loading="lazy" style="width:900px" />`,
		`<a href="https://sonarcloud.io/x"><img src="https://sonarcloud.io/badge.svg" alt="Quality Gate" loading="lazy" />`,
		`<em>One command.</em>`,
		`<h1 id="corgi">CORGI</h1>`,
		`&lt;img src=&quot;keep.png&quot;&gt;`,
		`<code>&lt;img src=&quot;code.png&quot;&gt;</code>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "run.gif\" alt=\"corgi run\" loading=\"lazy\" style=\"width:900px\" />\n<em>") {
		t.Fatalf("the caption is a paragraph of its own, not beside the picture: %s", out)
	}
	if strings.Contains(out, "<div") || strings.Contains(out, `align="center"`) {
		t.Fatalf("wrappers go: %s", out)
	}
	if got := htmlMediaToMarkdown(`<img src="javascript:alert(1)" onerror="x()">`); strings.Contains(renderIdeaHTML(got), "javascript") || strings.Contains(renderIdeaHTML(got), "onerror") {
		t.Fatalf("no script survives: %s", renderIdeaHTML(got))
	}
}
