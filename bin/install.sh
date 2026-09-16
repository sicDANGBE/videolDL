#!/bin/sh
set -eu
case "${1:-}" in
  -h|--help)
    printf '%s\n' 'Usage : bin/install.sh [PREFIX]' \
      'Préfixe par défaut : ~/.local. Compilation, installation ou mise à jour.' \
      'Les services actifs de cette installation sont arrêtés proprement puis relancés.' \
      'Configuration et file conservées. Aucun service arrêté ne sera démarré.' \
      'Terminez les workers au premier plan avant la mise à jour.'
    exit 0 ;;
esac
[ "$#" -le 1 ] || { printf '%s\n' 'Usage : bin/install.sh [PREFIX]' >&2; exit 1; }
prefix=${1:-"$HOME/.local"}
case "$prefix" in /*) ;; *) printf '%s\n' 'PREFIX doit être un chemin absolu.' >&2; exit 1 ;; esac
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=$(mktemp -d)
install_pid=
trap 'rm -rf -- "$build_dir"' EXIT
interrupt_install() {
  trap '' HUP INT TERM
  if [ -n "$install_pid" ]; then
    kill -TERM "$install_pid" 2>/dev/null || :
    wait "$install_pid" 2>/dev/null || :
  fi
  exit "$1"
}
trap 'interrupt_install 129' HUP
trap 'interrupt_install 130' INT
trap 'interrupt_install 143' TERM
run_child() {
  "$@" &
  install_pid=$!
  if wait "$install_pid"; then result=0; else result=$?; fi
  install_pid=
  return "$result"
}
cd "$project_dir"
printf '%s\n' 'Préparation de videodl et de l’installateur…'
run_child go build -trimpath -o "$build_dir/videodl" ./cmd/videodl
run_child go build -trimpath -o "$build_dir/videodl-install" ./cmd/videodl-install
run_child "$build_dir/videodl-install" --prefix "$prefix" --binary "$build_dir/videodl" --manual "$project_dir/internal/cli/assets/videodl.1"
