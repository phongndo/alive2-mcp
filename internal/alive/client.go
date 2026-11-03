// Package alive checks LLVM IR transformations with Alive2, either with a local
// alive-tv or on a Compiler Explorer instance that runs alive-tv as a compiler
// (normally https://alive2.llvm.org/ce).
package alive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultWeb is the public Alive2 Compiler Explorer instance.
const DefaultWeb = "https://alive2.llvm.org/ce"

// Client talks to the website and, when configured, local tools.
type Client struct {
	Web     string       // Compiler Explorer base URL, without a trailing slash
	AliveTV string       // local alive-tv binary; empty means web only
	Opt     string       // default opt for producing tgt from passes; empty means upstream
	HTTP    *http.Client // used for every web request
}

// NewClient returns a client with a three-minute HTTP timeout.
func NewClient(web, aliveTV, opt string) *Client {
	return &Client{
		Web:     strings.TrimRight(web, "/"),
		AliveTV: aliveTV,
		Opt:     opt,
		HTTP:    &http.Client{Timeout: 3 * time.Minute},
	}
}

func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Web+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "alive2-mcp")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: HTTP %d: %.300s", method, path, resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: decoding response: %w", method, path, err)
	}
	return nil
}

type textLines []struct {
	Text string `json:"text"`
}

func (t textLines) join() string {
	parts := make([]string, len(t))
	for i, l := range t {
		parts[i] = l.Text
	}
	return strings.Join(parts, "\n")
}

// WebAlive runs alive-tv on the website. With args "" it compares @src with
// @tgt; with "--passes=P" it optimizes every function with trunk opt and checks
// each result.
func (c *Client) WebAlive(ctx context.Context, source, args string) (string, error) {
	var out struct {
		Asm    textLines `json:"asm"`
		Stderr textLines `json:"stderr"`
	}
	body := map[string]any{
		"source":  source,
		"options": map[string]any{"userArguments": args, "filters": map[string]any{}},
	}
	if err := c.call(ctx, http.MethodPost, "/api/compiler/alive/compile", body, &out); err != nil {
		return "", err
	}
	return out.Asm.join() + "\n" + out.Stderr.join(), nil
}

// Session is what a short link stores.
type Session struct {
	Language  string     `json:"language"`
	Source    string     `json:"source"`
	Compilers []compiler `json:"compilers"`
}

type compiler struct {
	ID      string `json:"id"`
	Options string `json:"options"`
}

// Options returns the alive-tv arguments stored with the session.
func (s Session) Options() string {
	if len(s.Compilers) == 0 {
		return ""
	}
	return s.Compilers[0].Options
}

// Shorten stores source and options as a public short link, then reads the
// link back and confirms the stored copy matches.
func (c *Client) Shorten(ctx context.Context, source, options string) (string, error) {
	session := map[string]any{
		"id": 1, "language": "llvm", "source": source,
		"compilers": []compiler{{ID: "alive", Options: options}},
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, http.MethodPost, "/shortener", map[string]any{"sessions": []any{session}}, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("the shortener returned no url")
	}
	stored, err := c.Resolve(ctx, out.URL)
	if err != nil {
		return "", fmt.Errorf("reading back %s: %w", out.URL, err)
	}
	if strings.TrimSpace(stored.Source) != strings.TrimSpace(source) || stored.Options() != options {
		return "", fmt.Errorf("%s does not hold the submitted input", out.URL)
	}
	return out.URL, nil
}

var shortID = regexp.MustCompile(`([A-Za-z0-9_-]+)/?$`)

// Resolve reads a short link, given as a URL or a bare id, into its session.
func (c *Client) Resolve(ctx context.Context, link string) (Session, error) {
	m := shortID.FindStringSubmatch(strings.TrimSpace(link))
	if m == nil {
		return Session{}, fmt.Errorf("not a short link: %q", link)
	}
	var out struct {
		Sessions []Session `json:"sessions"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/shortlinkinfo/"+m[1], nil, &out); err != nil {
		return Session{}, err
	}
	if len(out.Sessions) == 0 {
		return Session{}, fmt.Errorf("short link %q has no sessions", link)
	}
	return out.Sessions[0], nil
}

// LocalAlive runs alive-tv on two modules, which pairs functions by name.
func (c *Client) LocalAlive(ctx context.Context, src, tgt string, smtSeconds int) (string, error) {
	dir, err := os.MkdirTemp("", "alive2-mcp-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	a, b := filepath.Join(dir, "src.ll"), filepath.Join(dir, "tgt.ll")
	if err := os.WriteFile(a, []byte(src), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(b, []byte(tgt), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.AliveTV, "-smt-to="+strconv.Itoa(smtSeconds*1000), a, b)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return "", fmt.Errorf("alive-tv: %w", err)
	}
	return string(out), nil // alive-tv exits nonzero when a transform is incorrect
}

// RunOpt produces tgt by running passes through an opt binary.
func RunOpt(ctx context.Context, opt, passes, src string) (string, error) {
	cmd := exec.CommandContext(ctx, opt, "-passes="+passes, "-S", "-", "-o", "-")
	cmd.Stdin = strings.NewReader(src)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s -passes=%s: %w\n%s", opt, passes, err, stderr.String())
	}
	return stdout.String(), nil
}
