#!/bin/sh
set -eu
case "${1:-}" in
  -h|--help) printf '%s\n' 'Usage: bin/install.sh [PREFIX]' 'Default: ~/.local. Builds and installs videodl, manual and completions.' 'No shell startup file is modified; configuration is created by videodl setup.'; exit 0 ;;
esac
[ "$#" -le 1 ] || { printf '%s\n' 'Usage: bin/install.sh [PREFIX]' >&2; exit 1; }
prefix=${1:-"$HOME/.local"}
case "$prefix" in /*) ;; *) printf '%s\n' 'PREFIX must be an absolute path.' >&2; exit 1 ;; esac
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=$(mktemp -d)
trap 'rm -rf -- "$build_dir"' EXIT HUP INT TERM
cd "$project_dir"
go build -trimpath -o "$build_dir/videodl" ./cmd/videodl
install -d "$prefix/bin" "$prefix/share/man/man1" "$prefix/share/bash-completion/completions" "$prefix/share/zsh/site-functions" "$prefix/share/fish/vendor_completions.d"
# Rename a new executable into place so a running old process is not truncated.
install -m 0755 "$build_dir/videodl" "$prefix/bin/.videodl-install"
mv -f "$prefix/bin/.videodl-install" "$prefix/bin/videodl"
install -m 0644 internal/cli/assets/videodl.1 "$prefix/share/man/man1/videodl.1"
"$build_dir/videodl" completion bash > "$build_dir/videodl.bash"
"$build_dir/videodl" completion zsh > "$build_dir/_videodl"
"$build_dir/videodl" completion fish > "$build_dir/videodl.fish"
install -m 0644 "$build_dir/videodl.bash" "$prefix/share/bash-completion/completions/videodl"
install -m 0644 "$build_dir/_videodl" "$prefix/share/zsh/site-functions/_videodl"
install -m 0644 "$build_dir/videodl.fish" "$prefix/share/fish/vendor_completions.d/videodl.fish"
printf '%s\n' "Installé : $prefix/bin/videodl" "Vérifiez que $prefix/bin est dans PATH, puis lancez videodl et videodl doctor." "Premier lancement uniquement : videodl setup --destination DIR" "Manuel : man -l $prefix/share/man/man1/videodl.1" 'Bash: source <(videodl completion bash)' 'Un service déjà actif utilise encore son ancien exécutable ; arrêtez-le avant une mise à jour.'
