package watch

import (
	"context"
	"strings"
	"testing"
)

// One comment per ticket, sections rewritten in place: the second write
// updates, it does not append, and a section can be replaced or removed.
func TestTheWorkpadIsOneCommentThatGrows(t *testing.T) {
	board := &leaseTracker{}
	ctx := context.Background()
	_ = board.Comment(ctx, "ABC-1", "any progress on this?")

	if err := UpsertWorkpad(ctx, board, "ABC-1", "Spec", "429 on the limit, banner on the web"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertWorkpad(ctx, board, "ABC-1", "Pull requests", "https://github.com/acme/api/pull/7"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertWorkpad(ctx, board, "ABC-1", "Spec", "429 on the limit; the banner is a follow-up"); err != nil {
		t.Fatal(err)
	}
	comments, _ := board.RecentComments(ctx, "ABC-1", 50)
	if len(comments) != 2 {
		t.Fatalf("one chat comment and one workpad, got %d", len(comments))
	}
	pad, ok := ParseWorkpad(comments[1].Body)
	if !ok || pad.Sections["Spec"] != "429 on the limit; the banner is a follow-up" || !strings.Contains(pad.Sections["Pull requests"], "pull/7") {
		t.Fatalf("sections: %+v", pad)
	}
	if pad.Order[0] != "Spec" || pad.Order[1] != "Pull requests" {
		t.Fatalf("order kept: %v", pad.Order)
	}
	if err := UpsertWorkpad(ctx, board, "ABC-1", "Pull requests", ""); err != nil {
		t.Fatal(err)
	}
	comments, _ = board.RecentComments(ctx, "ABC-1", 50)
	if strings.Contains(comments[1].Body, "Pull requests") {
		t.Fatal("an empty text removes the section")
	}
	if _, ok := ParseWorkpad("corgi opened https://x for this."); ok {
		t.Fatal("an old-style comment is not a workpad")
	}
}
