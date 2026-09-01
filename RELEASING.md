# Releasing corgi

Releases are cut by pushing to `main`: `.github/workflows/release.yml` tags the
version in `cmd/root.go` and runs GoReleaser.

## Signing the macOS binaries (optional, one-time)

macOS files a privacy answer ("allow corgi to read Documents") against the exact
binary that asked. corgi ships ad-hoc signed, so every release is a new identity
and the dialog comes back for anyone with a workspace in `~/Documents`,
`~/Desktop`, `~/Downloads` or iCloud Drive.

A Developer ID signature fixes that for every user at once. It costs the
maintainer an Apple Developer Program membership ($99/yr) and nothing at all to
anyone installing corgi. Skipping it is fine — the `notarize` block in
`.goreleaser.yaml` is inert until the secrets below exist, and releases without
them ship unsigned exactly as they do now.

**1. Developer ID Application certificate**

Keychain Access → Certificate Assistant → *Request a Certificate From a
Certificate Authority* → "Saved to disk". Upload the `.certSigningRequest` at
<https://developer.apple.com/account/resources/certificates> → **+** →
**Developer ID Application**. Download the `.cer`, double-click to install, then
right-click it in Keychain Access → **Export** as `.p12` with a password.

```bash
base64 -i corgi.p12 | pbcopy      # → MACOS_SIGN_P12
                                   # the .p12 password → MACOS_SIGN_PASSWORD
```

**2. App Store Connect API key** (notarization)

<https://appstoreconnect.apple.com/access/integrations/api> → **Generate API
Key**, role **Developer**. The `.p8` downloads once.

```bash
base64 -i AuthKey_XXXXXXXXXX.p8 | pbcopy   # → MACOS_NOTARY_KEY
                                            # Key ID    → MACOS_NOTARY_KEY_ID
                                            # Issuer ID → MACOS_NOTARY_ISSUER_ID
```

**3. Add the five secrets** at *Settings → Secrets and variables → Actions*.
The next release signs and notarizes itself, from the same Linux runner.

Nothing secret lives in the repo — `.goreleaser.yaml` only names the variables.
Forked PRs cannot read repo secrets, and the release workflow runs on pushes to
`main` only.

**Verify a signed build:**

```bash
codesign -dv "$(readlink -f "$(which corgi)")" 2>&1 | grep TeamIdentifier
# TeamIdentifier=ABCDE12345  → privacy grants now survive `corgi upd`
```
