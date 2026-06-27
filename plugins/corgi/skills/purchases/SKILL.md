---
name: purchases
description: Use when managing In-App Purchases / store metadata / RevenueCat for an Expo / React Native app — "set up the IAPs", "push the IAP review notes", "update store metadata for purchases", "the App Review notes for each pack", "configure RevenueCat products", "add a premium pack / deck / unlock", "manage non-consumables", "sync product ids across stores". Covers the product-id source-of-truth, a version-controlled local source for per-IAP App Review notes (примечание) generated from the catalog with a drift-guard test, and pushing IAP `reviewNote` via the App Store Connect API — because fastlane `deliver` does NOT manage IAP metadata. NOT for the binary submit (ship skill) or device verification (mobile skill).
---

# Manage IAPs + store purchase metadata

## Overview
Two surfaces, often confused: the **binary** (ship skill) and the **products** (here).
Load-bearing fact: **fastlane `deliver` does NOT manage IAP metadata** — it pushes the app
listing + screenshots, not per-IAP fields. IAP review notes / names / prices → the **App
Store Connect API** (or by hand in the console). RevenueCat owns offerings + entitlements,
NOT the store-side review notes.

## The product-id source of truth
ONE app module owns the exact store product-id strings (`<category>_<id>`, e.g.
`pack_<id>` / `deck_<id>` / `<feature>_unlock`). These strings ARE the contract: App Store
Connect, Play Console, and RevenueCat must all mirror them, and ownership is checked
against `customerInfo.allPurchasedProductIdentifiers`. Renaming one = renaming it in three
places. Derive everything else (the store grid, the metadata below) FROM this module so
they can't drift.

## Version-control the IAP review notes (примечание)
The per-IAP "how a reviewer tests this purchase" steps used to live only in App Store
Connect (and often only for ONE product), so they were easy to lose + drift. Make them a
code artifact:
- **Source:** a small RN-free module that, for every product in the catalog, emits a
  templated review note (steps to find + buy it, platforms, which product is under review),
  keyed by category. RN-free so a plain `bun`/`node` script can run it.
- **Stage:** a generator writes one `review_notes.txt` per product under a version-tracked
  folder (e.g. `fastlane/metadata/iap/<product_id>/review_notes.txt`) + an INDEX. One
  `bun run iap:stage` regenerates them ALL together — adding a pack = one edit, every note
  stays in lockstep.
- **Drift-guard test:** a unit test pins the generated product set to the app's REAL catalog
  (import the catalog + the free/excluded set, compare) so a new/removed product can't
  silently fall out of the notes.

## Push to App Store Connect (deliver can't)
A small script talks to the ASC API directly:
1. **Auth** — mint an ES256 JWT from the team API key. `aud: appstoreconnect-v1`,
   `alg: ES256`, 10-min exp; sign with the EC P-256 key using a JOSE raw signature
   (Node: `crypto.sign('SHA256', input, { key, dsaEncoding: 'ieee-p1363' })`). Get the key
   from the repo's keychain helper (it injects `*_KEY_ID` / `*_ISSUER_ID` /
   `*_KEY_CONTENT` (base64 .p8) for the wrapped command) — never read a .p8 off disk.
2. **Map** — `GET /v1/apps?filter[bundleId]=<bundle>` → app id; `GET
   /v1/apps/<id>/inAppPurchasesV2?limit=200&fields[inAppPurchases]=productId,name,state,reviewNote`
   → match each by `productId`.
3. **PATCH** — `reviewNote` IS an attribute on the InAppPurchaseV2 resource:
   `PATCH /v2/inAppPurchases/<iap-id>` with
   `{ data: { type: "inAppPurchases", id, attributes: { reviewNote } } }`.
4. **Dry-run by DEFAULT** (it mutates live store config) — print which IAPs would change +
   skip ones already current / not in ASC; `--apply` sends the PATCHes. Make the apply loop
   per-product try/catch so one bad IAP can't abort the batch.
5. **Round-trip verify** — re-run the dry-run; "0 to update, N already current" proves it
   persisted. The required review **screenshot** is still attached by hand in the console —
   only the TEXT is managed here.

## Gotchas
- **`deliver` ignores IAPs.** `precheck_include_in_app_purchases` is a PRECHECK flag, not a
  pusher. Don't expect `fastlane deliver` to touch a single IAP field.
- **Apple's first-IAP review gate.** The FIRST in-app purchase rides a full app-VERSION
  review. Ship with ONE live product, pass review, then widen the live set + submit the rest
  — no new binary needed for the later IAPs. Code this as a staged-rollout flag in the
  catalog, not a scramble at submit time.
- **Per-platform enablement.** A product hidden until its store/RevenueCat config exists
  avoids unbuyable store cards AND RevenueCat erroring on a product that doesn't exist on
  that platform. Gate premium sections per-platform in the catalog.
- **RevenueCat ≠ ASC notes.** Use the RevenueCat MCP / dashboard for offerings, entitlements,
  packages, and a comp/all-access entitlement — but the App Review NOTE is an ASC-side
  field; push it with the API above, not RevenueCat.
- **State doesn't block a note edit.** PATCHing `reviewNote` works on `READY_TO_SUBMIT` AND
  `APPROVED` IAPs (metadata edit). The dry-run shows each one's state so you know what you're
  touching.

## Red flags — stop
- Expecting `fastlane deliver` / `supply` to push IAP review notes → it can't; use the ASC
  API.
- Review notes living only in App Store Connect → un-versioned, drift-prone; generate them
  from the catalog into the repo with a drift-guard test.
- Hand-listing the products in the generator → it drifts from the app; derive from the
  product-id source of truth + pin it with a test.
- `--apply` on the first run → dry-run first (it mutates live store config), eyeball the
  diff, THEN apply + round-trip verify.
- Mismatched product-id strings across app / stores / RevenueCat → ownership checks fail
  silently; the catalog module is the single contract.

## See also
- **`ship` skill** — submitting the binary (separate from the products here).
- **`mobile` skill** — driving the actual purchase sheet on a device to verify the flow.
- RevenueCat MCP / dashboard — offerings + entitlements (not the ASC review notes).
