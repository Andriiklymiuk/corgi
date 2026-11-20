package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

const inlineScriptSource = "script-src 'unsafe-inline'"

var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

func scriptHashes(pages ...string) []string {
	seen := map[string]bool{}
	var hashes []string
	for _, page := range pages {
		for _, m := range inlineScript.FindAllStringSubmatch(page, -1) {
			sum := sha256.Sum256([]byte(m[1]))
			hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
			if !seen[hash] {
				seen[hash] = true
				hashes = append(hashes, hash)
			}
		}
	}
	return hashes
}

func hashedScriptPolicy(policy string, pages ...string) string {
	hashes := scriptHashes(pages...)
	if len(hashes) == 0 {
		return policy
	}
	return strings.Replace(policy, inlineScriptSource, "script-src "+strings.Join(hashes, " "), 1)
}

var boardPagePolicy = sync.OnceValue(func() string { return hashedScriptPolicy(boardPageCSP, boardPageHTML) })

var ideaPagePolicy = sync.OnceValue(func() string { return hashedScriptPolicy(ideaPageCSP, ideaPageHTML, ideaIndexHTML) })

func setSharedPageHeaders(w http.ResponseWriter, policy string) {
	w.Header().Set(headerContentType, htmlContentType)
	w.Header().Set(headerContentSecurityPolicy, policy)
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
}
