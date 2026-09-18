package cmd

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

const (
	accessTokenPrefix  = "corgi_oat_"
	refreshTokenPrefix = "corgi_ort_"
	accessTokenTTL     = time.Hour
	refreshTokenTTL    = 30 * 24 * time.Hour
	rotatedHashKeep    = 24 * time.Hour
	authCodeTTL        = 60 * time.Second
)

type authCode struct {
	clientID      string
	clientName    string
	redirectURI   string
	codeChallenge string
	expires       time.Time
}

func (oa *oauthServer) issueCodeLocked(client oauthClient, redirectURI, challenge string) (string, error) {
	code, err := randomToken("")
	if err != nil {
		return "", err
	}
	if oa.codes == nil {
		oa.codes = map[string]authCode{}
	}
	oa.codes[pairing.HashToken(code)] = authCode{
		clientID:      client.ID,
		clientName:    client.Name,
		redirectURI:   redirectURI,
		codeChallenge: challenge,
		expires:       oa.now().Add(authCodeTTL),
	}
	return code, nil
}

func (oa *oauthServer) redeemCodeLocked(code, clientID, redirectURI, verifier string) (authCode, bool) {
	hash := pairing.HashToken(code)
	c, ok := oa.codes[hash]
	delete(oa.codes, hash)
	if !ok || !oa.now().Before(c.expires) {
		return authCode{}, false
	}
	if c.clientID != clientID || !sameRedirectURI(c.redirectURI, redirectURI) {
		return authCode{}, false
	}
	if !pkceMatches(verifier, c.codeChallenge) {
		return authCode{}, false
	}
	return c, true
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(challenge)) == 1
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func (oa *oauthServer) grantLocked(clientID, clientName string) (tokenResponse, error) {
	id, err := randomToken("")
	if err != nil {
		return tokenResponse{}, err
	}
	now := oa.now()
	fam := tokenFamily{
		ID: id[:16], ClientID: clientID, ClientName: clientName,
		CreatedAt: now, ExpiresAt: now.Add(refreshTokenTTL),
	}
	oa.state.Families = append(oa.state.Families, fam)
	return oa.rotateLocked(len(oa.state.Families) - 1)
}

func (oa *oauthServer) rotateLocked(i int) (tokenResponse, error) {
	fam := &oa.state.Families[i]
	access, err := randomToken(accessTokenPrefix)
	if err != nil {
		return tokenResponse{}, err
	}
	refresh, err := randomToken(refreshTokenPrefix)
	if err != nil {
		return tokenResponse{}, err
	}
	now := oa.now()
	if fam.RefreshHash != "" {
		fam.RotatedHashes = append(fam.RotatedHashes, rotatedHash{Hash: fam.RefreshHash, At: now})
	}
	fam.RefreshHash = pairing.HashToken(refresh)
	oldAccess := fam.AccessHashes
	fam.AccessHashes = []string{pairing.HashToken(access)}

	store, err := pairing.Load(oa.deviceStore)
	if err != nil {
		return tokenResponse{}, err
	}
	store.Devices = dropDevicesByHash(store.Devices, oldAccess)
	store.Devices = append(store.Devices, pairing.Device{
		Name:      fam.ClientName + " · oauth " + fam.ID[:4],
		TokenHash: fam.AccessHashes[0],
		CreatedAt: now.UTC(),
		ExpiresAt: now.Add(accessTokenTTL).UTC(),
		Family:    fam.ID,
	})
	if err := pairing.Save(oa.deviceStore, store); err != nil {
		return tokenResponse{}, err
	}
	if err := oa.saveLocked(); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(accessTokenTTL.Seconds()), RefreshToken: refresh}, nil
}

func dropDevicesByHash(devices []pairing.Device, hashes []string) []pairing.Device {
	if len(hashes) == 0 {
		return devices
	}
	drop := map[string]bool{}
	for _, h := range hashes {
		drop[h] = true
	}
	kept := devices[:0]
	for _, d := range devices {
		if !drop[d.TokenHash] {
			kept = append(kept, d)
		}
	}
	return kept
}

func (oa *oauthServer) familyIndexLocked(refreshHash string) (i int, rotated bool) {
	for i, f := range oa.state.Families {
		if subtle.ConstantTimeCompare([]byte(f.RefreshHash), []byte(refreshHash)) == 1 {
			return i, false
		}
		for _, r := range f.RotatedHashes {
			if r.Hash == refreshHash {
				return i, true
			}
		}
	}
	return -1, false
}

func (oa *oauthServer) revokeFamilyLocked(id string) bool {
	for i, f := range oa.state.Families {
		if f.ID != id {
			continue
		}
		oa.state.Families = append(oa.state.Families[:i], oa.state.Families[i+1:]...)
		if store, err := pairing.Load(oa.deviceStore); err == nil {
			store.Devices = dropDevicesByFamily(store.Devices, id)
			_ = pairing.Save(oa.deviceStore, store)
		}
		return true
	}
	return false
}

