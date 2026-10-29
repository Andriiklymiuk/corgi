---
name: showcase
description: "Use when the user wants README pictures, animated demos or a short video of a CLI, app, or extension: \"make screenshots for the README\", \"showcase gif\", \"demo video\", \"marketplace media\", \"hero image\". Draws the UI as HTML from the product's own strings, in the project's own colours, with a caption that says what you are watching. NOT for phone store screenshots (mobile-screenshots) or a running app (mobile)."
---

# Showcase: README pictures, GIFs and short videos from HTML

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** the pictures are rendered from the script in the project's look, every gif carries its caption, you looked at each one, and the README (or listing) points at them with a line that says what happens.

Pictures drawn from the product's own output, not from a design tool. A
terminal session, a menu bar dropdown, a VS Code window, a phone page - each
is an HTML mockup whose every line is the string the code prints, rendered by
headless Chrome at 2x, cut and joined by ImageMagick. Re-run the script after
a UI change and the README is current again.

## When this fits

- A README, store listing, or docs page needs a hero, a feature picture, or an
  animation of a flow.
- The product is a CLI, a desktop app, an editor extension, a phone web page,
  or a hardware surface (a Stream Deck) whose look can be reproduced in HTML.
- The user wants it to look professional and to stay in step with the code.

## The project's look

A gif in GitHub's default dark is anybody's gif. Before drawing, find what
makes this project's:

- **The logo.** `resources/`, `assets/`, `docs/`, the app icon, the extension
  icon, `favicon`. It goes in the caption strip of every frame, small (28 px),
  so the loop is branded without a title card that the reader waits through.
- **The palette.** Pull it from the logo (`grep -o 'fill="#[0-9a-fA-F]*"'
  logo.svg | sort | uniq -c | sort -rn`) and from the product's own theme (the
  launcher page CSS, the app's colour tokens, the ANSI palette the CLI maps
  to). Pick one ground colour tinted from the brand's dark (corgi: navy
  `#000072` → ground `#0a0e2a`), one accent for the caption and the
  highlighted result line (corgi: orange `#FFA133`), one text colour
  (corgi: cream `#FDF6F2`). Everything inside a terminal or an app window
  keeps the product's own colours - those are verbatim too.
- **One strip, every frame.** `<logo> <caption>` above the scene, in the
  brand text colour, the same on every frame of every scene, so twenty gifs
  read as one product.

## Making it understandable

The reader does not know the product. Each scene answers three questions
without a word of prose around it:

1. **What was typed?** The first frame shows the command alone, cursor
   blinking, nothing else - a beat before the output.
2. **What is happening?** The frames fill in at a reading pace; a `{d}#
   comment{/}` line in the terminal says what the next lines mean when the
   output alone would not ("# 16:02 - claude's window is spent").
3. **What came of it?** The last frame lingers, and the line that matters is
   highlighted - `{H}…{/}` draws it on the accent colour with a left bar - so
   the eye lands on the PR URL, the green check, the push, not on the
   scrollback.

The caption strip is one plain sentence in the reader's words, not the
command's: "An agent takes the ticket across three repos and opens three
draft PRs", not "corgi stories ABC-123". Under the gif in the README, the
same sentence in `<sub>`, and `alt` text that says what happens for those
who cannot see it.

## The rules that make it good

1. **Verbatim strings.** Before drawing, read the code that prints: format
   strings, glyphs, colours, table layouts. Quote them in the mockup. A
   picture that shows output the code does not produce is a lie in the README.
2. **One scene, one idea.** A scene is a short story: the command, its first
   lines, the moment that matters, the result. Six to twelve frames. The
   last frame stays longest so the reader can read the result.
3. **Fixed canvas per scene.** Every frame of a GIF must be the same size:
   pad the terminal to the final line count, or extent all frames to the
   largest. A GIF that grows jumps.
4. **2x pixels, sized down in the README** (`width="760"`), so it is crisp on
   retina. Flat ground colour behind close-ups so `-trim` works.
5. **Real fonts and real icons.** System UI font for chrome, a mono face for
   terminals, real SF Symbols or codicons drawn as SVG, a real QR made by the
   same library the product uses. Emoji render in Chrome as they do on the
   Mac.
6. **The gif tells the truth about time.** Frame delay 110 to 140 cs; the last
   frame 300 cs. No frame that shows a state the product never has.
7. **No made-up names.** Workspaces `acme-api`, `web`, `mobile`; people
   `max`; tickets `ABC-123`. Never a client's real name, path, token or URL.
