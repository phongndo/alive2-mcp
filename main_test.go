package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/phongndo/alive2-mcp/internal/alive"
)

func connect(t *testing.T, c *alive.Client) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := newServer(c).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestToolsAreListed(t *testing.T) {
	s := connect(t, alive.NewClient(alive.DefaultWeb, "", ""))
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"verify": true, "create_link": false, "get_link": true}
	if len(res.Tools) != len(want) {
		t.Fatalf("got %d tools", len(res.Tools))
	}
	for _, tool := range res.Tools {
		readOnly, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %s", tool.Name)
			continue
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s: readOnlyHint should be %v", tool.Name, readOnly)
		}
		if tool.OutputSchema == nil || tool.Description == "" {
			t.Errorf("%s: missing output schema or description", tool.Name)
		}
	}
}

func TestBadInputIsAToolError(t *testing.T) {
	s := connect(t, alive.NewClient(alive.DefaultWeb, "", ""))
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "verify",
		Arguments: map[string]any{"passes": "gvn"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing src should be reported as a tool error")
	}
}

func TestVerifyOverMCP(t *testing.T) {
	ce := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"asm":[{"text":"----------------------------------------"},` +
			`{"text":"define i8 @src(i8 %x) {"},{"text":"Transformation seems to be correct!"}]}`))
	}))
	defer ce.Close()
	s := connect(t, alive.NewClient(ce.URL, "", ""))
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "verify",
		Arguments: map[string]any{
			"src": "define i8 @f(i8 %x) {\n  ret i8 %x\n}\n",
			"tgt": "define i8 @f(i8 %x) {\n  ret i8 %x\n}\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, ok := res.StructuredContent.(map[string]any)
	if res.IsError || !ok || out["verdict"] != alive.Correct {
		t.Fatalf("result = %+v", res)
	}
}