func dropDevicesByFamily(devices []pairing.Device, family string) []pairing.Device {
	kept := devices[:0]
	for _, d := range devices {
		if d.Family != family {
			kept = append(kept, d)
		}
	}
	return kept
}

func revokeOAuthFamily(agentDir, family string) {
	if family == "" {
		return
	}
	path := oauthStatePath(agentDir)
	st, err := loadOAuthState(path)
	if err != nil {
		return
	}
	for i, f := range st.Families {
		if f.ID == family {
			st.Families = append(st.Families[:i], st.Families[i+1:]...)
			_ = saveOAuthState(path, st)
			return
		}
	}
}

func (oa *oauthServer) sweepLocked() {
	now := oa.now()
	for h, c := range oa.codes {
		if !now.Before(c.expires) {
			delete(oa.codes, h)
		}
	}
	for id, p := range oa.pending {
		if !now.Before(p.expires) {
			delete(oa.pending, id)
		}
	}
	changed := false
	kept := oa.state.Families[:0]
	for _, f := range oa.state.Families {
		if !now.Before(f.ExpiresAt) {
			changed = true
			continue
		}
		rotated := f.RotatedHashes[:0]
		for _, r := range f.RotatedHashes {
			if now.Sub(r.At) < rotatedHashKeep {
				rotated = append(rotated, r)
			} else {
				changed = true
			}
		}
		f.RotatedHashes = rotated
		kept = append(kept, f)
	}
	oa.state.Families = kept
	if changed {
		_ = oa.saveLocked()
	}
	if store, err := pairing.Load(oa.deviceStore); err == nil && store.RevokeExpired(now) > 0 {
		_ = pairing.Save(oa.deviceStore, store)
	}
}

func (oa *oauthServer) authorizeAccessToken(header string) bool {
	offered, ok := strings.CutPrefix(header, bearerPrefix)
	if !ok || !strings.HasPrefix(offered, accessTokenPrefix) {
		return false
	}
	store, err := pairing.Load(oa.deviceStore)
	if err != nil {
		return false
	}
	d, ok := store.AuthorizeDevice(offered)
	if !ok || d.Family == "" {
		return false
	}
	oa.mu.Lock()
	defer oa.mu.Unlock()
	for _, f := range oa.state.Families {
		if f.ID == d.Family && oa.now().Before(f.ExpiresAt) {
			return true
		}
	}
	return false
}

func (oa *oauthServer) tokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if ct := r.Header.Get(headerContentType); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the token endpoint wants application/x-www-form-urlencoded, got "+strings.TrimSpace(ct))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthBody)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "body is not a valid form")
		return
	}
	oa.mu.Lock()
	defer oa.mu.Unlock()
	if oa.limitedLocked(w) {
		return
	}
	oa.sweepLocked()

	var resp tokenResponse
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		code, ok := oa.redeemCodeLocked(r.PostForm.Get("code"), r.PostForm.Get("client_id"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
		if !ok {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is unknown, expired, already used, or the verifier does not match")
			return
		}
		resp, err = oa.grantLocked(code.clientID, code.clientName)
	case "refresh_token":
		hash := pairing.HashToken(r.PostForm.Get("refresh_token"))
		i, rotated := oa.familyIndexLocked(hash)
		if i < 0 || r.PostForm.Get("refresh_token") == "" {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown or expired")
			return
		}
		fam := oa.state.Families[i]
		if rotated {
			oa.revokeFamilyLocked(fam.ID)
			_ = oa.saveLocked()
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token was already used; the grant is revoked, sign in again")
			return
		}
		if cid := r.PostForm.Get("client_id"); cid != "" && cid != fam.ClientID {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token belongs to another client")
			return
		}
		resp, err = oa.rotateLocked(i)
	case "":
		oauthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
		return
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "corgi supports authorization_code and refresh_token")
		return
	}
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue tokens: "+err.Error())
		return
	}
	w.Header().Set(headerContentType, mimeJSON)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	body, _ := json.Marshal(resp)
	_, _ = w.Write(body)
}

func (oa *oauthServer) revokeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthBody)
	_ = r.ParseForm()
	token := r.PostForm.Get("token")
	oa.mu.Lock()
	defer oa.mu.Unlock()
	oa.sweepLocked()
	if token != "" {
		hash := pairing.HashToken(token)
		if i, _ := oa.familyIndexLocked(hash); i >= 0 {
			oa.revokeFamilyLocked(oa.state.Families[i].ID)
			_ = oa.saveLocked()
		} else {
			for _, f := range oa.state.Families {
				for _, a := range f.AccessHashes {
					if a == hash {
						oa.revokeFamilyLocked(f.ID)
						_ = oa.saveLocked()
						break
					}
				}
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}
