#!/bin/sh
# Deterministic build and metadata preparation only; never publishes/releases.
set -eu

usage() { echo "usage: $0 build VERSION GOOS GOARCH OUTDIR | assemble VERSION HTTPS_ORIGIN ARTIFACT_DIR OUTDIR" >&2; exit 2; }
fail() { echo "release: $*" >&2; exit 1; }
valid_version() { printf '%s' "$1" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; }
sha256() { shasum -a 256 "$1" | awk '{print tolower($1)}'; }

mode=${1:-}; shift || usage
case $mode in
  build)
    [ "$#" -eq 4 ] || usage
    version=$1 goos=$2 goarch=$3 outdir=$4
    valid_version "$version" || fail 'invalid semantic version'
    command -v python3 >/dev/null 2>&1 || fail 'Python 3 is required'
    [ -n "${ANZA_RELEASE_PUBLIC_KEY_BASE64:-}" ] || fail 'ANZA_RELEASE_PUBLIC_KEY_BASE64 must contain the pinned Ed25519 public key'
    python3 - "$ANZA_RELEASE_PUBLIC_KEY_BASE64" <<'PYKEY' || fail 'ANZA_RELEASE_PUBLIC_KEY_BASE64 must be canonical base64 for a 32-byte Ed25519 public key'
import base64, sys
try:
    key=base64.b64decode(sys.argv[1],validate=True)
except Exception:
    raise SystemExit(1)
if len(key)!=32 or base64.b64encode(key).decode()!=sys.argv[1]: raise SystemExit(1)
PYKEY
    case "$goos/$goarch" in darwin/amd64|darwin/arm64|linux/amd64|linux/arm64|windows/amd64|windows/arm64) ;; *) fail 'unsupported GOOS/GOARCH pair' ;; esac
    command -v go >/dev/null 2>&1 || fail 'Go toolchain is required'
    root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
    if [ "$goos" = darwin ]; then
      host_os=$(cd "$root" && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go env GOHOSTOS) || fail 'Go host platform could not be determined'
      host_arch=$(cd "$root" && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go env GOHOSTARCH) || fail 'Go host platform could not be determined'
      [ "$host_os/$host_arch" = "$goos/$goarch" ] || fail 'Darwin releases must be built natively on the matching macOS architecture'
      command -v clang >/dev/null 2>&1 || fail 'Xcode clang is required for the native macOS credential backend'
    fi
    mkdir -p "$outdir"
    outdir=$(CDPATH='' cd -- "$outdir" && pwd)
    name="anza-$goos-$goarch"; [ "$goos" != windows ] || name="$name.exe"
    if [ "$goos" = darwin ]; then
      (cd "$root" && CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" go build -tags 'keyring_no1password,keyring_noprotonpass,keyring_nofile,keyring_nopass' -trimpath -buildvcs=false -ldflags="-buildid= -X main.version=$version -X main.releasePublicKeyBase64=$ANZA_RELEASE_PUBLIC_KEY_BASE64" -o "$outdir/$name" ./cmd/anza)
    else
      (cd "$root" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -tags 'keyring_no1password,keyring_noprotonpass,keyring_nofile,keyring_nopass' -trimpath -buildvcs=false -ldflags="-buildid= -X main.version=$version -X main.releasePublicKeyBase64=$ANZA_RELEASE_PUBLIC_KEY_BASE64" -o "$outdir/$name" ./cmd/anza)
    fi
    echo "$outdir/$name"
    ;;
  assemble)
    [ "$#" -eq 4 ] || usage
    version=$1 origin=$2 artifacts=$3 outdir=$4
    valid_version "$version" || fail 'invalid semantic version'
    command -v python3 >/dev/null 2>&1 || fail 'Python 3 is required'
    python3 - "$origin" <<'PYURL' || fail 'release origin must be a plain HTTPS origin without credentials, port, path, query, or fragment'
import re, sys
from urllib.parse import urlsplit
try:
    u=urlsplit(sys.argv[1])
    host=u.hostname or ''
    port=u.port
    host.encode('ascii')
except (UnicodeError, ValueError):
    raise SystemExit(1)
labels=host.split('.')
label_pattern=re.compile(r'^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$')
valid_host=(len(host)<=253 and len(labels)>=2 and all(label_pattern.fullmatch(label) for label in labels)
            and not labels[-1].isdigit())
if (u.scheme!='https' or not valid_host or u.username or u.password or port is not None
        or u.path or u.query or u.fragment):
    raise SystemExit(1)
PYURL
    [ -d "$artifacts" ] || fail 'artifact directory is missing'
    artifacts=$(CDPATH='' cd -- "$artifacts" && pwd)
    mkdir -p "$outdir"
    outdir=$(CDPATH='' cd -- "$outdir" && pwd)
    command -v openssl >/dev/null 2>&1 || fail 'OpenSSL is required for detached Ed25519 signing'
    [ -n "${ANZA_ED25519_PRIVATE_KEY_FILE:-}" ] && [ -r "$ANZA_ED25519_PRIVATE_KEY_FILE" ] || fail 'ANZA_ED25519_PRIVATE_KEY_FILE must name a readable protected secret key'
    root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
    mkdir -p "$outdir/$version/windows" "$outdir/$version/scripts"
    python3 - "$version" "$origin" "$artifacts" "$outdir" "$root" <<'PY'
