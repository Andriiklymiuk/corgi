# Framework- and app-specific traps

Read when the screen under test uses one of these. Each surfaced once in a real run; the general drive loop and gotchas are in `../SKILL.md`.

## SceneKit / Metal
- **SceneKit / Metal shader-modifier failures render magenta at runtime, not xcodebuild.** A
  clean prod build builds + ships a magenta board uncaught. Verify a native shader on a
  sim/device before the store submit. Classic trigger: `#pragma arguments float3` + a KVC
  uniform binding - hardcode colour literals instead.
- **Programmatic `SCNParticleSystem` with no `particleImage` draws hard squares.** Set a
  soft radial (white→transparent) puff texture → smoke/fire/splash read as round puffs.

## SwiftUI / RNGH / native views in sheets
- **Maestro can't flip a SwiftUI / `@expo/ui` Toggle by tapping its label** - label Text +
  switch are separate elements. Tap the switch control (`point` on the row's right edge);
  gate it with the `checked` selector (`when: notVisible: { id, checked: true }`) so it
  flips only when off.
- **A gesture-handler `Pressable` as the sized flex cell stretches its child - circle →
  square.** RNGH `Pressable` doesn't hold a fixed pixel width the way a plain `View` does,
  and an inline / dynamic width style (worse with React Compiler on) lets the child disc
  grow to fill the cell → a "circle" renders as a rounded square - and often only after a
  re-render (a freshly-toggled day) while the first-paint ones still look right. Fix: size
  the cell with a plain `View` / static `StyleSheet` entry, keep the shape a fixed, centred
  child, and mirror the screen's already-working sibling cell (e.g. the month-view DayCell)
  instead of re-deriving sizes inline. `onLayout` on an RNGH Pressable is flaky too - put
  it on a plain wrapper.
- **Absolute-fill background behind a separately-centred label clips / offsets on iOS.** A
  disc drawn as a `position:absolute` layer behind a sibling number can sit off-centre or
  get clipped at the top on iOS (fine on Android). Fix: make it one in-flow element - a
  fixed circle with the label inside it - so the cell centres the whole unit. (Keep an
  absolute layer only for a shape that must bleed past the cell, like a joined period
  pill.)
- **A native 3D / Skia view (SceneKit, react-native-skia, a Metal/GL surface) inside a
  react-navigation `formSheet` (`sheetAllowedDetents: "fitToContents"`) breaks RN layout for
  its siblings.** A sibling box (a toggle row, a label) overlaps the native view no matter
  the child order, a wrapper `View`, a fixed-height slot, or `position:absolute` - `maestro
  hierarchy` shows the two `bounds` overlapping by tens of px (the sheet's content-fit
  measure + the native view reporting no intrinsic box). Measure with `maestro hierarchy`
  before reshuffling flex for an hour; clean separation may need the native view in its own
  detent-sized container (or dropping the sibling). It renders fine on a plain (non-sheet)
  screen - so it's the sheet, not your styles.
- **Dismiss an iOS `formSheet` in Maestro with a grabber swipe-down, not a backdrop tap.**
  The backdrop tap is racy (works once, misses the next - leaving the sheet open so the next
  step fails); `swipe: { start: "50%,38%", end: "50%,97%", duration: 600 }` is reliable. And
  in a capture sweep (open → `takeScreenshot` → dismiss → next), the screenshot can race the
  present animation and silently grab the previous screen - the flow logs `COMPLETED` but the
  PNG is the list behind the sheet. Read every captured frame; `COMPLETED` ≠ the right screen.
- **To drive or screenshot a paywall, the product must be unowned** - tapping an owned premium
  item usually equips it (no sheet opens), so a sweep silently captures the store grid instead.
  Reset the purchases SDK's anonymous user to all-unowned by reinstalling the same build
  (`simctl uninstall` + `simctl install <existing .app>` - also re-triggers onboarding, Skip
  it; no rebuild needed). A sandbox/test SDK key (e.g. RevenueCat Test Store `test_…`) returns
  SDK-configured prices, so set those to match prod to capture prod-looking paywalls without
  waiting on store approval.

## Native header search (react-native-screens)
- **Native `headerSearchBarOptions` (react-native-screens) on iOS 26 floats to the bottom by
  default.** The default `placement: "automatic"` drops the search field to the bottom of the
  screen, overlapping content (a UIKit root-screen toolbar-integration bug) → set
  `placement: "stacked"` and it anchors below the title bar as expected (rn-screens forces
  `allowToolbarIntegration:false` for stacked, which dodges the bug). These header/search
  options are JS nav config → they hot-reload on an already-built dev client (no native
  rebuild), so iterate the layout live on the sim. (`headerLargeTitle` can also render blank
  in some expo-router setups - if it does on yours, draw the big title in-content instead of
  fighting it; verify per-app, don't assume.) A documented "native X can't anchor / doesn't
  work here" is often a stale, fixable conclusion - re-test the native option on-device first.
- **"Search visible at rest and tucking on scroll" (Telegram-style) wants a working large
  title; without one, drive it from JS.** `hideWhenScrolling: true` on its own leaves a
  stacked search hidden at rest (pull-to-reveal). To show it at the top and hide it once the
  list scrolls, keep `headerSearchBarOptions` mounted and remove it (set `undefined`) past a
  scroll threshold - with hysteresis whose gap clears the search bar's own height, or the
  layout shift from removing it bounces the offset back over the threshold (flicker loop).
  Gate to iOS - a Material toolbar search icon (Android) is compact and shouldn't hide on
  scroll.
