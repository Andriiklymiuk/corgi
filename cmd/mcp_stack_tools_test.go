package cmd

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

// corgi_http hits a service by name on the port the compose file declares,
// sends JSON as JSON, and caps the body.
func TestCorgiHTTPTalksToAServiceByName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "abc")
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "application/json" && r.URL.Path == "/limits" {
			w.WriteHeader(201)
			w.Write([]byte(strings.Repeat("x", httpBodyMax+10)))
			return
		}
		w.Write([]byte("ok " + r.URL.RawQuery))
	}))
	defer srv.Close()
	_, portStr, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"), "")
	port, _ := strconv.Atoi(portStr)

	orig := loadComposeForMCP
	defer func() { loadComposeForMCP = orig }()
	loadComposeForMCP = func(string) (*utils.CorgiCompose, error) {
		return &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api", Port: port}}}, nil
	}

	out, err := mcpHTTP(httpArgs{Service: "api", Path: "health?x=1"})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(map[string]any)
	if got["status"] != 200 || got["body"] != "ok x=1" || got["headers"].(map[string]string)["X-Request-Id"] != "abc" {
		t.Fatalf("get: %+v", got)
	}
	out, err = mcpHTTP(httpArgs{Service: "api", Path: "/limits", Method: "post", Body: `{"user":1}`})
	if err != nil {
		t.Fatal(err)
	}
	got = out.(map[string]any)
	if got["status"] != 201 || got["truncated"] != true || len(got["body"].(string)) != httpBodyMax {
		t.Fatalf("post: status %v truncated %v len %d", got["status"], got["truncated"], len(got["body"].(string)))
	}
	if _, err := mcpHTTP(httpArgs{Service: "nope", Path: "/"}); err == nil || !strings.Contains(err.Error(), "no service") {
		t.Fatal("an unknown service is named")
	}
}

func TestExplainHintReadsThePlan(t *testing.T) {
	if h := explainHint("Seq Scan on users  (cost=0.00..35.50 rows=2550 width=4)"); !strings.Contains(h, "sequential scan") {
		t.Fatalf("hint: %s", h)
	}
	if h := explainHint("Index Scan using users_pkey on users"); h != "" {
		t.Fatalf("an index scan needs no hint: %s", h)
	}
}
