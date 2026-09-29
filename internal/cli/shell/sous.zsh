# Sourced from ~/.zshrc. Prints the cached sous board on a new interactive
# shell, at most once per refresh window. Never computes anything itself.
sous_ambient() {
  [[ -o interactive ]] || return 0
  command -v sous >/dev/null 2>&1 || return 0
  local home="${SOUS_HOME:-$HOME/.sous}" hours="${SOUS_REFRESH_HOURS:-4}" stamp now mtime
  stamp="$home/.zsh-stamp"
  mkdir -p "$home"
  now=$(date +%s)
  if [[ -f $stamp ]]; then
    mtime=$(stat -f %m "$stamp" 2>/dev/null || stat -c %Y "$stamp" 2>/dev/null || echo 0)
    (( now - mtime < hours * 3600 )) && return 0
  fi
  # exit 3 = no board yet (a refresh was started); don't stamp, so the next
  # shell prints it instead of waiting out the window.
  sous --cached 2>/dev/null && touch "$stamp"
}
sous_ambient

# `sous go X` should leave you in X after the agent exits. A child process
# cannot change this shell's directory, so the wrapper does the cd first.
sous() {
  if [[ "$1" == go && -n "$2" && "$2" != -* ]]; then
    local p
    p="$(command sous projects --path "$2")" || return $?
    cd "$p" && command sous "$@"
  else
    command sous "$@"
  fi
}
