#!/usr/bin/env bash
# Writes the Homebrew formula for a release from GoReleaser's checksums.txt.
# phongndo/homebrew-tap's updater copies it from the release into the tap.
#
#   scripts/formula.sh v0.1.0 dist/checksums.txt > dist/alive2-mcp.rb
set -euo pipefail

tag=$1
checksums=$2
version=${tag#v}
base="https://github.com/phongndo/alive2-mcp/releases/download/$tag"

source_for() {
  local asset="alive2-mcp_${version}_$1.tar.gz" sum
  sum=$(awk -v asset="$asset" '$2 == asset { print $1 }' "$checksums")
  if [[ ! $sum =~ ^[0-9a-f]{64}$ ]]; then
    echo "no checksum for $asset in $checksums" >&2
    exit 1
  fi
  printf '      url "%s/%s"\n      sha256 "%s"\n' "$base" "$asset" "$sum"
}

# Assigned first: under set -e a failed lookup stops the script here, which it
# wouldn't inside the heredoc.
mac_arm=$(source_for darwin_arm64)
mac_intel=$(source_for darwin_amd64)
linux_arm=$(source_for linux_arm64)
linux_intel=$(source_for linux_amd64)

cat <<EOF
class Alive2Mcp < Formula
  desc "MCP server and CLI for checking LLVM IR transformations with Alive2"
  homepage "https://github.com/phongndo/alive2-mcp"
  version "$version"
  license "MIT"

  on_macos do
    on_arm do
$mac_arm
    end

    on_intel do
$mac_intel
    end
  end

  on_linux do
    on_arm do
$linux_arm
    end

    on_intel do
$linux_intel
    end
  end

  def install
    bin.install "alive2-mcp"
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/alive2-mcp -version").strip
  end
end
EOF
