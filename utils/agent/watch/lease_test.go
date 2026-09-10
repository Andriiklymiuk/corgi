package watch

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestALeaseSaysWhoAndWhenAndLapses(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	line := Lease{Machine: "laptop", At: now}.String()

	got, ok := ParseLease(line)
	if !ok || got.Machine != "laptop" || !got.At.Equal(now) {
		t.Fatalf("a claim has to survive the round trip: %+v", got)
	}
	// A person reading the ticket should see it is bookkeeping.
	if !strings.Contains(line, "corgi is working on this") {
		t.Fatalf("the claim must explain itself: %q", line)
	}
	// It is found even with a person's comment around it.
	if _, ok := ParseLease("looks good to me\n" + line + "\nthanks"); !ok {
		t.Fatal("a claim inside a longer comment still counts")
	}
	for _, notALease := range []string{"", "just a comment", "corgi-lease", "corgi-lease laptop not-a-time"} {
		if _, ok := ParseLease(notALease); ok {
			t.Errorf("%q is not a claim", notALease)
		}
	}

	if got.Expired(now.Add(time.Minute)) {
		t.Fatal("a fresh claim holds")
	}
	if !got.Expired(now.Add(LeaseTTL + time.Minute)) {
		t.Fatal("a machine that died mid-run must not block the ticket for good")
	}
}

func TestHeldByIgnoresMyOwnAndExpiredClaims(t *testing.T) {
	now := time.Now()
	mine := Comment{Body: Lease{Machine: "laptop", At: now}.String()}
	theirs := Comment{Body: Lease{Machine: "desktop", At: now}.String()}
	old := Comment{Body: Lease{Machine: "desktop", At: now.Add(-LeaseTTL - time.Hour)}.String()}
	chat := Comment{Body: "any progress on this?"}

	if HeldBy([]Comment{mine, chat}, "laptop", now) != "" {
		t.Fatal("my own claim does not stop me")
	}
	if HeldBy([]Comment{old, chat}, "laptop", now) != "" {
		t.Fatal("a lapsed claim does not stop anyone")
	}
	if got := HeldBy([]Comment{chat, theirs}, "laptop", now); got != "desktop" {
		t.Fatalf("another machine's live claim stops me: %q", got)
	}
}

// leaseTracker is a tracker two machines can both post to, so the race can
// actually be run rather than reasoned about.
type leaseTracker struct {
	mu       sync.Mutex
	comments []Comment
	readErr  error
}

func (f *leaseTracker) Name() string                                       { return "fake" }
func (f *leaseTracker) Statuses(context.Context) ([]Status, error)         { return nil, nil }
func (f *leaseTracker) Whoami(context.Context) (Identity, error)           { return Identity{}, nil }
func (f *leaseTracker) Move(context.Context, string, string) error         { return nil }
func (f *leaseTracker) Assign(context.Context, string, string) error       { return nil }
func (f *leaseTracker) Comment(_ context.Context, _, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, Comment{Body: body})
	return nil
}
func (f *leaseTracker) RecentComments(context.Context, string, int) ([]Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	return append([]Comment(nil), f.comments...), nil
}

func TestTwoMachinesOneTicketOneRun(t *testing.T) {
	board := &leaseTracker{}
	now := time.Now()

	// Both find it free and both claim: the older claim keeps it.
	ok, holder, err := Claim(context.Background(), board, "ABC-1", "laptop", now)
	if err != nil || !ok || holder != "" {
		t.Fatalf("the first machine takes it: ok=%v holder=%q err=%v", ok, holder, err)
	}
	ok, holder, err = Claim(context.Background(), board, "ABC-1", "desktop", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ok || holder != "laptop" {
		t.Fatalf("the second machine stands down and says who has it: ok=%v holder=%q", ok, holder)
	}

	// The same machine coming back to its own ticket is not blocked by itself.
	if ok, _, _ := Claim(context.Background(), board, "ABC-1", "laptop", now.Add(time.Minute)); !ok {
		t.Fatal("my own claim must not lock me out")
	}

	// Once it lapses, the other machine may take it.
	if ok, _, _ := Claim(context.Background(), board, "ABC-1", "desktop", now.Add(LeaseTTL+time.Hour)); !ok {
		t.Fatal("a lapsed claim frees the ticket")
	}
}

// A dead race: both read an empty board before either posted. They must not
// both proceed, and both must reach the same verdict without talking.
func TestASimultaneousClaimIsSettledTheSameWayByBoth(t *testing.T) {
	now := time.Now()
	board := &leaseTracker{}
	// Both posted at the same instant; the tie goes to the smaller name so
	// each machine, reading the same board, decides the same thing.
	_ = board.Comment(context.Background(), "ABC-1", Lease{Machine: "desktop", At: now}.String())
	ok, holder, err := Claim(context.Background(), board, "ABC-1", "laptop", now)
	if err != nil {
		t.Fatal(err)
	}
	if ok || holder != "desktop" {
		t.Fatalf("laptop must stand down for desktop on a tie: ok=%v holder=%q", ok, holder)
	}
}

// A tracker that cannot be read leaves the claim unknown. Claim says so and
// lets the caller decide, rather than silently allowing a duplicate.
func TestAnUnreadableTrackerIsReportedNotGuessed(t *testing.T) {
	board := &leaseTracker{readErr: context.DeadlineExceeded}
	ok, holder, err := Claim(context.Background(), board, "ABC-1", "laptop", time.Now())
	if err == nil {
		t.Fatal("a failed read has to surface")
	}
	if ok || holder != "" {
		t.Fatalf("nothing is claimed when nothing could be read: ok=%v holder=%q", ok, holder)
	}
}

func TestMachineNameIsUsableInAClaim(t *testing.T) {
	name := MachineName()
	if name == "" || strings.ContainsAny(name, " \t\n") {
		t.Fatalf("a name with spaces would break the one-line claim: %q", name)
	}
	if _, ok := ParseLease(Lease{Machine: name, At: time.Now()}.String()); !ok {
		t.Fatal("this machine's own name must round-trip")
	}
}
