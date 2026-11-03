package alive

import (
	"fmt"
	"regexp"
	"strings"
)

// Verdicts, from worst to best.
const (
	Incorrect = "incorrect"
	Error     = "error"
	Unknown   = "unknown"
	Correct   = "correct"
)

// FunctionResult is Alive2's verdict for one function.
type FunctionResult struct {
	Function string `json:"function"`
	Verdict  string `json:"verdict" jsonschema:"correct, incorrect, unknown (timeout or approximation) or error"`
	Detail   string `json:"detail,omitempty" jsonschema:"Alive2's diagnosis and counterexample, when not correct"`
}

var (
	separator = strings.Repeat("-", 40)
	unknownRE = regexp.MustCompile(`[Tt]imeout|timed out|failed-to-prove|Couldn't prove|approximat`)
	headerRE  = regexp.MustCompile(`define[^\n]*?` + fnName)
)

func verdictOf(text string) string {
	switch {
	case strings.Contains(text, "doesn't verify"):
		return Incorrect
	case strings.Contains(text, "seems to be correct"):
		return Correct
	case unknownRE.MatchString(text):
		return Unknown
	}
	return Error
}

// Parse splits alive-tv output into one result per function. When name is
// set, every result is attributed to it, since pair mode reports @src.
// maxLines caps each detail; zero means no cap.
func Parse(output, name string, maxLines int) []FunctionResult {
	blocks := strings.Split(output, separator)
	if len(blocks) > 1 {
		blocks = blocks[1:]
	}
	var out []FunctionResult
	for _, b := range blocks {
		b = strings.TrimSpace(strings.SplitN(b, "\nSummary:", 2)[0])
		if b == "" {
			continue
		}
		r := FunctionResult{Function: name, Verdict: verdictOf(b)}
		if r.Function == "" {
			r.Function = "?"
			if m := headerRE.FindStringSubmatch(b); m != nil {
				r.Function = m[1]
			}
		}
		if r.Verdict != Correct {
			r.Detail = capLines(b[diagnosisStart(b):], maxLines)
		}
		out = append(out, r)
	}
	return out
}

// diagnosisStart skips the echoed IR to where Alive2 explains the verdict.
func diagnosisStart(block string) int {
	start := -1
	for _, k := range []string{"Transformation doesn't verify", "ERROR:"} {
		if i := strings.Index(block, k); i >= 0 && (start < 0 || i < start) {
			start = i
		}
	}
	return max(start, 0)
}

func capLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n <= 0 || len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n... %d more lines; raise max_detail_lines", len(lines)-n)
}

// Overall is the worst verdict among results, or Error when there are none.
func Overall(rs []FunctionResult) string {
	seen := map[string]bool{}
	for _, r := range rs {
		seen[r.Verdict] = true
	}
	for _, v := range []string{Incorrect, Error, Unknown} {
		if seen[v] {
			return v
		}
	}
	if len(rs) == 0 {
		return Error
	}
	return Correct
}