8. **A script in the repo, a make target, a note in the README.** The next
   person redraws with one command.
9. **Re-check old scenes when a new one lands.** Output drifts: a summary
   line grows a suffix (`→ fix` became `→ fix, at most 3/h 10/day`), a tab
   is renamed (Sessions → Agents), a version string ages. Before adding a
   scene, grep the current print statements for every line the existing
   scenes show and fix what moved; the new gif next to a stale one makes
   the stale one obvious.
10. **Two machines are two names, one story.** A multi-laptop feature is
    told from one terminal: the other laptop appears only as the product
    prints it (`on home-mbp (awake, leads):`, a ring "by home-mbp"), never
    as a second window - two terminals side by side are unreadable at
    README width. Use `yourdomain.com` for hostnames a user would own.

## Layout of the work

```
scripts/showcase.mjs        node: writes docs/media/frames/<scene>-<n>.html + scenes.json {name: {frames, caption}}
scripts/capture-showcase.sh Chrome → png (2x) → magick trim/extent → gif + still + mp4
docs/media/<scene>.gif      what the README embeds
docs/media/<scene>.png      the last frame, for docs that cannot animate
docs/media/<scene>.mp4      the same frames as a video for a store page, a post, a slide - gitignored, redrawn on demand
```

Chrome: `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
`--headless=new --hide-scrollbars --disable-gpu --force-device-scale-factor=2
--window-size=W,H --screenshot=<png> file://<html>`. Chrome clamps windows
narrower than about 500 px: draw small things big and shrink with magick.

## Templates

### A terminal window

