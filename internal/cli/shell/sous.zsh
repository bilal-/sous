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
    # Find the project among the arguments, skipping -a/--agent and its value.
    local a p="" skip=0
    for a in "${@:2}"; do
      if (( skip )); then skip=0; continue; fi
      case "$a" in
        -a|--agent) skip=1 ;;
        -*) ;;
        *) p="$a"; break ;;
      esac
    done
    if [[ -n "$p" && "$p" != . ]]; then
      p="$(command sous projects --path "$p")" || return $?
      cd "$p" || return $?
    fi
  fi
  command sous "$@"
}
