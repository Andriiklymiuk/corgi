package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const handsToolNote = " The Mac must allow it first (corgi agent hands allow on that Mac) and shows a banner while you work. " +
	"Use only when the user asked you to operate that Mac; never to get around a permission prompt. Omit peer for this Mac."

func handsToolResult(out handsResult, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(mcpErrorText(err.Error())), nil
	}
	text := out.Text
	if out.Width > 0 {
		text = strings.TrimSpace(text + "\n(picture " + itoaInt(out.Width) + "x" + itoaInt(out.Height) + " px; act takes coordinates in these pixels)")
	}
	if out.Image == "" {
		if text == "" {
			text = "done"
		}
		return mcp.NewToolResultText(text), nil
	}
	mime := out.Mime
	if mime == "" {
		mime = "image/jpeg"
	}
	return mcp.NewToolResultImage(text, out.Image, mime), nil
}

func itoaInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

const handsTunnelBlockedMsg = "corgi_mac_* tools are off over a public tunnel: a screenshot or a click must not reach the internet; use them from Claude Code on a paired machine, or set CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1 behind an identity proxy"

func handsCall(r mcp.CallToolRequest, req handsRequest) (*mcp.CallToolResult, error) {
	if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
		return mcp.NewToolResultError(handsTunnelBlockedMsg), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return handsToolResult(callHands(ctx, r.GetString("peer", ""), req))
}

func parseHandsActions(raw any) ([]handsAction, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out []handsAction
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func registerHandsMCPTools(s *server.MCPServer) {
	s.AddTool(newCorgiTool("corgi_mac_windows",
		mcp.WithDescription("The windows and apps open on a Mac (app, title, position, which is in front), as text: "+
			"cheap, no picture, the first thing to call."+handsToolNote),
		mcp.WithString("peer", mcp.Description("Peer Mac name from corgi agent peers; omit for this Mac")),
	), func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handsCall(r, handsRequest{Do: "windows"})
	})

	s.AddTool(newCorgiTool("corgi_mac_look",
		mcp.WithDescription("A screenshot of a Mac's screen or one window, as a JPEG no longer than max pixels (default 1280). "+
			"Password managers, Messages and Mail never appear."+handsToolNote),
		mcp.WithString("peer", mcp.Description("Peer Mac name; omit for this Mac")),
		mcp.WithString("window", mcp.Description("Only this window: an app name or a title")),
		mcp.WithNumber("max", mcp.Description("Longest side in pixels, up to 2560; smaller is cheaper")),
	), func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handsCall(r, handsRequest{Do: "look", Window: r.GetString("window", ""), Max: r.GetInt("max", 0)})
	})

	s.AddTool(newCorgiTool("corgi_mac_act",
		mcp.WithDescription("Use a Mac: click, double, right (right click), move, drag, type, key (like cmd+s, enter, esc, tab, up), "+
			"scroll, wait. Coordinates are pixels of the last corgi_mac_look picture. look:true returns a picture after."+handsToolNote),
		mcp.WithString("peer", mcp.Description("Peer Mac name; omit for this Mac")),
		mcp.WithArray("actions", mcp.Required(), mcp.Description(`In order, e.g. [{"do":"click","x":640,"y":400},{"do":"type","text":"hello"},{"do":"key","keys":"cmd+s"},{"do":"scroll","dy":-400},{"do":"drag","x":1,"y":2,"x2":3,"y2":4},{"do":"wait","ms":500}]`), mcp.Items(map[string]any{"type": "object"})),
		mcp.WithBoolean("look", mcp.Description("Return a screenshot after the actions")),
	), func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		acts, err := parseHandsActions(r.GetArguments()["actions"])
		if err != nil {
			return mcp.NewToolResultError("actions: " + err.Error()), nil
		}
		return handsCall(r, handsRequest{Do: "act", Actions: acts, Look: r.GetBool("look", false)})
	})

	s.AddTool(newCorgiTool("corgi_mac_menus",
		mcp.WithDescription("The front app's menu bar on a Mac as JSON (titles, enabled, shortcuts, submenus): "+
			"more reliable than clicking, and cheap. Pair with corgi_mac_menu."+handsToolNote),
		mcp.WithString("peer", mcp.Description("Peer Mac name; omit for this Mac")),
	), func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handsCall(r, handsRequest{Do: "menus"})
	})

	s.AddTool(newCorgiTool("corgi_mac_menu",
		mcp.WithDescription("Press a menu item of the front app on a Mac by its path, like [\"File\", \"Save\"]."+handsToolNote),
		mcp.WithString("peer", mcp.Description("Peer Mac name; omit for this Mac")),
		mcp.WithArray("path", mcp.Required(), mcp.Description("Menu titles from the menu bar down"), mcp.WithStringItems()),
	), func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var path []string
		if raw, ok := r.GetArguments()["path"].([]any); ok {
			for _, p := range raw {
				if s, ok := p.(string); ok {
					path = append(path, s)
				}
			}
		}
		return handsCall(r, handsRequest{Do: "menu", Path: path})
	})

	s.AddTool(newCorgiTool("corgi_mac_open",
		mcp.WithDescription("Open an app (by name) or a URL on a Mac and bring it to the front."+handsToolNote),
		mcp.WithString("peer", mcp.Description("Peer Mac name; omit for this Mac")),
		mcp.WithString("app", mcp.Description("App name, like Safari or Xcode")),
		mcp.WithString("url", mcp.Description("A URL to open instead")),
	), func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handsCall(r, handsRequest{Do: "open", App: r.GetString("app", ""), URL: r.GetString("url", "")})
	})
}
