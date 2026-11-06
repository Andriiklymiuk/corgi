---
name: unslop
description: "Use when a UI or a text looks AI-made, generic or too colourful: \"unslop this\", \"it looks like AI slop\", \"too many cards\", \"make it look like Linear\", \"this reads like ChatGPT\", \"make this sound human\". NOT matching a Figma (design-parity) or a one-off UI bug."
---

# Unslop a UI or a text

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** the scan below is clean or each hit has a reason, every main screen and its empty,
loading and error states were read as screenshots in light and dark, contrast is measured, and tests
that pin the palette pass.

## What slop is

Slop is not ugliness. It is the same defaults with no decision behind them: every block boxed,
every status a filled colour, every number a tile, every icon in a tinted circle. The fix is
fewer things on the screen, not more styling. Do not swap one cliche for another.

A text only (a README, a release note, a store page, a PR body, a doc): go straight to "Writing" below.
Change the words and keep the meaning and the facts.

## Work from the shared parts down

```
look at the screens ──▶ tokens ──▶ shared parts ──▶ one-off screens ──▶ look again
                                                                         │
                                         contrast + tests + both modes ◀─┘
```

1. **Look first.** Run the app on made-up data and capture every main screen, plus sheets and
   empty states. Slop is judged on pixels, never from code. Squint at each: can you name the one
   thing that matters? If everything shouts, nothing does.
2. **Scan.** Run the scan below, count hits per file, and fix the shared part that causes most of them.
3. **Tokens.** Rewrite the palette, type scale and spacing in one file.
4. **Shared parts.** Section, row, button, chip, note, empty state. Most screens fix themselves.
5. **One-offs.** Go through the scan again, screen by screen.
6. **Look again**, in light and dark, at the smallest and largest window.

Keep behaviour, navigation and copy unless they are the problem. A design pass that also moves
features cannot be reviewed.

## Rules

### Colour

- **Neutral surfaces.** Backgrounds carry almost no chroma: under 12 between the highest and lowest
  RGB channel. A tint across every surface is what makes an app look themed by a generator.
- **One accent, one job.** It means "you can act here". Keep it out of the palette maths, so the
  neutrals stay neutral. A brand mark or icon can keep its own colours.
- **One filled button per view.** Secondary actions are neutral. If two things are primary, one is not.
- **Status colour goes on a glyph or a word, never on a surface.** No tinted rows, cards or banners.
  A note is a neutral block with a coloured title.
- **One colour, one meaning.** Decide them and write them down, for example: indigo = work moving,
  amber = needs you, red = broken, green = a good result. Amber must not mean both "running" and
  "waiting". Finished work recedes to grey.
- **Measure, do not trust your eye.** Body text 4.5:1 or more, muted text too. Check every pair in
  both appearances, and on the surfaces it really sits on (a chip is not the page). Pin it in a test.
- **Dark is not inverted light.** No pure #000 or #fff grounds. Raise surfaces in lightness steps.

### Structure

- **Rows, not cards.** A list is rows on the page with hairlines between them. Add a container only
  when space alone cannot group it: a composer, a form, a block of text that must hold together.
- **Never a card in a card.** Never a border on every block. Drop dividers that have no reason.
- **No hero numbers.** Four tiles with big numbers that repeat the header line are decoration. Say it
  in one line, or give the number a comparison, a trend or a limit it is measured against.
- **Do not say it twice.** "Working" under a heading "Working", a title in both the top bar and the
  page, a status in the header and a chip beside it. Cut one.
- **Facts are text.** A dozen outlined pills are noise: write `branch · 6 files · tests passed`. Only
  what can be tapped looks tappable.
- **Align on one edge.** Avatar plus glyph plus indicator pushes titles out of line. One leading mark.

### Space and type

- **A scale, not numbers.** 4, 8, 12, 16, 24, 32. The gap between groups is at least twice the gap
  inside one, and a heading sits closer to its content than to what came before.
