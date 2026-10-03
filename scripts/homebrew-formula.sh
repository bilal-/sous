#!/bin/sh
# Print the Homebrew formula for a released version of sous, using the
# checksums published with that release.
#
#   scripts/homebrew-formula.sh v0.1.1 > Formula/sous.rb
set -eu
tag="${1:?usage: scripts/homebrew-formula.sh vX.Y.Z}"
version="${tag#v}"
repo="bilal-/sous"
sums="$(curl -fsSL "https://github.com/$repo/releases/download/$tag/checksums.txt")"
sha() { printf '%s\n' "$sums" | awk -v f="sous_${version}_$1.tar.gz" '$2 == f { print $1 }'; }
url() { echo "https://github.com/$repo/releases/download/$tag/sous_${version}_$1.tar.gz"; }
for p in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
  [ -n "$(sha "$p")" ] || { echo "no checksum for $p in $tag" >&2; exit 1; }
done

cat <<RUBY
class Sous < Formula
  desc "One list of what is waiting on you, across every project"
  homepage "https://github.com/$repo"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "$(url darwin_arm64)"
      sha256 "$(sha darwin_arm64)"
    else
      url "$(url darwin_amd64)"
      sha256 "$(sha darwin_amd64)"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "$(url linux_arm64)"
      sha256 "$(sha linux_arm64)"
    else
      url "$(url linux_amd64)"
      sha256 "$(sha linux_amd64)"
    end
  end

  def install
    bin.install "sous"
  end

  def caveats
    <<~EOS
      Run this once to add the session hooks, the agent skill and the shell snippet:
        sous setup
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/sous version")
  end
end
RUBY
