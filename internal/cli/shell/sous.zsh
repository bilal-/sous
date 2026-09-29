# Sourced from ~/.zshrc. Prints the saved sous board in a new interactive
# shell, at most once per refresh_hours (set in ~/.sous/config.toml). Never
# scans anything itself.
if [[ -o interactive ]] && command -v sous >/dev/null 2>&1; then
  sous --ambient 2>/dev/null
fi

# `sous go X` should leave you in X after the agent exits. A child process
# cannot change this shell's directory, so the wrapper does the cd first.
sous() {
  if [[ "$1" == go ]]; then
    local p
    p="$(command sous go --where "${@:2}" 2>/dev/null)" && cd "$p"
  fi
  command sous "$@"
}
