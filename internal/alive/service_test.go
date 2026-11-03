package alive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeCE stands in for alive2.llvm.org/ce: compile answers with a canned
// alive-tv transcript, and the shortener stores sessions in memory.
type fakeCE struct {
	mu       sync.Mutex
	compiles []map[string]any
	links    map[string]json.RawMessage
	output   func(source, args string) string
	tamper   bool // store a different source than was submitted
}

func (f *fakeCE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/compiler/alive/compile":
		var body struct {
			Source  string `json:"source"`
			Options struct {
				UserArguments string `json:"userArguments"`
			} `json:"options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.compiles = append(f.compiles, map[string]any{"source": body.Source, "args": body.Options.UserArguments})
		var lines []map[string]string
		for _, l := range strings.Split(f.output(body.Source, body.Options.UserArguments), "\n") {
			lines = append(lines, map[string]string{"text": l})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "asm": lines})
	case r.Method == http.MethodPost && r.URL.Path == "/shortener":
		var body struct {
			Sessions []json.RawMessage `json:"sessions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		stored := body.Sessions[0]
		if f.tamper {
			stored = json.RawMessage(`{"source":"tampered","compilers":[{"id":"alive","options":""}]}`)
		}
		f.links["abc123"] = stored
		_ = json.NewEncoder(w).Encode(map[string]string{"url": "http://" + r.Host + "/z/abc123"})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/shortlinkinfo/"):
		s, ok := f.links[strings.TrimPrefix(r.URL.Path, "/api/shortlinkinfo/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"sessions":[` + string(s) + `]}`))
	default:
		http.NotFound(w, r)
	}
}

func newFake(t *testing.T, output func(source, args string) string) (*fakeCE, *Client) {
	t.Helper()
	f := &fakeCE{links: map[string]json.RawMessage{}, output: output}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, NewClient(srv.URL+"/", "", "")
}

// byMarker answers incorrect when tgt is the shl version, else correct.
func byMarker(t *testing.T) func(string, string) string {
	return func(source, _ string) string {
		if strings.Contains(source, "shl i8 %x, 2") {
			return fixture(t, "out-mismatch.txt")
		}
		return fixture(t, "out-correct-one.txt")
	}
}

func TestVerifyWebPairs(t *testing.T) {
	f, c := newFake(t, byMarker(t))
	out, err := c.Verify(context.Background(), VerifyInput{IR: IR{SrcFile: "testdata/pair-src.ll", TgtFile: "testdata/pair-tgt.ll"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Backend != "web" || out.TgtSource != "given" || out.Verdict != Incorrect {
		t.Errorf("out = %+v", out)
	}
	if len(f.compiles) != 2 {
		t.Fatalf("want one website check per shared function, got %d", len(f.compiles))
	}
	got := map[string]string{}
	for _, r := range out.Functions {
		got[r.Function] = r.Verdict
	}
	if got["h"] != Incorrect || got["f"] != Correct {
		t.Errorf("verdicts = %v (results must be named after the paired function)", got)
	}
}

func TestVerifyUpstreamSendsPasses(t *testing.T) {
	f, c := newFake(t, func(string, string) string { return fixture(t, "out-incorrect.txt") })
	out, err := c.Verify(context.Background(), VerifyInput{IR: IR{SrcFile: "testdata/gvn-pre.ll", Passes: "gvn"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.compiles[0]["args"] != "--passes=gvn" || out.Verdict != Incorrect || out.Functions[0].Function != "test" {
		t.Errorf("args=%v out=%+v", f.compiles[0]["args"], out)
	}
	if _, err := c.Verify(context.Background(), VerifyInput{IR: IR{SrcFile: "testdata/gvn-pre.ll", Passes: "gvn"}, Backend: "local"}); err == nil {
		t.Error("upstream passes with backend=local should fail")
	}
}

func TestVerifyLocalOpt(t *testing.T) {
	dir := t.TempDir()
	opt := filepath.Join(dir, "opt")
	// A stand-in opt that records its arguments and echoes the module unchanged.
	script := "#!/bin/sh\necho \"$@\" > " + filepath.Join(dir, "args") + "\ncat\n"
	if err := os.WriteFile(opt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	f, c := newFake(t, byMarker(t))
	out, err := c.Verify(context.Background(), VerifyInput{
		IR:         IR{SrcFile: "testdata/pair-src.ll", Passes: "instcombine", Opt: opt},
		IncludeTgt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if !strings.Contains(string(args), "-passes=instcombine") {
		t.Errorf("opt args = %q", args)
	}
	if out.Verdict != Correct || !strings.Contains(out.TgtSource, opt) || out.Tgt != fixture(t, "pair-src.ll") {
		t.Errorf("out = %+v", out)
	}
	if len(f.compiles) != 2 || f.compiles[0]["args"] != "" {
		t.Errorf("local tgt must be checked as pairs, got %v", f.compiles)
	}
}

func TestVerifyInputErrors(t *testing.T) {
	_, c := newFake(t, byMarker(t))
	for name, in := range map[string]VerifyInput{
		"no src":       {IR: IR{Passes: "gvn"}},
		"no tgt":       {IR: IR{Src: "define void @f() {\n}"}},
		"both src":     {IR: IR{Src: "x", SrcFile: "testdata/gvn-pre.ll", Passes: "gvn"}},
		"tgt+passes":   {IR: IR{Src: "x", Tgt: "y", Passes: "gvn"}},
		"no shared fn": {IR: IR{Src: "define void @a() {\n}", Tgt: "define void @b() {\n}"}},
		"bad backend":  {IR: IR{Src: "x", Tgt: "y"}, Backend: "cloud"},
		"local unset":  {IR: IR{Src: "x", Tgt: "y"}, Backend: "local"},
		"missing file": {IR: IR{SrcFile: "testdata/nope.ll", Passes: "gvn"}},
	} {
		if _, err := c.Verify(context.Background(), in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLinkReadsBack(t *testing.T) {
	f, c := newFake(t, byMarker(t))
	in := LinkInput{IR: IR{SrcFile: "testdata/pair-src.ll", TgtFile: "testdata/pair-tgt.ll"}, Function: "h"}
	out, err := c.Link(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.URL, "/z/abc123") || out.Verdict != Incorrect {
		t.Errorf("out = %+v", out)
	}
	if _, err := c.Link(context.Background(), LinkInput{IR: in.IR}); err == nil {
		t.Error("two shared functions without function= should fail")
	}
	f.tamper = true
	if _, err := c.Link(context.Background(), in); err == nil || !strings.Contains(err.Error(), "does not hold") {
		t.Errorf("a link whose stored IR differs must be rejected, got %v", err)
	}
}

func TestGetLink(t *testing.T) {
	f, c := newFake(t, func(string, string) string { return fixture(t, "out-correct-two.txt") })
	f.links["xyz"] = json.RawMessage(`{"language":"llvm","source":"define i8 @f(i8 %x) {\n  ret i8 %x\n}","compilers":[{"id":"alive","options":"--passes=instcombine"}]}`)
	out, err := c.GetLink(context.Background(), GetLinkInput{Link: "https://alive2.llvm.org/ce/z/xyz", Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Passes != "instcombine" || out.Verdict != Correct || !strings.Contains(out.Source, "@f") {
		t.Errorf("out = %+v", out)
	}
	if f.compiles[0]["args"] != "--passes=instcombine" {
		t.Errorf("re-run must use the stored options, got %v", f.compiles[0]["args"])
	}
	if _, err := c.GetLink(context.Background(), GetLinkInput{Link: "missing"}); err == nil {
		t.Error("unknown link should fail")
	}
}
