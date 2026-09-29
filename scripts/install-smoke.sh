#!/bin/sh
# Install a released sous the way a person does, in a throwaway home, and
# check that it works: install.sh, sous setup, a second setup that changes
# nothing, and sous doctor with no problems. The real home is never touched.
#
#   scripts/install-smoke.sh v0.2.2 [zsh|bash|fish]
set -eu
tag="${1:?usage: scripts/install-smoke.sh vX.Y.Z [zsh|bash|fish]}"
shell="${2:-zsh}"
here="$(cd "$(dirname "$0")/.." && pwd)"

HOME="$(mktemp -d)"
trap 'rm -rf "$HOME"' EXIT
export HOME SHELL="/bin/$shell" SOUS_VERSION="$tag"
# Nothing from the real setup: sous, shell and tracker settings.
unset SOUS_HOME ZDOTDIR XDG_CONFIG_HOME GH_TOKEN GITHUB_TOKEN GH_HOST GH_CONFIG_DIR \
  GITLAB_HOST GITLAB_TOKEN GITLAB_URI GLAB_CONFIG_DIR 2>/dev/null || true
PATH="$HOME/.local/bin:$PATH"
export PATH
fail() { echo "install-smoke: $*" >&2; exit 1; }

# One project, so setup has something to find.
mkdir -p "$HOME/code/acme/api"
git -C "$HOME/code/acme/api" init -q
git -C "$HOME/code/acme/api" remote add origin https://github.com/acme/api.git

sh "$here/install.sh"
[ "$(sous version)" = "sous ${tag#v}" ] || fail "installed $(sous version), wanted $tag"

# Setup again: safe to repeat, so nothing changes.
files="$HOME/.claude/settings.json $HOME/.codex/hooks.json $HOME/.sous/config.toml"
case "$shell" in
  zsh) files="$files $HOME/.zshrc" ;;
  bash) files="$files $HOME/.bashrc"
    [ -f "$HOME/.bash_profile" ] && files="$files $HOME/.bash_profile" ;;
  fish) files="$files $HOME/.config/fish/conf.d/sous.fish" ;;
esac
# shellcheck disable=SC2086
before="$(cat $files | cksum)"
sous setup >/dev/null
# shellcheck disable=SC2086
[ "$(cat $files | cksum)" = "$before" ] || fail "a second sous setup changed files"

sous --refresh >/dev/null 2>&1 || true
sous doctor || fail "sous doctor found problems"
sous doctor --json | grep -q '"problems": 0' || fail "sous doctor --json disagrees"
echo "install-smoke: $tag works with $shell"
