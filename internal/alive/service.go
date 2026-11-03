package alive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Upstream as the opt value means alive2.llvm.org runs the passes in its own
// trunk LLVM, instead of producing tgt locally.
const Upstream = "upstream"

const (
	defaultSMTSeconds = 10
	defaultMaxLines   = 40
)

// IR names the modules to check. Exactly one of Src and SrcFile is required;
// tgt comes from Tgt, TgtFile, or Passes run through Opt.
type IR struct {
	Src     string `json:"src,omitempty" jsonschema:"source module as LLVM IR text (or use src_file)"`
	SrcFile string `json:"src_file,omitempty" jsonschema:"path to the source .ll file; relative paths resolve against the server's working directory"`
	Tgt     string `json:"tgt,omitempty" jsonschema:"transformed module as LLVM IR text; functions are matched to src by name"`
	TgtFile string `json:"tgt_file,omitempty" jsonschema:"path to the transformed .ll file"`
	Passes  string `json:"passes,omitempty" jsonschema:"instead of tgt: a -passes pipeline that turns src into tgt, e.g. gvn or instcombine"`
	Opt     string `json:"opt,omitempty" jsonschema:"opt binary that runs passes, e.g. your patched build; 'upstream' runs them in trunk LLVM on alive2.llvm.org. Default: the server's -opt flag, else upstream"`
}

// VerifyInput is the verify tool's input.
type VerifyInput struct {
	IR
	Backend        string `json:"backend,omitempty" jsonschema:"auto (default: local alive-tv if configured, else web), local, or web"`
	SMTTimeout     int    `json:"smt_timeout,omitempty" jsonschema:"seconds per SMT query (default 10)"`
	MaxDetailLines int    `json:"max_detail_lines,omitempty" jsonschema:"cap on each counterexample's lines (default 40)"`
	IncludeTgt     bool   `json:"include_tgt,omitempty" jsonschema:"also return the tgt module that opt produced"`
}

// VerifyOutput is the verify tool's result.
type VerifyOutput struct {
	Verdict   string           `json:"verdict" jsonschema:"worst verdict across functions"`
	Backend   string           `json:"backend" jsonschema:"local or web"`
	TgtSource string           `json:"tgt_source" jsonschema:"where tgt came from"`
	Functions []FunctionResult `json:"functions"`
	Tgt       string           `json:"tgt,omitempty"`
}

// LinkInput is the create_link tool's input.
type LinkInput struct {
	IR
	Function string `json:"function,omitempty" jsonschema:"function to link when src and tgt share several; a pair link holds one"`
}

// LinkOutput is the create_link tool's result.
type LinkOutput struct {
	URL       string           `json:"url"`
	Verdict   string           `json:"verdict" jsonschema:"what the link will show"`
	Functions []FunctionResult `json:"functions"`
}

// GetLinkInput is the get_link tool's input.
type GetLinkInput struct {
	Link           string `json:"link" jsonschema:"an alive2.llvm.org/ce/z/... URL or its id"`
	Verify         bool   `json:"verify,omitempty" jsonschema:"also run the link's check again on the website"`
	MaxDetailLines int    `json:"max_detail_lines,omitempty" jsonschema:"cap on each counterexample's lines (default 40)"`
}

// GetLinkOutput is the get_link tool's result.
type GetLinkOutput struct {
	Source    string           `json:"source" jsonschema:"the stored IR; pair links hold @src and @tgt"`
	Options   string           `json:"options" jsonschema:"stored alive-tv arguments"`
	Passes    string           `json:"passes,omitempty" jsonschema:"the --passes pipeline, if the link runs one"`
	Verdict   string           `json:"verdict,omitempty"`
	Functions []FunctionResult `json:"functions,omitempty"`
}

func read(text, file, what string) (string, error) {
	switch {
	case text != "" && file != "":
		return "", fmt.Errorf("give %s or %s_file, not both", what, what)
	case file != "":
		if rest, ok := strings.CutPrefix(file, "~/"); ok {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			file = filepath.Join(home, rest)
		}
		b, err := os.ReadFile(file)
		return string(b), err
	}
	return text, nil
}

