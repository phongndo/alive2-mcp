{
  lib,
  buildGoModule,
  version ? "dev",
}:

buildGoModule {
  pname = "alive2-mcp";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../.golangci.yml
      (lib.fileset.fileFilter (f: f.hasExt "go") ../.)
      ../internal/alive/testdata
    ];
  };

  vendorHash = "sha256-NbuvbsmNbz3aKdjyyfpkbi52yRhXl473j3LvAUDn3L8=";

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  # The tests stand up a fake Alive2 server on localhost.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "MCP server and CLI for checking LLVM IR transformations with Alive2";
    homepage = "https://github.com/phongndo/alive2-mcp";
    mainProgram = "alive2-mcp";
    platforms = lib.platforms.unix;
  };
}
