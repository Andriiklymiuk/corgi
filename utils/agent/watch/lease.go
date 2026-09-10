package watch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// A lease stops two machines working the same ticket. The seen index, the
// active-fix claim and the caps all live on one machine's disk, so a laptop
// and a desktop watching one board will both take the same ticket and open
// two pull requests for it. The tracker is the one thing both can see, so the
// claim goes there — as a comment, which every tracker has and neither needs
// new infrastructure for.
const (
	leaseMarker = "corgi-lease"
	// LeaseTTL is how long a claim holds. Longer than a fix takes, short
	// enough that a machine that died mid-run frees the ticket the same
	// morning rather than blocking it for good.
	LeaseTTL = 90 * time.Minute
)

// Lease is one machine's claim on a ticket.
type Lease struct {
	Machine string
	At      time.Time
}

// String is the one line posted to the tracker. Deliberately dull and
// machine-readable: a person reading the ticket should be able to tell at a
// glance that it is bookkeeping, not a comment meant for them.
func (l Lease) String() string {
	return fmt.Sprintf("%s %s %s (corgi is working on this; the claim lapses on its own)",
		leaseMarker, l.Machine, l.At.UTC().Format(time.RFC3339))
}

// Expired says the claim is old enough to ignore.
func (l Lease) Expired(now time.Time) bool { return now.Sub(l.At) > LeaseTTL }

// ParseLease reads a claim out of a comment body, if it is one.
func ParseLease(body string) (Lease, bool) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 || fields[0] != leaseMarker {
			continue
		}
		at, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		return Lease{Machine: fields[1], At: at}, true
	}
	return Lease{}, false
}

// MachineName is what this machine calls itself in a claim. The hostname,
// which is what a person reading the ticket would recognise.
func MachineName() string {
	if name, err := os.Hostname(); err == nil && strings.TrimSpace(name) != "" {
		return strings.ReplaceAll(strings.TrimSpace(name), " ", "-")
	}
	return "a-machine"
}

// HeldBy is the live claim on a ticket by a machine other than this one, from
// its recent comments. "" means nobody else holds it.
func HeldBy(comments []Comment, me string, now time.Time) string {
	for _, c := range comments {
		lease, ok := ParseLease(c.Body)
		if !ok || lease.Expired(now) || lease.Machine == me {
			continue
		}
		return lease.Machine
	}
	return ""
}

// Claim takes the ticket for this machine, or reports who already has it.
//
// Read, then write, then read again. The second read is what settles a race:
// if two machines both found the ticket free and both claimed it, the one
// whose claim is older keeps it and the other stands down. Its own claim is
// left to lapse rather than deleted — a delete is another write to lose a
// race on, and a stale claim costs one skipped run.
func Claim(ctx context.Context, w Writer, ref, me string, now time.Time) (ok bool, holder string, err error) {
	before, err := w.RecentComments(ctx, ref, 20)
	if err != nil {
		// A tracker that cannot be read cannot be claimed on. Refusing to run
		// would be worse than the duplicate work it is guarding against, so
		// the caller decides; say so plainly.
		return false, "", err
	}
	if holder = HeldBy(before, me, now); holder != "" {
		return false, holder, nil
	}
	mine := Lease{Machine: me, At: now}
	if err := w.Comment(ctx, ref, mine.String()); err != nil {
		return false, "", err
	}
	after, err := w.RecentComments(ctx, ref, 20)
	if err != nil {
		return true, "", nil // we posted; a failed re-read is not a reason to stop
	}
	for _, c := range after {
		other, isLease := ParseLease(c.Body)
		if !isLease || other.Machine == me || other.Expired(now) {
			continue
		}
		// Someone else claimed it too. The older claim wins; a tie goes to
		// the lexicographically smaller name so both machines decide the
		// same way without talking to each other.
		if other.At.Before(mine.At) || (other.At.Equal(mine.At) && other.Machine < me) {
			return false, other.Machine, nil
		}
	}
	return true, "", nil
}