```js
const c = { g: "#3fb950", r: "#ff7b72", y: "#e3b341", b: "#79c0ff", c: "#56d4dd", d: "#8b949e" };
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;");
// {g}green{/} {r}red{/} {y}yellow{/} {b}blue{/} {c}cyan{/} {d}dim{/} {B}bold{/}
const mark = (s) => esc(s).replace(/\{([grybcdB])\}/g, (_, k) => (k === "B" ? "<b>" : `<span style="color:${c[k]}">`))
	.replace(/\{\/\}/g, "</span>").replace(/<\/span>(?=[^<]*<\/b>)/g, "</b>");
const line = (s) => `<div class="l">${s === "" ? "&nbsp;" : mark(s)}</div>`;
const css = `
  html,body{margin:0;background:#0d1117;font-family:-apple-system,Inter,Helvetica,Arial,sans-serif;color:#e6edf3}
  .term{width:900px;background:#161b22;border:1px solid #30363d;border-radius:12px;box-shadow:0 24px 70px rgba(0,0,0,.6);overflow:hidden}
  .bar{height:38px;display:flex;align-items:center;padding:0 14px;background:#21262d;border-bottom:1px solid #30363d;font-size:12px;color:#8b949e;position:relative}
  .bar i{width:12px;height:12px;border-radius:50%;background:#ff5f57;margin-right:8px}.bar i+i{background:#febc2e}.bar i+i+i{background:#28c840}
  .bar span{position:absolute;left:0;right:0;text-align:center}
  .body{padding:14px 18px;font:13.5px/1.55 "SF Mono",ui-monospace,Menlo,monospace;white-space:pre}
  .l{min-height:21px}.cur{display:inline-block;width:8px;height:16px;background:#e6edf3;vertical-align:-3px}
  .body b{font-weight:600;color:#fff}`;
const term = (title, lines, { rows = lines.length, cursor = false } = {}) =>
	`<div class="term"><div class="bar"><i></i><i></i><i></i><span>${esc(title)}</span></div><div class="body">${lines.map(line).join("")}${cursor ? `<div class="l"><span class="cur"></span></div>` : ""}${`<div class="l">&nbsp;</div>`.repeat(Math.max(0, rows - lines.length - (cursor ? 1 : 0)))}</div></div>`;
const brand = { ground: "#0a0e2a", accent: "#FFA133", text: "#FDF6F2", logo: "../../../resources/logo.svg" };
const strip = (caption) => `<div class="cap"><img src="${brand.logo}" width="28" height="28">${esc(caption)}</div>`;
const page = (inner, caption) => `<!doctype html><meta charset="utf-8"><style>${css}</style>${strip(caption)}<div class="stage">${inner}</div>`;
const grow = (lines, cuts) => cuts.map((n) => lines.slice(0, n));   // frames that fill in
```

With `.cap{position:absolute;left:28px;top:18px;display:flex;align-items:center;gap:10px;font:600 15px/1 system-ui;color:${brand.text}}`,
`.stage{position:absolute;left:0;top:56px;padding:28px}`, `html,body{background:${brand.ground}}`
and `.l.hi{background:rgba(255,161,51,.14);box-shadow:inset 3px 0 ${brand.accent}}` for the
`{H}` line. The strip's height is part of every frame, so a gif never jumps.

Half-block QR codes (`█▀▄`) need `line-height:1` and Menlo, or the rows
show gaps.

### A phone

```css
.phone{width:300px;height:650px;border-radius:46px;background:#0b0b0d;border:4px solid #3a3a3e;box-shadow:0 40px 90px rgba(0,0,0,.65),inset 0 0 0 2px #000;position:relative;overflow:hidden}
.notch{position:absolute;left:50%;top:11px;transform:translateX(-50%);width:92px;height:28px;border-radius:16px;background:#000;z-index:5}
.sb{position:absolute;left:0;right:0;top:0;height:50px;z-index:4;display:flex;justify-content:space-between;align-items:center;padding:14px 26px 0 30px;font:600 14px/1 -apple-system,system-ui,sans-serif;color:#fff}
.home{position:absolute;left:50%;bottom:7px;transform:translateX(-50%);width:120px;height:5px;border-radius:3px;background:rgba(255,255,255,.85);z-index:6}
.screen{position:absolute;inset:0;overflow:hidden}
```

300 x 650 is an iPhone's ratio (393 x 852). Every phone gets the same
three pieces of chrome - a status bar (time, signal, Wi-Fi, battery as
inline SVG), the Dynamic Island, the home indicator - from one shared
`statusBar` string, so a lock screen, a chat and the app look like the same
device. Put the product's own page CSS inside `.screen` (copy its palette
and card rules from the source), and a lock screen or camera frame for the
"before".

**The tab bar.** Read the app's tab layout (`app/(tabs)/_layout.tsx` in an
Expo app) for the labels and the SF Symbol names, and draw those symbols as
inline SVG paths - not Unicode box glyphs (▤ ▥ ▮), which read as broken. A
system tab bar is a glass capsule: `border-radius:30px`, a translucent
fill, a 1 px hairline, the selected tab a lighter pill. Content scrolls
*under* it, so put a gradient (`transparent → page colour`, ~130 px) behind
the bar, or the rows collide with it. A badge is a red pill in the tab's
top-right corner.

**A Live Activity with buttons** needs two rows on a 300 px phone - title
and laptop on top, the ask and the buttons below - or everything truncates
to "Login re…". Show the pressed state (a ring on the button) and the state
after (the word the app writes, "allowed") as two more frames: the reader
sees the answer land.

### A desktop app or menu bar

Draw the OS chrome once (menu bar 38 px, blurred wallpaper, a popover with
`border-radius:12px` and a 1 px hairline), then the app's own rows with the
same system colours the app uses (`systemOrange` `#FF9F0A`, `systemRed`
`#FF453A`, `systemGreen` `#30D158`, `systemBlue` `#0A84FF` in dark mode).
Render a real SF Symbol with a five-line Swift script
(`NSImage(systemSymbolName:)` tinted, written as PNG) and embed it base64.

### A VS Code window

Dark Modern: `#1F1F1F` editor, `#181818` side bar, `#2B2B2B` borders,
`#CCCCCC` text, `#0078D4` accents, `#CCA700` warning text. Activity bar 48 px,
side bar 320 to 360 px, tabs 35 px, status bar 22 px. Codicons as inline
SVG (`pulse`, `bell`, `check`, `warning`, `folder`). Editor lines in
`white-space:pre` blocks, not flex, or spaces collapse.

### A Stream Deck

Keys are 144 px canvases. Render them with the plugin's own key renderer
(the SVG the plugin sends to the device), never redrawn by hand, laid out
on a deck mockup: dark rounded slab, 3x2 or 5x3 grid, 26 px gaps.

## Capture script skeleton

```sh
#!/bin/sh
set -e
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
node scripts/showcase.mjs
M=docs/media; F=$M/frames; GROUND="#0d1117"
shot() { "$CHROME" --headless=new --hide-scrollbars --disable-gpu --force-device-scale-factor=2 --window-size="$3" --screenshot="$PWD/$2" "file://$PWD/$1" >/dev/null 2>&1; }
trim() { magick "$1" -fuzz 1% -trim +repage -bordercolor "$GROUND" -border 28 "$1"; }
for scene in $(node -e 'console.log(Object.keys(require("./docs/media/frames/scenes.json")).join(" "))'); do
  n=$(node -e "console.log(require('./docs/media/frames/scenes.json')['$scene'])")
  i=0; files=""
  while [ $i -lt $n ]; do shot $F/$scene-$i.html $F/$scene-$i.png 960,1000; trim $F/$scene-$i.png; files="$files $F/$scene-$i.png"; i=$((i+1)); done
  w=0; h=0
  for f in $files; do set -- $(magick identify -format "%w %h" "$f"); [ "$1" -gt "$w" ] && w=$1; [ "$2" -gt "$h" ] && h=$2; done
  for f in $files; do magick "$f" -background "$GROUND" -gravity north -extent "${w}x${h}" "$f"; done
  last=$F/$scene-$((n-1)).png
  magick -delay 120 -loop 0 $files -delay 300 $last -layers Optimize $M/$scene.gif
  cp $last $M/$scene.png
  if command -v ffmpeg >/dev/null; then
    list=$F/$scene.txt; : > $list
    for f in $files; do printf "file '%s'\nduration 1.2\n" "$PWD/$f" >> $list; done
    printf "file '%s'\nduration 3\nfile '%s'\n" "$PWD/$last" "$PWD/$last" >> $list
    ffmpeg -y -loglevel error -f concat -safe 0 -i $list -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p" -r 30 $M/$scene.mp4
  fi
done
rm -rf $F
```

The mp4 is the gif's frames at the gif's pace (1.2 s a frame, 3 s on the last),
even dimensions for H.264, playable in a store listing, a post, or a slide.
A 30-second product video is the scenes joined in README order with
`ffmpeg -f concat` on the per-scene mp4s, no narration, no music.
A clip that explains a feature belongs in its spec too: one line,
`![what it shows](media/<scene>.mp4)`, under the heading it explains (a path
next to the spec, inside the repository). `corgi idea share <spec.md>` takes it
along, and it plays in the browser, VS Code, the Mac and the phone.

## Procedure

1. **Find the look.** The logo file and the palette, as above; write the
   `brand` block first so every scene is drawn in it.
2. **Read the output code.** For each command or screen in the story, find
   the print statements and copy the format strings, glyphs and colours into
   a scratch list. For a page or an app, copy its palette and the row layout.
3. **Pick the scenes.** The hero (the one command that shows why the product
   exists), then one per feature the README names. Skip anything the README
   does not talk about.
4. **Write `scripts/showcase.mjs`** with the templates above: a scene is a
   caption, an array of lines and a list of cuts (`grow`), or a list of
   states for a UI. The first cut is the command alone; the last frame
   carries one `{H}` line.
5. **Write `scripts/capture-showcase.sh`**, add `make showcase` (or an npm
   script), and note it in the README's development section.
6. **Render, then look.** Open the first and the last frame of two scenes as
   images. Check: the strip and logo on both, the highlighted line, cut-off
   lines, wrapped words, wrong glyphs, a QR with gaps, a frame that changes
   size. Fix and re-render only that scene.
7. **Place in the README**: the hero right under the tagline, full width,
   stacked one under another (side-by-side halves are too small to read).
   Each other gif inside the section it illustrates, with its caption as a
   `<sub>` line under it and `alt` text that says what happens.
8. **Commit media with the script.** The frames directory stays out of git.
9. **Exclude from analysis.** Add `scripts/**` to SonarCloud exclusions and
   the media to the package's ignore file (`.vscodeignore`, `.npmignore`)
   so a store package does not carry gifs; marketplaces rewrite relative
   README image links to the repo's raw URLs.

## Sizes that read well

| what | canvas (1x) | README width |
|---|---|---|
| terminal | 900 px wide, 12 to 26 lines | 760 |
| terminal + phone | 1290 wide | 900 |
| phone alone | 300 x 620 | 340 |
| menu bar dropdown | 340 wide | 400 |
| VS Code window | 1600 x 1000 | 800 |
| menu bar item strip | 600 x 300 shrunk 50% | 300 |

## Done when

- Every frame carries the logo and the caption strip, on the brand ground.
- The first frame of a scene is the command alone; the last has one
  highlighted line and lingers.
- Every line in every frame is something the code prints, or a real UI state.
- One command redraws everything; the README says which.
- The hero gif is the first thing under the tagline; nothing side by side.
- Frames of a gif share one size; the last frame lingers.
- No real client names, paths, tokens or URLs anywhere in the media.
- The phone's tab bar uses the app's own labels and symbols, drawn as SVG,
  with a fade under it; the status bar, island and home indicator are on
  every phone frame, lock screen included.
- A long notification wraps onto a second line inside the terminal instead
  of running off the right edge (check the still, not just the gif).
