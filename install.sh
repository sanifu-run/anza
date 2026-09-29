#!/bin/sh
# Anza POSIX bootstrap. The reviewed release manifest digest is stamped by T9.4.
set -u

VERSION=${ANZA_VERSION:-0.1.0}
MANIFEST_SHA256=UNSTAMPED_BY_T9_4
BASE_URL=https://releases.anza.dev

fail() { printf '%s\n' "anza bootstrap: $*" >&2; exit 1; }
say() { printf '%s\n' "anza bootstrap: $*" >&2; }

# Test fixtures may use a local HTTP server and synthetic digest. This switch has
# no effect unless both explicit fixture values are supplied.
if [ "${ANZA_BOOTSTRAP_TESTING:-}" = 1 ]; then
  [ -n "${ANZA_TEST_BASE_URL:-}" ] && [ -n "${ANZA_TEST_MANIFEST_SHA256:-}" ] || fail 'incomplete test fixture configuration'
  BASE_URL=$ANZA_TEST_BASE_URL
  MANIFEST_SHA256=$ANZA_TEST_MANIFEST_SHA256
fi
[ "$MANIFEST_SHA256" != UNSTAMPED_BY_T9_4 ] || fail 'release manifest digest is not stamped; use a reviewed release build'

os=$(uname -s 2>/dev/null) || fail 'cannot detect operating system'
arch=$(uname -m 2>/dev/null) || fail 'cannot detect architecture'
case $os in Darwin) target_os=darwin ;; Linux) target_os=linux ;; *) fail "unsupported operating system: $os" ;; esac
case $arch in x86_64|amd64) target_arch=amd64 ;; arm64|aarch64) target_arch=arm64 ;; *) fail "unsupported architecture: $arch" ;; esac

if ! command -v curl >/dev/null 2>&1; then
  fail 'curl is required'
fi
if [ "${ANZA_BOOTSTRAP_TESTING:-}" = 1 ]; then
  fetch() { curl --fail --silent --show-error --location --proto '=http,https' --proto-redir '=http,https' "$1" -o "$2"; }
else
  fetch() { curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "$1" -o "$2"; }
fi
if command -v sha256sum >/dev/null 2>&1; then
  digest() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  digest() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  fail 'sha256sum or shasum is required'
fi

case $BASE_URL in https://*) ;; *) [ "${ANZA_BOOTSTRAP_TESTING:-}" = 1 ] || fail 'release URL must use HTTPS' ;; esac
tmp=${TMPDIR:-/tmp}/anza-bootstrap.$$
(umask 077 && mkdir "$tmp") || fail 'cannot create temporary directory'
trap 'rm -rf "$tmp"' 0
trap 'exit 130' HUP INT TERM
manifest=$tmp/manifest.json
artifact=$tmp/anza
manifest_url=$BASE_URL/$VERSION/manifest.json
fetch "$manifest_url" "$manifest" || fail 'manifest download failed'
actual=$(digest "$manifest") || fail 'cannot hash release manifest'
[ "$actual" = "$MANIFEST_SHA256" ] || fail 'release manifest digest mismatch'

# The manifest format is deliberately tiny and strict: one version plus one
# platform asset with a URL and lowercase SHA-256 digest.
manifest_version=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest" | head -n 1)
[ "$manifest_version" = "$VERSION" ] || fail 'manifest version mismatch'
asset_name=$target_os-$target_arch
asset_line=$(awk -v key="\"$asset_name\"" 'index($0,key) {print; exit}' "$manifest")
[ -n "$asset_line" ] || fail "release has no asset for $asset_name"
asset_url=$(printf '%s\n' "$asset_line" | sed -n 's/.*"url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
asset_hash=$(printf '%s\n' "$asset_line" | sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{64\}\)".*/\1/p')
[ -n "$asset_url" ] && [ -n "$asset_hash" ] || fail 'manifest asset entry is malformed'
case $asset_url in https://*) ;; *) [ "${ANZA_BOOTSTRAP_TESTING:-}" = 1 ] || fail 'asset URL must use HTTPS' ;; esac
fetch "$asset_url" "$artifact" || fail 'artifact download failed'
actual=$(digest "$artifact") || fail 'cannot hash release artifact'
[ "$actual" = "$asset_hash" ] || fail 'artifact digest mismatch'
[ -s "$artifact" ] || fail 'release artifact is empty'

home=${HOME:-}
[ -n "$home" ] || fail 'HOME is not set'
bindir=$home/.local/bin
mkdir -p "$bindir" || fail "cannot create $bindir"
dest=$bindir/anza
if [ -e "$dest" ] || [ -L "$dest" ]; then
  [ "${ANZA_UPDATE:-}" = 1 ] || fail "$dest already exists; preserve it and set ANZA_UPDATE=1 to replace it"
fi
stage=$bindir/.anza-stage.$$
(umask 077 && mkdir "$stage") || fail 'cannot create private staging directory'
install_tmp=$stage/anza
if ! cp "$artifact" "$install_tmp" || ! chmod 755 "$install_tmp"; then
  rm -rf "$stage"
  fail 'cannot stage verified artifact'
fi
mv -f "$install_tmp" "$dest" || { rm -rf "$stage"; fail 'cannot install verified artifact'; }
rmdir "$stage" || fail 'cannot remove private staging directory'
say "installed Anza $VERSION at $dest"
case :${PATH:-}: in *:$bindir:*) ;; *) say "add $bindir to PATH (for example: export PATH=\"\$HOME/.local/bin:\$PATH\")" ;; esac

# Never consume installer stdin. Only use the controlling terminal when the
# caller requested the default wizard and a terminal device is available.
if [ "$#" -eq 0 ] && ( : </dev/tty ) 2>/dev/null; then
  "$dest" </dev/tty || exit $?
else
  say "run '$dest' to continue"
fi