- **Few sizes.** Three or four text sizes do the work. Hierarchy comes from weight and contrast first.
- **Sentence case headings.** Small tracked capitals over every group are a template tell.
- **Monospace is for code.** Commands, diffs, ids a person copies. Not for labels or "tech vibes".
- **Tabular numbers** wherever numbers line up or update.
- **Touch and pointer.** 44pt targets on touch; a small control gets hit slop. Native bars and sheets
  stay native: platform chrome is not the slop.

### Icons and motion

- **Icons earn their place.** A row of properties needs labels, not an icon each. No icon in a tinted
  circle or square above a heading.
- **Sparkles are not an icon for "AI".** Name what it does: ask, explain, summarise.
- **No emoji as interface.** Use the platform's symbols, or a word.
- **One icon set, one stroke weight**, close to the weight of the text beside it.
- **Motion says something changed.** No glow, no breathing blobs, no bounce on every press. Press
  scale stays at 0.96 or above; rows highlight instead. Respect reduced motion.
- **No colored shadows, no gradients for depth, no glass under text.** System glass stays on chrome.
- **One-sided accent borders** are the best-known tell. Use a small dot, a tint of text, or nothing.
  A quote's rule is typography, not an accent.

### States

A screen has more states than the full one. Design each; the empty and broken ones are what a new
person sees first.

- **Loading.** Under a second, show nothing new. Over a second, show that it is working. Over ten, show
  progress and a way out. Keep the old content up while refreshing; a spinner over data you already
  have is a step back. Never say "no results" while still loading.
- **Empty.** Never a blank panel. Say what belongs here, how it gets filled, and offer the action that
  does it. "Star a file to keep it here", not "Nothing here". An illustration is not the message.
- **Error.** Say what happened in plain words and what to do next, next to what caused it. Keep what
  the person typed. Offer Retry when a retry can work. No "Something went wrong", no codes unless they
  help support, no "invalid", no blame, no jokes.
- **Offline, slow, stale.** Say the data is old and from when, and which actions still work.
- **Busy actions.** Respond within 100 ms, even if only by pressing in. Disable while sending so a second
  tap cannot send twice. Confirm what cannot be undone, or better, offer Undo.
- **Long and many.** Try the longest real name, 0, 1 and 1000 rows, the largest text size, the smallest
  window. A layout that only works with the demo's data is not done.
- **Never colour alone.** A state also has a word or a glyph.

### Words

- **No greetings.** "Good morning, Sam" takes the best spot on the page and tells the person nothing.
- **Buttons name the action.** "Allow", not "Continue". A heading says what is here, not "Overview".
- **Say it short, and in the reader's words.** One idea per sentence, active voice, the same word for the
  same thing on every screen. Cut what the screen already shows. No "Please", no "successfully", no
  feature names a person never learned.
- **Hints once.** Teach in context, then get out of the way. A paragraph under every control is noise.

## Writing

Copy on the screen, and anything written for people: store pages, READMEs, release notes, PR bodies.

**Say what it does.**
- Name the mechanism or a number, not a feeling. "Types that follow your schema" becomes "a renamed
  column fails the build". Ask what the sentence tells the reader to do or know. No answer, cut it.
- **The swap test.** If the sentence could sit unchanged in another product's docs, it says nothing about
  this one.
- Name the source or drop the claim: no "experts say", "many teams find".
- End on a fact or a plan, not "the future looks bright".

**Plain words.**
- `is` and `has`, not "serves as", "stands as", "boasts", "features".
- `use`, `help`, `many`, `if`, `to`, not "leverage", "facilitate", "numerous", "in the event that",
  "in order to".
- Cut the generator's words: delve, crucial, pivotal, robust, seamless, tapestry, landscape, testament,
  underscore, foster, showcase, vibrant, additionally.
- Cut metaphor nouns that pose as technical: substrate, wedge, north star, flywheel, paradigm, bedrock.
  Pick the concrete word.
- A cut adverb or a stronger verb, or the number: "significantly faster" becomes "2 s to 200 ms".

**Sentences.**
- One idea each, whole, with its verb. Active: "the compiler checks queries", not "queries are checked".
  Passive only when the actor truly does not matter.
