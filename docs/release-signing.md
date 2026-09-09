# Signing and notarizing the macOS builds

Why: macOS ties permission grants (Documents, Desktop, Downloads, the
Accessibility list) to the *signing identity* of a binary. An unsigned or
ad-hoc-signed corgi is a new identity on every release, so the "corgi wants
to access your Documents folder" dialog comes back after each `corgi upd`,
and the daemon sits blocked until someone clicks Allow. Signed with one
Developer ID, the grant survives updates. Notarization is the second half:
Gatekeeper lets a notarized binary run without the "unidentified developer"
warning.

Both happen in CI. GoReleaser signs and notarizes the darwin binaries from
the Linux runner (`notarize.macos` in `.goreleaser.yaml`); corgi-bar's
workflow signs, notarizes and staples the app bundle on a macOS runner.
Nothing is signed locally, and no key lives in a repo — only in GitHub
Actions secrets. When the secrets are missing the release is simply
unsigned, as before.

## One-time setup (Apple Developer Program membership required)

### 1. Developer ID Application certificate → `MACOS_SIGN_P12`, `MACOS_SIGN_PASSWORD`

1. Keychain Access → Certificate Assistant → *Request a Certificate From a
   Certificate Authority…* → your email, "Saved to disk". This makes a
   `.certSigningRequest`.
2. https://developer.apple.com/account/resources/certificates/add → type
   **Developer ID Application** → upload the request → download the `.cer`
   → double-click to import into the login keychain.
3. Keychain Access → My Certificates → right-click *Developer ID
   Application: Andrii Klymiuk (TEAMID)* → Export → `.p12`, choose a
   password.
4. Into GitHub, for **each** repo that signs (corgi, corgi-bar):

   ```bash
   gh secret set MACOS_SIGN_P12 --repo Andriiklymiuk/corgi < <(base64 -i developer-id.p12)
   gh secret set MACOS_SIGN_PASSWORD --repo Andriiklymiuk/corgi
   ```

### 2. App Store Connect API key → `MACOS_NOTARY_ISSUER_ID`, `MACOS_NOTARY_KEY_ID`, `MACOS_NOTARY_KEY`

1. https://appstoreconnect.apple.com/access/integrations/api → *Team Keys*
   → **Generate API Key**, name `notary`, access **Developer**.
2. Note the **Issuer ID** (top of the page) and the key's **Key ID**;
   download the `.p8` (only offered once).
3. Secrets:

   ```bash
   gh secret set MACOS_NOTARY_ISSUER_ID --repo Andriiklymiuk/corgi
   gh secret set MACOS_NOTARY_KEY_ID --repo Andriiklymiuk/corgi
   gh secret set MACOS_NOTARY_KEY --repo Andriiklymiuk/corgi < AuthKey_XXXX.p8   # the .p8 as is; the workflow base64-encodes it for GoReleaser
   ```

   corgi-bar takes the same five, plus `APPLE_TEAM_ID` (the ten characters in
   the certificate name).

### 3. Release once and check

```bash
corgi upd
codesign -dv --verbose=2 "$(which corgi)" 2>&1 | grep -E "Authority|TeamIdentifier"
spctl -a -vv -t install "$(which corgi)"     # "accepted … source=Notarized Developer ID"
corgi agent doctor | grep "macOS file access"
```

After the first signed release macOS asks for Documents access one more
time (the identity changed once, from ad-hoc to yours). From then on it
keeps the answer across updates.

## Rotation

Developer ID certificates last five years; the API key does not expire but
can be revoked in App Store Connect. Rotating either is the same
`gh secret set` again.
