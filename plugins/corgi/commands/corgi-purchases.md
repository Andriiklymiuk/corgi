---
description: Manage In-App Purchases / store metadata / RevenueCat for an Expo / React Native app — the product-id source of truth, a version-controlled local source for per-IAP App Review notes (примечание) generated from the catalog with a drift-guard test, and pushing IAP `reviewNote` via the App Store Connect API (fastlane `deliver` can't manage IAP metadata). Dry-run before apply; the review screenshot stays manual.
---

Run the **purchases** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = what to do — `stage` (regenerate the local IAP notes), `push` (dry-run the
  ASC update), `push --apply` (PATCH live), or a plain-words ask ("add a premium pack",
  "sync product ids"). Pushing mutates live store config — dry-run + confirm first.

Per `plugins/corgi/skills/purchases/SKILL.md`:

1. **Source of truth.** Product ids live in ONE app catalog module (`<category>_<id>`);
   App Store Connect, Play, and RevenueCat mirror them exactly. Derive metadata FROM it.
2. **Stage notes.** Generate one templated App Review note per product into a version-tracked
   folder (`fastlane/metadata/iap/<product_id>/review_notes.txt`); a drift-guard test pins the
   set to the real catalog.
3. **Push (deliver can't).** ASC API: mint an ES256 JWT from the team key (keychain helper),
   `GET …/inAppPurchasesV2` to map by `productId`, `PATCH /v2/inAppPurchases/<id>` with
   `attributes.reviewNote`. **Dry-run by default**; `--apply` writes; per-product try/catch.
4. **Verify.** Re-run the dry-run → "0 to update, N already current". Attach the review
   screenshot by hand in the console (only the text is managed here).
5. **RevenueCat** for offerings/entitlements (MCP / dashboard) — NOT the ASC review notes.

Report: products staged, what would/did change on ASC (and any not found), and the manual
screenshot step still outstanding.
