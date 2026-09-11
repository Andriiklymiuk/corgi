package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The running, seeded stack is the one thing a session here has that a
// session anywhere else does not. These tools finish that: hit a service by
// name, ask the database how it would run a query.

const httpBodyMax = 64 << 10

type httpArgs struct {
	ComposePath string
	Service     string
	Method      string
	Path        string
	Body        string
	Headers     map[string]string
}

func addStackTools(s *server.MCPServer, composeOpt, serviceOpt mcp.ToolOption) {
	s.AddTool(mcp.NewTool("corgi_http",
		mcp.WithDescription("Send one HTTP request to a running service of the stack by its name — corgi knows the port — and return {status, headers, body, truncated, ms}. For checking a route the way a client would: GET /health, POST /limits with a JSON body. Body is capped at 64 KB. The service must be up (corgi_up); the request goes to 127.0.0.1. Disabled over a public tunnel unless CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1, like exec and the database tools."),
		composeOpt,
		serviceOpt,
		mcp.WithString("path", mcp.Required(), mcp.Description("Path with query, e.g. /api/limits?user=1")),
		mcp.WithString("method", mcp.Description("GET (default), POST, PUT, PATCH, DELETE")),
		mcp.WithString("body", mcp.Description("Request body; JSON is sent as application/json unless headers say otherwise")),
		mcp.WithObject("headers", mcp.Description("Extra headers, e.g. {\"Authorization\": \"Bearer …\"}")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", dangerousToolBlockedMsg)
		}
		headers := map[string]string{}
		if raw, ok := r.GetArguments()["headers"].(map[string]any); ok {
			for k, v := range raw {
				headers[k] = fmt.Sprint(v)
			}
		}
		return mcpHTTP(httpArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Method:      r.GetString("method", "GET"),
			Path:        r.GetString("path", ""),
			Body:        r.GetString("body", ""),
			Headers:     headers,
		})
	}))

	s.AddTool(mcp.NewTool("corgi_explain",
		mcp.WithDescription("Ask a postgres-family db_service how it would run a query: EXPLAIN (or EXPLAIN ANALYZE when analyze is true — the query then actually runs, so not on a mutating statement) through psql. Returns {service, plan, truncated}. For finding the sequential scan behind a slow endpoint. Disabled over a public tunnel unless CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1."),
		composeOpt,
		mcp.WithString("service", mcp.Required(), mcp.Description("db_service name")),
		mcp.WithString("query", mcp.Required(), mcp.Description("The SQL to explain")),
		mcp.WithBoolean("analyze", mcp.Description("EXPLAIN ANALYZE: run it and report real timings (default false)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", dangerousToolBlockedMsg)
		}
		return mcpExplain(r.GetString("composePath", ""), r.GetString("service", ""), r.GetString("query", ""), r.GetBool("analyze", false))
	}))
}

func mcpHTTP(a httpArgs) (any, error) {
	if strings.TrimSpace(a.Service) == "" || strings.TrimSpace(a.Path) == "" {
		return nil, fmt.Errorf("%s: service and path are required", utils.ErrUsage)
	}
	corgi, err := loadComposeForMCP(a.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
	port, _, ok := serviceTunnelInfo(corgi, a.Service)
	if !ok {
		return nil, fmt.Errorf("%s: no service called %q in this stack", utils.ErrServiceNotFound, a.Service)
	}
	if port == 0 {
		return nil, fmt.Errorf("%s: service %q declares no port", utils.ErrUsage, a.Service)
	}
	path := a.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	method := strings.ToUpper(strings.TrimSpace(a.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), body)
	if err != nil {
		return nil, err
	}
	if a.Body != "" && a.Headers["Content-Type"] == "" && looksLikeJSON(a.Body) {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %s did not answer on :%d — is it up? (%v)", utils.ErrServiceNotFound, a.Service, port, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, httpBodyMax+1))
	truncated := len(raw) > httpBodyMax
	if truncated {
		raw = raw[:httpBodyMax]
	}
	headers := map[string]string{}
	for _, k := range []string{"Content-Type", "Content-Length", "Location", "Retry-After", "X-Request-Id", "Cache-Control"} {
		if v := resp.Header.Get(k); v != "" {
			headers[k] = v
		}
	}
	return map[string]any{
		"service": a.Service, "url": req.URL.String(), "status": resp.StatusCode,
		"headers": headers, "body": string(raw), "truncated": truncated,
		"ms": time.Since(started).Milliseconds(),
	}, nil
}

func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

func mcpExplain(composePath, service, query string, analyze bool) (any, error) {
	query = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	if query == "" {
		return nil, fmt.Errorf("%s: query is required", utils.ErrUsage)
	}
	prefix := "EXPLAIN "
	if analyze {
		prefix = "EXPLAIN (ANALYZE, BUFFERS) "
	}
	res, err := mcpDBQuery(dbQueryArgs{ComposePath: composePath, Service: service, Query: prefix + query + ";"})
	if err != nil {
		return nil, err
	}
	return map[string]any{"service": service, "plan": res.Output, "truncated": res.Truncated,
		"hint": explainHint(res.Output)}, nil
}

// explainHint is the one line a plan is usually read for.
func explainHint(plan string) string {
	var hints []string
	if strings.Contains(plan, "Seq Scan") {
		hints = append(hints, "a sequential scan: an index on the filtered column would change it")
	}
	if strings.Contains(plan, "Nested Loop") && strings.Contains(plan, "Seq Scan") {
		hints = append(hints, "a nested loop over a scan: the inner side runs once per outer row")
	}
	if strings.Contains(plan, "Sort") && strings.Contains(plan, "external") {
		hints = append(hints, "a sort that spilled to disk: work_mem or an index in that order")
	}
	return strings.Join(hints, "; ")
}
