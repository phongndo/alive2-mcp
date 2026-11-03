package alive

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseRealOutputs(t *testing.T) {
	tests := []struct {
		file    string
		name    string
		want    []string // verdict per function
		funcs   []string
		overall string
		detail  string // substring expected in the first non-correct detail
	}{
		{"out-incorrect.txt", "", []string{Incorrect}, []string{"test"}, Incorrect, "Target is more poisonous than source"},
		{"out-correct-two.txt", "", []string{Correct, Correct}, []string{"f", "g"}, Correct, ""},
		{"out-mismatch.txt", "", []string{Incorrect}, []string{"src"}, Incorrect, "Value mismatch"},
		{"out-correct-one.txt", "f", []string{Correct}, []string{"f"}, Correct, ""},
		{"out-error.txt", "f", []string{Error}, []string{"f"}, Error, "Compilation failed"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			rs := Parse(fixture(t, tt.file), tt.name, 0)
			if len(rs) != len(tt.want) {
				t.Fatalf("got %d results %+v, want %d", len(rs), rs, len(tt.want))
			}
			for i, r := range rs {
				if r.Verdict != tt.want[i] || r.Function != tt.funcs[i] {
					t.Errorf("result %d = %s/%s, want %s/%s", i, r.Function, r.Verdict, tt.funcs[i], tt.want[i])
				}
				if r.Verdict == Correct && r.Detail != "" {
					t.Errorf("correct result has detail %q", r.Detail)
				}
			}
			if got := Overall(rs); got != tt.overall {
				t.Errorf("Overall = %s, want %s", got, tt.overall)
			}
			if tt.detail != "" && !strings.Contains(rs[0].Detail, tt.detail) {
				t.Errorf("detail %q lacks %q", rs[0].Detail, tt.detail)
			}
		})
	}
}

func TestDetailSkipsEchoedIR(t *testing.T) {
	r := Parse(fixture(t, "out-incorrect.txt"), "", 0)[0]
	if !strings.HasPrefix(r.Detail, "Transformation doesn't verify!") {
		t.Errorf("detail starts with %q", r.Detail[:40])
	}
}

func TestCapLines(t *testing.T) {
	r := Parse(fixture(t, "out-incorrect.txt"), "", 5)[0]
	lines := strings.Split(r.Detail, "\n")
	if len(lines) != 6 || !strings.Contains(lines[5], "more lines") {
		t.Errorf("capped detail = %q", r.Detail)
	}
}

func TestOverallOrder(t *testing.T) {
	rs := func(vs ...string) []FunctionResult {
		var out []FunctionResult
		for _, v := range vs {
			out = append(out, FunctionResult{Verdict: v})
		}
		return out
	}
	cases := map[string][]FunctionResult{
		Incorrect: rs(Correct, Unknown, Error, Incorrect),
		Error:     rs(Correct, Unknown, Error),
		Unknown:   rs(Correct, Unknown),
		Correct:   rs(Correct, Correct),
	}
	for want, in := range cases {
		if got := Overall(in); got != want {
			t.Errorf("Overall(%v) = %s, want %s", in, got, want)
		}
	}
	if Overall(nil) != Error {
		t.Error("no results should be an error")
	}
}
