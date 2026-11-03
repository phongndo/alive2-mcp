{
  description = "MCP server and CLI for checking LLVM IR transformations with Alive2";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;
      # Linux and macOS. nixpkgs no longer supports x86_64-darwin (Intel Macs);
      # those can still `go install` it.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # Releases tag the commit that sets VERSION, so Nix builds of a release
      # report it the way the release binaries do.
      version = lib.fileContents ./VERSION;
    in
    {
      overlays.default = final: _: {
        alive2-mcp = final.callPackage ./nix/package.nix { inherit version; };
      };

      packages = forAllSystems (pkgs: rec {
        alive2-mcp = pkgs.callPackage ./nix/package.nix { inherit version; };
        default = alive2-mcp;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          inputsFrom = [ self.packages.${pkgs.stdenv.hostPlatform.system}.default ];
          packages = with pkgs; [
            golangci-lint
            gopls
            nixfmt
          ];
        };
      });

      checks = forAllSystems (
        pkgs:
        let
          package = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        in
        {
          # Builds and runs `go test`.
          inherit package;

          # golangci-lint against the vendored modules the package build uses.
          lint = package.overrideAttrs (old: {
            pname = "alive2-mcp-lint";
            nativeBuildInputs = old.nativeBuildInputs ++ [ pkgs.golangci-lint ];
            buildPhase = ''
              runHook preBuild
              export HOME=$TMPDIR GOLANGCI_LINT_CACHE=$TMPDIR/golangci-lint
              golangci-lint run ./...
              runHook postBuild
            '';
            doCheck = false;
            installPhase = "touch $out";
          });

          format =
            pkgs.runCommand "alive2-mcp-format"
              {
                nativeBuildInputs = [
                  pkgs.go
                  pkgs.nixfmt
                ];
              }
              ''
                cd ${self}
                unformatted=$(gofmt -l .)
                if [ -n "$unformatted" ]; then
                  echo "gofmt needed: $unformatted"
                  exit 1
                fi
                nixfmt --check flake.nix nix/*.nix
                touch $out
              '';
        }
      );

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
