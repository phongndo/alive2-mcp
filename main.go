// Command alive2-mcp is an MCP server, and a CLI, for checking LLVM IR
// transformations with Alive2.
//
//	alive2-mcp [-opt PATH] [-alive-tv PATH] [-web URL] [-http ADDR]   serve MCP (stdio unless -http)
//	alive2-mcp [flags] verify [-passes P] [-backend B] SRC [TGT]
//	alive2-mcp [flags] link [-passes P] [-function F] SRC [TGT]
//	alive2-mcp [flags] get-link [-verify] LINK
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/phongndo/alive2-mcp/internal/alive"
)

// version is set by release builds; go install builds report their module version.
var version = "dev"

func init() {
	if info, ok := debug.ReadBuildInfo(); ok && version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = strings.TrimPrefix(info.Main.Version, "v")
	}
}

const instructions = `Alive2 proves or refutes that an LLVM IR transformation is correct (tgt refines src).
To check your own compiler change, give passes with opt set to your patched opt binary;
opt=upstream shows what trunk LLVM does instead. Verdicts: correct, incorrect (with a
counterexample), unknown (timeout or approximation), error. Alive2 is bounded and
intraprocedural: loops are unrolled a few times and calls are opaque, so "correct" is
strong evidence rather than proof. create_link publishes IR permanently on alive2.llvm.org.`

func main() { os.Exit(run()) }

// run returns the exit status, so deferred cleanups run before main exits.
func run() int {
	log.SetFlags(0)
	log.SetPrefix("alive2-mcp: ")
	fs := flag.NewFlagSet("alive2-mcp", flag.ExitOnError)
	opt := fs.String("opt", os.Getenv("ALIVE2_MCP_OPT"), "default opt for producing tgt from passes (env ALIVE2_MCP_OPT)")
	aliveTV := fs.String("alive-tv", os.Getenv("ALIVE2_MCP_ALIVE_TV"), "local alive-tv; web only if unset (env ALIVE2_MCP_ALIVE_TV)")
	web := fs.String("web", envOr("ALIVE2_MCP_WEB", alive.DefaultWeb), "Alive2 Compiler Explorer instance (env ALIVE2_MCP_WEB)")
	addr := fs.String("http", "", "serve stateless streamable HTTP on this address instead of stdio")
	showVersion := fs.Bool("version", false, "print the version")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "usage: alive2-mcp [flags] [verify|link|get-link ...]\n\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println(version)
		return 0
	}
	client := alive.NewClient(*web, absIfSet(*aliveTV), absIfSet(*opt))
	if client.AliveTV != "" {
		if _, err := os.Stat(client.AliveTV); err != nil {
			log.Printf("ignoring -alive-tv: %v", err)
			client.AliveTV = ""
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if fs.NArg() > 0 {
		return runCLI(ctx, client, fs.Args())
	}
	server := newServer(client)
	if *addr == "" {
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			log.Print(err)
			return 1
		}
		return 0
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	log.Printf("serving MCP on http://%s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print(err)
		return 1
	}
	return 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func absIfSet(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func ptr[T any](v T) *T { return &v }

func newServer(c *alive.Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "alive2", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	mcp.AddTool(s, &mcp.Tool{
		Name:  "verify",
		Title: "Verify an LLVM IR transformation with Alive2",
		Description: "Prove or refute that tgt refines src, per function. Give tgt directly, or passes " +
			"to produce it with opt (your patched build) or opt=upstream (trunk LLVM). " +
			"Returns each function's verdict and, when not correct, the counterexample.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in alive.VerifyInput) (*mcp.CallToolResult, alive.VerifyOutput, error) {
		out, err := c.Verify(ctx, in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:  "create_link",
		Title: "Create an Alive2 short link",
		Description: "Publish the check as a public alive2.llvm.org/ce/z/... link for an issue or PR, " +
			"then read it back to confirm it holds the submitted IR. Permanent and public: " +
			"get the user's agreement first. A src/tgt link holds one function.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in alive.LinkInput) (*mcp.CallToolResult, alive.LinkOutput, error) {
		out, err := c.Link(ctx, in)
		return nil, out, err
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:  "get_link",
		Title: "Read an Alive2 short link",
		Description: "Fetch the IR and options stored in an alive2.llvm.org/ce/z/... link, such as one " +
			"from an LLVM issue, and optionally re-run its check.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in alive.GetLinkInput) (*mcp.CallToolResult, alive.GetLinkOutput, error) {
		out, err := c.GetLink(ctx, in)
		return nil, out, err
	})
	return s
}

// runCLI returns 0 when the check is correct, 1 when it isn't, 2 on errors.
func runCLI(ctx context.Context, c *alive.Client, args []string) int {
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the full result as JSON")
	var in alive.VerifyInput
	var link alive.LinkInput
	var get alive.GetLinkInput
	switch cmd {
	case "verify", "link":
		fs.StringVar(&in.Passes, "passes", "", "pipeline that turns SRC into TGT")
		fs.StringVar(&in.Opt, "opt", "", "opt binary for -passes, or 'upstream'")
		if cmd == "verify" {
			fs.StringVar(&in.Backend, "backend", "", "auto, local or web")
			fs.BoolVar(&in.IncludeTgt, "tgt", false, "print the produced tgt")
		} else {
			fs.StringVar(&link.Function, "function", "", "function to link")
		}
	case "get-link":
		fs.BoolVar(&get.Verify, "verify", false, "re-run the link's check")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (verify, link, get-link)\n", cmd)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var (
		result  any
		verdict string
		err     error
	)
	switch cmd {
	case "verify", "link":
		if fs.NArg() < 1 || fs.NArg() > 2 {
			fmt.Fprintf(os.Stderr, "usage: alive2-mcp %s [flags] SRC [TGT]\n", cmd)
			return 2
		}
		in.SrcFile = fs.Arg(0)
		in.TgtFile = fs.Arg(1)
		if cmd == "verify" {
			var out alive.VerifyOutput
			out, err = c.Verify(ctx, in)
			result, verdict = out, out.Verdict
			if err == nil && !*asJSON {
				fmt.Printf("%s (%s; tgt: %s)\n", out.Verdict, out.Backend, out.TgtSource)
				printFunctions(out.Functions)
				if out.Tgt != "" {
					fmt.Print(out.Tgt)
				}
			}
		} else {
			link.IR = in.IR
			var out alive.LinkOutput
			out, err = c.Link(ctx, link)
			result, verdict = out, out.Verdict
			if err == nil && !*asJSON {
				fmt.Printf("%s\n%s\n", out.URL, out.Verdict)
				printFunctions(out.Functions)
			}
		}
	case "get-link":
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: alive2-mcp get-link [-verify] LINK")
			return 2
		}
		get.Link = fs.Arg(0)
		var out alive.GetLinkOutput
		out, err = c.GetLink(ctx, get)
		result, verdict = out, out.Verdict
		if err == nil && !*asJSON {
			fmt.Printf("; options: %s\n%s\n", out.Options, strings.TrimRight(out.Source, "\n"))
			if get.Verify {
				fmt.Printf("; %s\n", out.Verdict)
				printFunctions(out.Functions)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	}
	if verdict == "" || verdict == alive.Correct {
		return 0
	}
	return 1
}

func printFunctions(rs []alive.FunctionResult) {
	for _, r := range rs {
		fmt.Printf("  @%s: %s\n", r.Function, r.Verdict)
		if r.Detail != "" {
			fmt.Println("    " + strings.ReplaceAll(r.Detail, "\n", "\n    "))
		}
	}
}
