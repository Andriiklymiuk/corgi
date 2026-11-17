package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var strictToolName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func strictSchemaProblems(node any, path string, top bool) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		for _, k := range []string{"$ref", "anyOf", "oneOf", "allOf", "not", "$defs", "definitions"} {
			if _, has := v[k]; has {
				out = append(out, path+": "+k)
			}
		}
		if _, many := v["type"].([]any); many {
			out = append(out, path+": several types")
		}
		if v["type"] == "array" && v["items"] == nil {
			out = append(out, path+": array without items")
		}
		if v["type"] == "object" && v["properties"] == nil && !top {
			out = append(out, path+": object without properties")
		}
		for k, child := range v {
			out = append(out, strictSchemaProblems(child, path+"/"+k, false)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, strictSchemaProblems(child, path, false)...)
		}
	}
	return out
}

func TestEveryToolFitsTheStrictClients(t *testing.T) {
	s := newCorgiMCPServer("test")
	tools := s.ListTools()
	if len(tools) < 60 || len(tools) > 120 {
		t.Fatalf("%d tools: a client that caps a server's tools needs this under 128", len(tools))
	}
	for name, st := range tools {
		tool := st.Tool
		if !strictToolName.MatchString(name) {
			t.Errorf("%s: a tool name is letters, digits, _ and -, up to 64", name)
		}
		if n := len(tool.Description); n == 0 || n > 1024 {
			t.Errorf("%s: description is %d characters, 1 to 1024 fits every client", name, n)
		}
		if tool.Annotations.Title == "" || tool.Annotations.ReadOnlyHint == nil {
			t.Errorf("%s: needs a title and a read-only hint", name)
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if schema["type"] != "object" {
			t.Errorf("%s: the input schema is an object", name)
		}
		for _, problem := range strictSchemaProblems(schema, "", true) {
			t.Errorf("%s%s", name, problem)
		}
	}
}

func TestSignInMetadataIsWhatChatGPTAndCopilotDiscover(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	oa.setIssuer("https://corgi.example.ngrok.app")
	_, body := getJSON(t, mux, oauthASMetadataPath, nil)
	if body["authorization_response_iss_parameter_supported"] != true {
		t.Fatalf("issuer identification lets a client use one fixed redirect: %v", body)
	}
	if body["registration_endpoint"] != "https://corgi.example.ngrok.app"+oauthRegisterPath {
		t.Fatalf("dynamic registration is what Dynamic discovery looks for: %v", body["registration_endpoint"])
	}
	methods, _ := body["code_challenge_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "S256" {
		t.Fatalf("PKCE: %v", methods)
	}
	for _, uri := range []string{"https://chatgpt.com/connector_platform_oauth_redirect", "https://global.consent.azure-apim.net/redirect/corgi", "https://claude.ai/api/mcp/auth_callback"} {
		if code, _ := oa.registrationRefusal(registerRequest{RedirectURIs: []string{uri}}); code != "" {
			t.Fatalf("%s is refused: %s", uri, code)
		}
	}
}

func TestAClientThatWantsASecretGetsOneAndMustShowIt(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	rec, reg := postJSON(t, mux, oauthRegisterPath, `{"redirect_uris":["https://global.consent.azure-apim.net/redirect/corgi"],"client_name":"Copilot Studio","token_endpoint_auth_method":"client_secret_basic"}`, nil)
	if rec.Code != 201 {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	id, _ := reg["client_id"].(string)
	secret, _ := reg["client_secret"].(string)
	if !strings.HasPrefix(secret, "corgi_secret_") || reg["token_endpoint_auth_method"] != "client_secret_basic" || reg["client_secret_expires_at"] != float64(0) {
		t.Fatalf("registration answer: %v", reg)
	}
	oa.mu.Lock()
	stored, _ := oa.state.findClient(id)
	oa.mu.Unlock()
	if stored.SecretHash == "" || strings.Contains(stored.SecretHash, secret) {
		t.Fatal("only a hash of the secret is kept")
	}
	redirect := "https://global.consent.azure-apim.net/redirect/corgi"
	mint := func() string {
		oa.mu.Lock()
		defer oa.mu.Unlock()
		code, err := oa.issueCodeLocked(oauthClient{ID: id, Name: "Copilot Studio"}, redirect, challengeFor(testVerifier))
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	token := func(code string, basic, formSecret string, withID bool) (*httptest.ResponseRecorder, map[string]any) {
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {testVerifier}, "redirect_uri": {redirect}}
		if withID {
			form.Set("client_id", id)
		}
		if formSecret != "" {
			form.Set("client_secret", formSecret)
		}
		req := httptest.NewRequest(http.MethodPost, oauthTokenPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if basic != "" {
			req.SetBasicAuth(id, basic)
		}
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		var body map[string]any
		_ = json.Unmarshal(out.Body.Bytes(), &body)
		return out, body
	}
	if rec, body := token(mint(), "", "", true); rec.Code != 401 || body["error"] != "invalid_client" {
		t.Fatalf("no secret: %d %v", rec.Code, body)
	}
	if rec, _ := token(mint(), "wrong", "", false); rec.Code != 401 {
		t.Fatalf("wrong secret: %d", rec.Code)
	}
	rec, body := token(mint(), secret, "", false)
	if rec.Code != 200 || body["access_token"] == nil {
		t.Fatalf("secret in the header: %d %v", rec.Code, body)
	}
	if rec, body := token(mint(), "", secret, true); rec.Code != 200 {
		t.Fatalf("secret in the form: %d %v", rec.Code, body)
	}
	refresh, _ := body["refresh_token"].(string)
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}}
	if rec, _ := postForm(t, mux, oauthTokenPath, form); rec.Code != 401 {
		t.Fatalf("a refresh without the secret: %d", rec.Code)
	}
	form.Set("client_secret", secret)
	if rec, out := postForm(t, mux, oauthTokenPath, form); rec.Code != 200 {
		t.Fatalf("a refresh with the secret: %d %v", rec.Code, out)
	}
}

func TestAPublicClientStillNeedsNoSecret(t *testing.T) {
	oa, mux, _ := newTestOAuthServer(t)
	if rec, _ := postForm(t, mux, oauthTokenPath, codeGrant(issueTestCode(t, oa))); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec, body := postJSON(t, mux, oauthRegisterPath, `{"redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"client_name":"ChatGPT"}`, nil)
	if rec.Code != 201 || body["client_secret"] != nil || body["token_endpoint_auth_method"] != "none" {
		t.Fatalf("%d %v", rec.Code, body)
	}
	if rec, _ := postJSON(t, mux, oauthRegisterPath, `{"redirect_uris":["https://chatgpt.com/cb"],"token_endpoint_auth_method":"private_key_jwt"}`, nil); rec.Code != 400 {
		t.Fatalf("an unknown method is refused: %d", rec.Code)
	}
}
