# alive2-mcp

An MCP server and CLI for checking LLVM IR transformations with
[Alive2](https://github.com/AliveToolkit/alive2). It lets an agent prove or refute that an
optimization is correct, check a patched `opt` against trunk, and read or create the
`alive2.llvm.org/ce/z/...` links used in LLVM issues and PRs.

It talks to the public instance at [alive2.llvm.org](https://alive2.llvm.org/ce/) by
default, and to a local `alive-tv` when you have one. It's a single static Go binary
with no runtime dependencies.

## Tools

| Tool | Does | Annotations |
| --- | --- | --- |
| `verify` | Checks that `tgt` refines `src`, per function. `tgt` is given directly, or produced from `passes` by your `opt` or by trunk LLVM (`opt: "upstream"`). Returns each verdict and, when not correct, Alive2's counterexample. | read-only |
| `create_link` | Publishes the check as a short link, then reads it back to confirm it holds the submitted IR. | additive, idempotent |
| `get_link` | Reads a short link back into IR and options, and optionally re-runs it. | read-only |

Verdicts are `correct`, `incorrect`, `unknown` (timeout or approximation) and `error`.
Inputs take IR as text (`src`, `tgt`) or as paths (`src_file`, `tgt_file`), so agents
don't have to paste large modules. Results are returned as structured content.

## Install

| With | Run |
| --- | --- |
| Homebrew (macOS, Linux) | `brew install phongndo/tap/alive2-mcp` |
| mise | `mise use -g github:phongndo/alive2-mcp` |
| Go | `go install github.com/phongndo/alive2-mcp@latest` |
| Nix | `nix profile add github:phongndo/alive2-mcp` |
| Docker | `docker pull ghcr.io/phongndo/alive2-mcp` |
| Download | archives for Linux, macOS and Windows on the [releases page](https://github.com/phongndo/alive2-mcp/releases) |

It's also listed in the [MCP Registry](https://registry.modelcontextprotocol.io) as
`io.github.phongndo/alive2-mcp`. Release archives carry build provenance; verify one
with `gh attestation verify <archive> --repo phongndo/alive2-mcp`.

## Use

### Claude Code

```sh
claude mcp add alive2 -- alive2-mcp -opt /path/to/build/bin/opt
```

or in a project's `.mcp.json`:

```json
{
  "mcpServers": {
    "alive2": { "command": "alive2-mcp", "args": ["-opt", "build/bin/opt"] }
  }
}
```

### Codex

```sh
codex mcp add alive2 -- alive2-mcp -opt /path/to/build/bin/opt
```

### Without installing

The image runs the server over stdio. It can't see your files or your `opt`, so pass
IR as text and use `opt: "upstream"`:

```json
{
  "mcpServers": {
    "alive2": { "command": "docker", "args": ["run", "-i", "--rm", "ghcr.io/phongndo/alive2-mcp"] }
  }
}
```

### Flags

| Flag | Env | Meaning |
| --- | --- | --- |
| `-opt PATH` | `ALIVE2_MCP_OPT` | Default `opt` for turning `passes` into `tgt`, typically your patched build. Without it, passes run upstream. |
| `-alive-tv PATH` | `ALIVE2_MCP_ALIVE_TV` | Local `alive-tv`; checks then run locally, with no network. |
| `-web URL` | `ALIVE2_MCP_WEB` | Alive2 Compiler Explorer instance (default `https://alive2.llvm.org/ce`). |
| `-http ADDR` | | Serve stateless streamable HTTP instead of stdio. |

### CLI

The same binary works from a shell. Exit status is 0 for correct, 1 for anything else
and 2 for errors, so it fits scripts and `git bisect run`.

```sh
alive2-mcp -opt build/bin/opt verify -passes gvn repro.ll   # your patch
alive2-mcp verify -opt upstream -passes gvn repro.ll        # trunk LLVM
alive2-mcp verify before.ll after.ll                        # two files, matched by name
alive2-mcp get-link -verify https://alive2.llvm.org/ce/z/_kCuTV
alive2-mcp link -function f before.ll after.ll              # publishes
```

## How it works

The Alive2 website is a [Compiler Explorer](https://github.com/compiler-explorer/compiler-explorer)
instance running `alive-tv` as a compiler, so this uses Compiler Explorer's API:
`/api/compiler/alive/compile`, `/shortener` and `/api/shortlinkinfo/<id>`.

The website only compares functions named `@src` and `@tgt`. For two modules, each
function they share is merged into one module, renamed, and checked concurrently. The
target's attribute groups and metadata are renumbered so they can't collide with the
source's. A local `alive-tv` gets both files and pairs functions itself.

Dropped connections, DNS timeouts and gateway errors are retried twice with a short
backoff. Other HTTP errors are reported at once.

## Limits

Alive2 is bounded and intraprocedural. Loops are unrolled a few times and calls to
other functions are opaque, so `correct` is strong evidence rather than a proof.
Anything sent to the website is sent to a public server, and `create_link` stores it
permanently.

## Development

```sh
nix develop          # Go, gopls, golangci-lint, nixfmt
go test -race ./...  # unit tests against a fake Alive2 server
nix flake check      # build, tests, golangci-lint, formatting
```

CI runs `nix flake check` on Linux (x86-64 and ARM) and macOS. A weekly job checks the
live alive2.llvm.org API, since a change there is what would break this tool.

To release, push a `vX.Y.Z` tag on `main`. The Release workflow builds the archives and
images with GoReleaser, attests them, publishes the release with its Homebrew formula
(which [phongndo/homebrew-tap](https://github.com/phongndo/homebrew-tap) picks up), and
lists the version in the MCP Registry. CI runs the same build as a dry run on every push.

## License

MIT; see [LICENSE](LICENSE).
