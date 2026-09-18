package watch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	leaseMarker = "corgi-lease"
	LeaseTTL    = 90 * time.Minute
)

type Lease struct {
	Machine string
	At      time.Time
}

func (l Lease) String() string {
	return fmt.Sprintf("%s %s %s (corgi is working on this; the claim lapses on its own)",
		leaseMarker, l.Machine, l.At.UTC().Format(time.RFC3339))
}

func (l Lease) Expired(now time.Time) bool { return now.Sub(l.At) > LeaseTTL }

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

func MachineName() string {
	if name, err := os.Hostname(); err == nil && strings.TrimSpace(name) != "" {
		return strings.ReplaceAll(strings.TrimSpace(name), " ", "-")
	}
	return "a-machine"
}

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

func Claim(ctx context.Context, w Writer, ref, me string, now time.Time) (ok bool, holder string, err error) {
	before, err := w.RecentComments(ctx, ref, 20)
	if err != nil {
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
		return true, "", nil
	}
	for _, c := range after {
		other, isLease := ParseLease(c.Body)
		if !isLease || other.Machine == me || other.Expired(now) {
			continue
		}
		if other.At.Before(mine.At) || (other.At.Equal(mine.At) && other.Machine < me) {
			return false, other.Machine, nil
		}
	}
	return true, "", nil
}