import hashlib, json, pathlib, sys
version, origin, artifacts, output, root = sys.argv[1:]
artifacts, output, root = pathlib.Path(artifacts), pathlib.Path(output), pathlib.Path(root)
sha=lambda b: hashlib.sha256(b).hexdigest()
posix_assets={}
windows_assets={}
for platform in ('darwin','linux','windows'):
    for arch in ('amd64','arm64'):
        name=f'anza-{platform}-{arch}'+('.exe' if platform=='windows' else '')
        data=(artifacts/name).read_bytes()
        (output/version/name).write_bytes(data)
        key=arch if platform=='windows' else f'{platform}-{arch}'
        entry={'url': f'{origin}/anza/{version}/{name}', 'sha256':sha(data)}
        (windows_assets if platform=='windows' else posix_assets)[key]=entry
(output/version/'manifest.json').write_text(json.dumps({'version':version,'assets':posix_assets},sort_keys=True,indent=2)+'\n')
(output/version/'windows'/'manifest.json').write_text(json.dumps({'version':version,'assets':windows_assets},sort_keys=True,indent=2)+'\n')
for source, name in ((root/'install.sh','install.sh'),(root/'install.ps1','install.ps1')):
    data=source.read_text()
    manifest=(output/version/('windows/manifest.json' if name.endswith('.ps1') else 'manifest.json')).read_bytes()
    digest=sha(manifest)
    if name=='install.sh':
        data=data.replace('UNSTAMPED_BY_T9_4',digest).replace('VERSION=${ANZA_VERSION:-0.1.0}',f'VERSION=${{ANZA_VERSION:-{version}}}').replace('BASE_URL=https://releases.anza.dev',f'BASE_URL={origin}')
    else:
        data=data.replace('__ANZA_VERSION__',version).replace('__T9_4_STAMP_MANIFEST_SHA256__',digest).replace('__T9_4_STAMP_REVIEWED_RELEASE_ORIGIN__',origin)
    if 'UNSTAMPED_BY_T9_4' in data or '__T9_4_' in data or '__ANZA_VERSION__' in data:
        raise SystemExit(f'{name}: unstamped marker remains')
    dest=output/version/'scripts'/name
    dest.write_text(data)
    if name=='install.sh': dest.chmod(0o755)
    print(f'{name} sha256 {sha(dest.read_bytes())}')
PY
    root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
    command -v go >/dev/null 2>&1 || fail 'Go toolchain is required to enumerate offline module licenses'
    module_cache=$(cd "$root" && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go env GOMODCACHE 2>/dev/null) || fail 'Go module cache could not be located'
    [ -n "$module_cache" ] || fail 'Go module cache could not be located'
    python3 "$root/scripts/license-bundle.py" --root "$root" --module-cache "$module_cache" --outdir "$outdir/$version"
python3 - "$outdir/$version" "$version" "$root" <<'PY'
import hashlib,json,pathlib,re,subprocess,sys
root=pathlib.Path(sys.argv[1]); version=sys.argv[2]; repo=pathlib.Path(sys.argv[3])
manifests={}
for path in (root/'manifest.json',root/'windows/manifest.json'):
    manifests[str(path.relative_to(root))]=hashlib.sha256(path.read_bytes()).hexdigest()
manifest_set=json.dumps(manifests,sort_keys=True,separators=(',',':')).encode()
catalog_script=repo/'scripts/catalog-manifest.py'
subprocess.run([sys.executable,str(catalog_script),'--check'],cwd=repo,check=True,stdout=subprocess.DEVNULL)
setup_catalog=json.loads(subprocess.check_output([sys.executable,str(catalog_script),'--setup-catalog'],cwd=repo))
source_manifest=json.loads((repo/'catalog/manifest.json').read_text(encoding='utf-8'))
catalog_version=setup_catalog.get('catalog_version')
catalog_digest=setup_catalog.get('catalog_digest')
if not isinstance(catalog_version,str) or not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)',catalog_version) or catalog_version!=source_manifest.get('version'):
    raise SystemExit('setup catalog version is empty, invalid, or differs from the checked source manifest')
if not isinstance(catalog_digest,str) or not re.fullmatch(r'[0-9a-f]{64}',catalog_digest):
    raise SystemExit('setup catalog digest is empty or invalid')
metadata={'version':version,'catalog_version':catalog_version,'catalog_digest':catalog_digest,'artifact_digest':'sha256:'+hashlib.sha256(manifest_set).hexdigest(),'protocol_min':1,'protocol_max':1,'payload_manifests':manifests,'signature_algorithm':'Ed25519','signature_file':'release-metadata.sig'}
(root/'release-metadata.json').write_text(json.dumps(metadata,sort_keys=True,indent=2)+'\n')
PY
    openssl pkeyutl -sign -rawin -inkey "$ANZA_ED25519_PRIVATE_KEY_FILE" -in "$outdir/$version/release-metadata.json" -out "$outdir/$version/release-metadata.sig"
    openssl pkeyutl -verify -rawin -pubin -inkey "${ANZA_ED25519_PUBLIC_KEY_FILE:-/dev/null}" -in "$outdir/$version/release-metadata.json" -sigfile "$outdir/$version/release-metadata.sig" >/dev/null || fail 'signature verification failed; supply matching ANZA_ED25519_PUBLIC_KEY_FILE'
    sums_tmp="$outdir/$version.SHA256SUMS.tmp"
    (cd "$outdir/$version" && find . -type f ! -name SHA256SUMS ! -name SHA256SUMS.sig -print | LC_ALL=C sort | while IFS= read -r file; do shasum -a 256 "$file"; done > "$sums_tmp")
    mv "$sums_tmp" "$outdir/$version/SHA256SUMS"
    ;;
  *) usage ;;
esac