// modules resolves the input into src and tgt. upstream is set, and tgt empty,
// when the website should run the passes itself.
func (c *Client) modules(ctx context.Context, in IR) (src, tgt, from, upstream string, err error) {
	if src, err = read(in.Src, in.SrcFile, "src"); err != nil {
		return
	}
	if src == "" {
		return "", "", "", "", errors.New("src or src_file is required")
	}
	if tgt, err = read(in.Tgt, in.TgtFile, "tgt"); err != nil || tgt != "" {
		if tgt != "" && in.Passes != "" {
			err = errors.New("give tgt or passes, not both")
		}
		return src, tgt, "given", "", err
	}
	if in.Passes == "" {
		return "", "", "", "", errors.New("tgt, tgt_file or passes is required")
	}
	opt := in.Opt
	if opt == "" {
		opt = c.Opt
	}
	if opt == "" || opt == Upstream {
		return src, "", "trunk opt on " + c.Web + " -passes=" + in.Passes, in.Passes, nil
	}
	tgt, err = RunOpt(ctx, opt, in.Passes, src)
	return src, tgt, opt + " -passes=" + in.Passes, "", err
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

// Verify checks that tgt refines src.
func (c *Client) Verify(ctx context.Context, in VerifyInput) (VerifyOutput, error) {
	src, tgt, from, upstream, err := c.modules(ctx, in.IR)
	if err != nil {
		return VerifyOutput{}, err
	}
	lines := orDefault(in.MaxDetailLines, defaultMaxLines)
	out := VerifyOutput{TgtSource: from}
	if in.IncludeTgt {
		out.Tgt = tgt
	}
	backend := in.Backend
	if upstream != "" {
		if backend == "local" {
			return out, errors.New("opt=upstream runs on the website; use a local opt for backend=local")
		}
		backend = "web"
	}
	switch backend {
	case "", "auto":
		backend = "web"
		if c.AliveTV != "" {
			backend = "local"
		}
	case "local":
		if c.AliveTV == "" {
			return out, errors.New("no local alive-tv configured; start the server with -alive-tv")
		}
	case "web":
	default:
		return out, fmt.Errorf("unknown backend %q", backend)
	}
	out.Backend = backend

	switch {
	case upstream != "":
		text, err := c.WebAlive(ctx, src, "--passes="+upstream)
		if err != nil {
			return out, err
		}
		out.Functions = Parse(text, "", lines)
	case backend == "local":
		text, err := c.LocalAlive(ctx, src, tgt, orDefault(in.SMTTimeout, defaultSMTSeconds))
		if err != nil {
			return out, err
		}
		out.Functions = Parse(text, "", lines)
	default:
		names := SharedFunctions(src, tgt)
		if len(names) == 0 {
			return out, errors.New("src and tgt define no function with the same name")
		}
		if out.Functions, err = c.webPairs(ctx, src, tgt, names, lines); err != nil {
			return out, err
		}
	}
	out.Verdict = Overall(out.Functions)
	return out, nil
}

// webPairs checks each shared function on the website, four at a time,
// keeping src order.
func (c *Client) webPairs(ctx context.Context, src, tgt string, names []string, lines int) ([]FunctionResult, error) {
	type result struct {
		rs  []FunctionResult
		err error
	}
	results := make([]result, len(names))
	sem := make(chan struct{}, 4)
	done := make(chan struct{})
	for i, name := range names {
		go func() {
			defer func() { done <- struct{}{} }()
			sem <- struct{}{}
			defer func() { <-sem }()
			module, err := Pair(src, tgt, name)
			if err != nil {
				results[i].err = err
				return
			}
			text, err := c.WebAlive(ctx, module, "")
			results[i] = result{Parse(text, name, lines), err}
		}()
	}
	for range names {
		<-done
	}
	var flat []FunctionResult
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		flat = append(flat, r.rs...)
	}
	return flat, nil
}

// Link publishes a short link for the check and confirms it by reading it back.
func (c *Client) Link(ctx context.Context, in LinkInput) (LinkOutput, error) {
	src, tgt, _, upstream, err := c.modules(ctx, in.IR)
	if err != nil {
		return LinkOutput{}, err
	}
	source, options := src, ""
	if upstream != "" {
		options = "--passes=" + upstream
	} else {
		var names []string
		for _, n := range SharedFunctions(src, tgt) {
			if in.Function == "" || n == in.Function {
				names = append(names, n)
			}
		}
		if len(names) != 1 {
			return LinkOutput{}, fmt.Errorf("a pair link holds one function; choose one of %v with function", names)
		}
		if source, err = Pair(src, tgt, names[0]); err != nil {
			return LinkOutput{}, err
		}
	}
	text, err := c.WebAlive(ctx, source, options)
	if err != nil {
		return LinkOutput{}, err
	}
	url, err := c.Shorten(ctx, source, options)
	if err != nil {
		return LinkOutput{}, err
	}
	rs := Parse(text, "", defaultMaxLines)
	return LinkOutput{URL: url, Verdict: Overall(rs), Functions: rs}, nil
}

// GetLink reads a short link and optionally re-runs its check.
func (c *Client) GetLink(ctx context.Context, in GetLinkInput) (GetLinkOutput, error) {
	s, err := c.Resolve(ctx, in.Link)
	if err != nil {
		return GetLinkOutput{}, err
	}
	out := GetLinkOutput{Source: s.Source, Options: s.Options()}
	for _, f := range strings.Fields(out.Options) {
		if p, ok := strings.CutPrefix(f, "--passes="); ok {
			out.Passes = p
		}
	}
	if in.Verify {
		text, err := c.WebAlive(ctx, s.Source, out.Options)
		if err != nil {
			return out, err
		}
		out.Functions = Parse(text, "", orDefault(in.MaxDetailLines, defaultMaxLines))
		out.Verdict = Overall(out.Functions)
	}
	return out, nil
}
