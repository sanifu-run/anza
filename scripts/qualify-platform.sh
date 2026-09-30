#!/bin/sh
# Read-only evidence helper for a human-run native platform qualification.
# This script never invokes Anza, a vendor installer, or the network. It only
# hashes explicitly supplied files and writes a TSV snapshot under --evidence-dir.
set -eu

usage() {
  cat >&2 <<'EOF'
Usage:
  qualify-platform.sh --phase before|after --target ID --evidence-dir DIR \
    [--owned LABEL=FILE]... [--unowned LABEL=FILE]...

Target IDs are documented in docs/evidence/platform-qualification.md.
Run once before and once after each scenario, using the same labels. The helper
does not run installers, change PATH/configuration, contact the network, or
remove files. Store evidence only in a disposable native/VM account.
EOF
  exit 2
}

phase=
target=
evidence_dir=
owned_pairs=
unowned_pairs=
seen_labels=' '
validate_pair() {
  pair=$1
  label=${pair%%=*}
  path=${pair#*=}
  case $label in ''|*[!A-Za-z0-9._-]*) echo "invalid label: $label" >&2; exit 2 ;; esac
  [ "$path" != "$pair" ] && [ -n "$path" ] || { echo "expected LABEL=FILE: $pair" >&2; exit 2; }
  case "$seen_labels" in *" $label "*) echo "duplicate label: $label" >&2; exit 2 ;; esac
  seen_labels="$seen_labels$label "
}
while [ "$#" -gt 0 ]; do
  case $1 in
    --phase) [ "$#" -ge 2 ] || usage; phase=$2; shift 2 ;;
    --target) [ "$#" -ge 2 ] || usage; target=$2; shift 2 ;;
    --evidence-dir) [ "$#" -ge 2 ] || usage; evidence_dir=$2; shift 2 ;;
    --owned) [ "$#" -ge 2 ] || usage; pair=$2; validate_pair "$pair"; owned_pairs="${owned_pairs-}
$pair"; shift 2 ;;
    --unowned) [ "$#" -ge 2 ] || usage; pair=$2; validate_pair "$pair"; unowned_pairs="${unowned_pairs-}
$pair"; shift 2 ;;
    *) usage ;;
  esac
done

case $phase in before|after) ;; *) usage ;; esac
[ -n "$target" ] && [ -n "$evidence_dir" ] || usage
case $target in ''|*[!A-Za-z0-9._-]*|[!A-Za-z0-9]*) echo 'target must be a simple ID' >&2; exit 2 ;; esac
umask 077
evidence_check=$evidence_dir
case $evidence_check in */) evidence_check=${evidence_check%/}; [ -n "$evidence_check" ] || evidence_check=/ ;; esac
[ ! -L "$evidence_check" ] || { echo 'evidence directory must not be a symlink' >&2; exit 2; }
mkdir -p "$evidence_dir"
evidence_dir=$(CDPATH='' cd -- "$evidence_dir" && pwd)
output=$evidence_dir/$target-$phase.tsv
[ ! -e "$output" ] && [ ! -L "$output" ] || { echo "refusing to overwrite $target-$phase.tsv" >&2; exit 2; }

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else echo 'need sha256sum or shasum' >&2; exit 2
  fi
}

record_pair() {
  kind=$1
  pair=$2
  label=${pair%%=*}
  path=${pair#*=}
  case $label in ''|*[!A-Za-z0-9._-]*|[!A-Za-z0-9]*) echo "invalid label: $label" >&2; exit 2 ;; esac
  [ "$path" != "$pair" ] && [ -n "$path" ] || { echo "expected LABEL=FILE: $pair" >&2; exit 2; }
  if [ -L "$path" ]; then
    state='symlink'
    digest='-'
  elif [ -f "$path" ]; then
    state='file'
    digest=$(hash_file "$path")
  elif [ -e "$path" ]; then
    state='other'
    digest='-'
  else
    state='absent'
    digest='-'
  fi
  printf '%s\t%s\t%s\t%s\n' "$kind" "$label" "$state" "$digest" >>"$output"
}

printf 'kind\tlabel\tstate\tsha256\n' >"$output"
printf '%s\n' "${owned_pairs-}" | while IFS= read -r pair; do [ -n "$pair" ] && record_pair owned "$pair"; done
printf '%s\n' "${unowned_pairs-}" | while IFS= read -r pair; do [ -n "$pair" ] && record_pair unowned "$pair"; done
printf 'Wrote %s (hashes only; no file paths recorded).\n' "$output"