- No "not just X, but Y": say Y. No trailing "-ing" clause that adds nothing: "..., ensuring quality".
- No forced threes. Use the number you have.
- One word for one thing. Do not rotate synonyms for variety.
- No "from X to Y" unless X and Y are ends of a real scale.
- No flourish: aphorisms, fragments for effect, code that "rides along". A literal phrase exists.
- Do not over-compress either. Arrows, dropped articles and private abbreviations make the reader decode.

**Marks.**
- Few em dashes, or none. A colon before a list or example, not as a connector in a sentence.
- Bold for the rare word that must be found, not every name. A bold label that restates its own line goes.
- Sentence case headings, straight quotes, no decorative emoji.

**Tone.**
- No chatbot talk: "I hope this helps", "Certainly!", "Great question", "Let me know if".
- No filler: "it is important to note that". One hedge at most: "may", not "could potentially be argued".

## The scan

Adapt the globs to the repo. These find candidates: read each hit, some are fine.

```bash
# one-sided borders, gradients, glows
grep -rnE 'border-?[Ll]eft(Width|:)|linear-gradient|LinearGradient|shadowColor: *C\.accent|box-shadow:.*(rgba\(.*(99|124|139))' src
# hard-coded colours outside the theme file
grep -rnE "#[0-9a-fA-F]{6}\b" src | grep -v theme
# emoji in the interface
grep -rnP '[\x{1F300}-\x{1FAFF}]' src
# AI icon, tracked capitals, rounded display faces, tints on surfaces
grep -rnE "sparkles|textTransform: *'uppercase'|\.uppercased\(\)|design: *\.rounded|Soft\b" src
# cards everywhere: a border and a radius on the same block
grep -rnE "borderWidth: *1.*borderRadius|borderRadius.*borderWidth: *1|strokeBorder" src | wc -l
# stat tiles and icon discs
grep -rnE "StatCard|MetricCard|<Tiles?\b|StatTile|Circle\(\)\.fill\(.*Soft" src
# the generator's words, in copy and docs
grep -rniE "\b(delve|pivotal|seamless(ly)?|tapestry|testament|underscore|leverag|facilitat|utiliz|in order to|serves as)\b" README.md docs store fastlane marketing src 2>/dev/null
```

## Proof

- **Read the screenshots**, every one, in both appearances, including a sheet over a list and the
  empty, loading and error states: cut the network to see them. A passing build proves nothing about how it looks.
- **Check light mode for white-on-white**: an icon that used the on-accent colour on a surface that
  stopped being the accent is the usual casualty.
- **Pin it.** Tests for palette parity between appearances, contrast of text pairs, and neutral grounds.
- **Store and README pictures follow.** They show the old look until retaken, and so does a store's
  preview video. Open each one and check it shows the current build (see `mobile-screenshots`, `showcase`).
- **Say what you did not test** on real hardware.

## Sources

Linear, [how we redesigned the UI](https://linear.app/now/how-we-redesigned-the-linear-ui) and
[behind the latest design refresh](https://linear.app/now/behind-the-latest-design-refresh): a
theme from base, accent and contrast; limit how much the accent feeds the palette; navigation
recedes; fewer dividers. Nielsen Norman Group,
[visual hierarchy](https://www.nngroup.com/articles/visual-hierarchy-ux-definition/): few sizes and
colours, containers only when space is not enough, the squint test;
[error messages](https://www.nngroup.com/articles/error-message-guidelines/),
[empty states](https://www.nngroup.com/articles/empty-state-interface-design/) and
[response times](https://www.nngroup.com/articles/response-times-3-important-limits/).
[pixelslop's catalog](https://cdn.jsdelivr.net/npm/pixelslop@0.3.8/dist/skill/resources/ai-slop-patterns.md):
detection rules for the common patterns. Treat the tell lists as prompts, not law: a pattern is
fine when it has a reason, and the catalog itself comes from opinion, not studies.

## See also

- **[`design-parity`](../design-parity/SKILL.md)** when there is a design to match.
- **[`before-after`](../before-after/SKILL.md)** to attach the proof to a PR.
- **[`mobile-screenshots`](../mobile-screenshots/SKILL.md)** and **[`showcase`](../showcase/SKILL.md)** to retake the pictures.
- **[`quality`](../quality/SKILL.md)** for the code under the screen.
