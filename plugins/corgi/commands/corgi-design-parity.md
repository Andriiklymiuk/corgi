---
description: Prove a screen matches its design — pull the design frames into the repo, make a flagged/unseeded feature reachable, capture the same states, compose labelled design-vs-app side-by-sides, read them, and report the deviations (fixed vs deliberate). Also covers shipping the copy from the design through the translation platform without wiping live translations.
---

Run the **design-parity** pass for `$ARGUMENTS`.

- `$ARGUMENTS` = the design link / ticket and the screens in scope. Nothing = the design
  linked on the current ticket, for the change just made.

Per `plugins/corgi/skills/design-parity/SKILL.md`:

1. **Pull the design** — file + node id from the URL, parse metadata for frame names/ids
   only (never read the whole dump), export the frames in scope into
   `docs/design/<ticket>/`, and read them before touching code. Crop + upscale the small
   parts (badges, chips, icon rows).
2. **Make it reachable** — seed real data if you can; otherwise ONE clearly-marked local
   switch that forces the feature on with faked values, flipped to reach every state, and
   **reverted + grepped before commit**.
3. **Capture the same states** the design draws (before / done / late / locked / empty),
   one device + one locale, full-res to files under `docs/design/<ticket>/app/`.
4. **Compose + READ** labelled `DESIGN | APP` images into `docs/design/<ticket>/compare/`,
   normalized by height, zoomed 2–3× for small components.
5. **Report the deviation table** — what the design shows, what the app shows, fixed or
   deliberate. Platform-native divergences are fine but must be named in the PR/MR; re-shoot
   after fixing.
6. **Copy counts as design** — translate before pushing to the translation platform (a push
   usually overwrites), push only the new keys, spell out long CLI flags (a short one may be
   a destructive cleanup), then verify the keys per locale from the platform API.
