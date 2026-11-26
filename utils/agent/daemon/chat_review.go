package daemon

import (
	"context"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

const fixedSaidEvery = 10 * time.Minute

func reviewWords(spec WatchSpec) *config.SlackReview {
	if spec.Chat == nil {
		return nil
	}
	return spec.Chat.Review
}

func isChannelReview(spec WatchSpec, e watch.Event) bool {
	return reviewWords(spec) != nil && e.Source == "slack" && e.Kind == watch.KindReviewRequested
}

func repoOfPull(link string) string {
	ref := watch.PullRef(link)
	if i := strings.LastIndexAny(ref, "#!"); i > 0 {
		return ref[:i]
	}
	return ""
}

func withoutSkippedRepos(spec WatchSpec, e watch.Event) watch.Event {
	if !isChannelReview(spec, e) {
		return e
	}
	var kept []string
	for _, link := range e.Links {
		repo := repoOfPull(link)
		if reviewWords(spec).Skips(repo) {
			e.Skipped = append(e.Skipped, repo)
			continue
		}
		kept = append(kept, link)
	}
	e.Links, e.Skipped = kept, uniqueStrings(e.Skipped)
	return e
}

func namesOnlyWatchedRepos(spec WatchSpec, e watch.Event) bool {
	if !isChannelReview(spec, e) || len(e.Links) == 0 || len(spec.Repos) == 0 {
		return false
	}
	for _, link := range e.Links {
		watched := false
		for _, repo := range spec.Repos {
			watched = watched || strings.EqualFold(repo, repoOfPull(link))
		}
		if !watched {
			return false
		}
	}
	return true
}

func (d *Daemon) rememberOwnReviewPost(spec WatchSpec, e watch.Event) {
	parts := strings.Split(e.Key, ":")
	if !isChannelReview(spec, e) || !e.Self || len(e.Links) == 0 || len(parts) != 3 || d.watchState.Thread(e.Key) != "" {
		return
	}
	thread := watch.ChannelPost{Workspace: spec.Workspace, Channel: parts[1], TS: parts[2], Refs: watch.PullRefsIn(e.Links), At: time.Now()}
	if err := watch.RememberChannelPost(d.Dir, thread, time.Now()); err != nil {
		utils.Infof("agent: review thread of %s not kept: %v\n", e.Ref, err)
	}
}

func (d *Daemon) sayReviewVerdict(ctx context.Context, run fixRun) {
	words, e := reviewWords(run.spec), run.e
	approvedAll, comments := len(e.Links) > 0, false
	for _, link := range e.Links {
		var one watch.ReviewOutcome
		reviewSaid(ctx, run.spec, watch.PullRef(link), run.started, &one)
		approvedAll = approvedAll && one.Approved
		comments = comments || one.Comments > 0 || one.ChangesRequested
	}
	switch {
	case comments:
		d.say(ctx, run.spec, e, words.CommentsWord(), "")
	case approvedAll && len(e.Skipped) > 0:
		d.say(ctx, run.spec, e, words.ApprovedWord()+", "+strings.Join(shortRepoNames(e.Skipped), ", ")+" left to its owner", "")
	case approvedAll:
		d.say(ctx, run.spec, e, words.ApprovedWord(), "white_check_mark")
	}
}

func shortRepoNames(repos []string) []string {
	var out []string
	for _, r := range repos {
		out = append(out, r[strings.LastIndex(r, "/")+1:])
	}
	return out
}

func (d *Daemon) sayFixedInReviewThreads(ctx context.Context, spec WatchSpec, e watch.Event) {
	words := reviewWords(spec)
	if words == nil || d.Chat == nil || e.Source == "slack" || (e.Kind != watch.KindPRComment && e.Kind != watch.KindPRReview) {
		return
	}
	refs := watch.PullRefsOf(e)
	for _, r := range e.Riders {
		refs = append(refs, watch.PullRefsOf(r)...)
	}
	var reader watch.ReviewThreadReader
	for _, src := range spec.Sources {
		if r, ok := src.(watch.ReviewThreadReader); ok {
			reader = r
		}
	}
	if reader == nil {
		return
	}
	now := time.Now()
	for _, t := range watch.ChannelPostsNaming(d.Dir, spec.Workspace, refs) {
		if now.Sub(t.FixedAt) < fixedSaidEvery || !reader.AwaitsMe(ctx, t.Channel, t.TS) {
			continue
		}
		target := watch.SlackTarget{Channel: t.Channel, ThreadTS: t.TS, As: spec.Chat.ReplyAs}
		if err := d.Chat(ctx, spec.Workspace, target, words.FixedWord(), ""); err != nil {
			utils.Infof("agent: chat reply for %s: %v\n", e.Ref, err)
			continue
		}
		_ = watch.MarkChannelPostFixed(d.Dir, t, now)
	}
}
