package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func testExtensions(t *testing.T) *mcpExtensions {
	t.Helper()
	s := server.NewMCPServer("corgi", "test", server.WithExtensions(mcpExtensionCapabilities))
	s.AddTool(mcp.NewTool("corgi_up"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("stack is up"), nil
	})
	x := newMCPExtensions(s)
	set := readSkillSet(os.DirFS("../" + skillsDirectory))
	x.skills = func() skillSet { return set }
	return x
}

func askExtensions(t *testing.T, x *mcpExtensions, owner, request string) map[string]any {
	t.Helper()
	raw, ok := x.answer(context.Background(), []byte(request), owner)
	if !ok {
		t.Fatalf("not answered here: %s", request)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSkillsAreListedWithEveryFileAndItsDigest(t *testing.T) {
	x := testExtensions(t)
	result := askExtensions(t, x, "a", `{"jsonrpc":"2.0","id":1,"method":"skills/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`)["result"].(map[string]any)
	skills := result["skills"].([]any)
	if len(skills) < 10 || result["resultType"] != "complete" || result["cacheScope"] != "public" {
		t.Fatalf("every bundled skill, as a cacheable complete result: %d %v", len(skills), result["resultType"])
	}
	for _, raw := range skills {
		skill := raw.(map[string]any)
		uri := skill["uri"].(string)
		name := skill["frontmatter"].(map[string]any)["name"].(string)
		if uri != skillScheme+name+"/SKILL.md" {
			t.Fatalf("the last folder of the address is the skill's name: %s vs %s", uri, name)
		}
		first := skill["resources"].([]any)[0].(map[string]any)
		if !strings.HasPrefix(first["digest"].(string), "sha256:") || first["size"].(float64) <= 0 {
			t.Fatalf("each file carries its digest and size: %v", first)
		}
	}
}

func TestASkillAndItsFilesAreRead(t *testing.T) {
	x := testExtensions(t)
	got := askExtensions(t, x, "a", `{"jsonrpc":"2.0","id":2,"method":"skills/get","params":{"uri":"skill://corgi/review/SKILL.md"}}`)
	if got["result"].(map[string]any)["skill"].(map[string]any)["uri"] != "skill://corgi/review/SKILL.md" {
		t.Fatalf("the skill by its address: %v", got)
	}
	read := askExtensions(t, x, "a", `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"skill://corgi/review/SKILL.md"}}`)
	content := read["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)
	if content["mimeType"] != mimeMarkdown || !strings.HasPrefix(content["text"].(string), "---\nname: review") {
		t.Fatalf("the file as markdown text: %v", content["mimeType"])
	}
	shared := askExtensions(t, x, "a", `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"skill://corgi/review/../_shared/conventions.md"}}`)
	if shared["error"] != nil {
		t.Fatalf("a file a skill points to beside it is readable: %v", shared["error"])
	}
	dir := askExtensions(t, x, "a", `{"jsonrpc":"2.0","id":5,"method":"resources/directory/read","params":{"uri":"skill://corgi/review"}}`)
	if len(dir["result"].(map[string]any)["resources"].([]any)) == 0 {
		t.Fatal("a skill folder lists its files")
	}
	for _, uri := range []string{"skill://corgi/nope/SKILL.md", "skill://corgi/../../go.mod"} {
		missing := askExtensions(t, x, "a", `{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"`+uri+`"}}`)
		if missing["error"].(map[string]any)["code"].(float64) != mcpInvalidParams {
			t.Fatalf("%s is refused: %v", uri, missing)
		}
	}
	if _, ok := x.answer(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"corgi://schema"}}`), "a"); ok {
		t.Fatal("other resources stay with the server")
	}
}

const taskCall = `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"corgi_up","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}}}}}`

func TestASlowToolBecomesATaskOnlyForAClientThatAsksForTasks(t *testing.T) {
	x := testExtensions(t)
	if _, ok := x.answer(context.Background(), []byte(`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"corgi_up","arguments":{}}}`), "a"); ok {
		t.Fatal("a client that did not declare tasks gets the plain call")
	}
	started := askExtensions(t, x, "a", taskCall)["result"].(map[string]any)
	id, _ := started["taskId"].(string)
	if started["resultType"] != "task" || started["status"] != mcpTaskWorking || len(id) != 32 {
		t.Fatalf("the call is handed off: %v", started)
	}
	get := `{"jsonrpc":"2.0","id":9,"method":"tasks/get","params":{"taskId":"` + id + `"}}`
	deadline := time.Now().Add(5 * time.Second)
	var task map[string]any
	for time.Now().Before(deadline) {
		task = askExtensions(t, x, "a", get)["result"].(map[string]any)
		if task["status"] != mcpTaskWorking {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	text, _ := json.Marshal(task["result"])
	if task["status"] != mcpTaskCompleted || !strings.Contains(string(text), "stack is up") {
		t.Fatalf("the task ends with the tool's own result: %v", task)
	}
	if other := askExtensions(t, x, "b", get); other["error"] == nil {
		t.Fatal("another caller cannot read the task")
	}
}

func TestStdioAnswersSkillsAndPassesTheRestOn(t *testing.T) {
	x := testExtensions(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"skills/list","params":{}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n")
	var out bytes.Buffer
	passed := make(chan string, 1)
	err := x.overStdio(context.Background(), in, &out, func(_ context.Context, r io.Reader, _ io.Writer) error {
		raw, err := io.ReadAll(r)
		passed <- string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-passed; !strings.Contains(got, `"ping"`) || strings.Contains(got, "skills/list") {
		t.Fatalf("only the other call reaches the server: %q", got)
	}
	if !strings.Contains(out.String(), `"skills"`) {
		t.Fatalf("the skills answer is written: %.80s", out.String())
	}
}
