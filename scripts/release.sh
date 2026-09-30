#!/bin/sh
# Deterministic build and metadata preparation only; never publishes/releases.
set -eu

usage() { echo "usage: $0 build VERSION GOOS GOARCH OUTDIR | assemble VERSION HTTPS_ORIGIN ARTIFACT_DIR OUTDIR" >&2; exit 2; }
fail() { echo "release: $*" >&2; exit 1; }
valid_version() { printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9.-]+)?$'; }
sha256() { shasum -a 256 "$1" | awk '{print tolower($1)}'; }

mode=${1:-}; shift || usage
case $mode in
  build)
    [ "$#" -eq 4 ] || usage
    version=$1 goos=$2 goarch=$3 outdir=$4
    valid_version "$version" || fail 'invalid semantic version'
    case "$goos/$goarch" in darwin/amd64|darwin/arm64|linux/amd64|linux/arm64|windows/amd64|windows/arm64) ;; *) fail 'unsupported GOOS/GOARCH pair' ;; esac
    command -v go >/dev/null 2>&1 || fail 'Go toolchain is required'
    mkdir -p "$outdir"
    outdir=$(CDPATH='' cd -- "$outdir" && pwd)
    name="anza-$goos-$goarch"; [ "$goos" != windows ] || name="$name.exe"
    root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
    (cd "$root" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false -ldflags='-buildid=' -o "$outdir/$name" ./cmd/anza)
    echo "$outdir/$name"
    ;;
  assemble)
    [ "$#" -eq 4 ] || usage
    version=$1 origin=$2 artifacts=$3 outdir=$4
    valid_version "$version" || fail 'invalid semantic version'
    command -v python3 >/dev/null 2>&1 || fail 'Python 3 is required'
    python3 - "$origin" <<'PYURL' || fail 'release origin must be a plain HTTPS origin without credentials, port, path, query, or fragment'
import sys
from urllib.parse import urlsplit
u=urlsplit(sys.argv[1])
if u.scheme!='https' or not u.hostname or u.username or u.password or u.port is not None or u.path not in ('','/') or u.query or u.fragment:
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
    python3 - "$root/go.mod" "$root/go.sum" "$outdir/$version/go-modules.json" "$outdir/$version/sbom.spdx.json" "$outdir/$version/dependency-licenses.txt" "$version" <<'PYMOD'
import hashlib,json,pathlib,re,sys
mod_path,sum_path,modules_path,sbom_path,licenses_path,release_version=sys.argv[1:]
requirements=[]; in_block=False
for line in pathlib.Path(mod_path).read_text().splitlines():
    stripped=line.strip()
    if stripped.startswith('require ('): in_block=True; continue
    if in_block and stripped==')': in_block=False; continue
    if stripped.startswith('require '): stripped=stripped[len('require '):]
    if stripped.startswith('//') or not stripped or stripped=='toolchain go1.27.1' or stripped.startswith('go '): continue
    parts=stripped.split()
    if len(parts)>=2 and parts[1].startswith('v'):
        requirements.append({'path':parts[0],'version':parts[1],'indirect':'// indirect' in stripped})
sums={}
for line in pathlib.Path(sum_path).read_text().splitlines():
    parts=line.split()
    if len(parts)==3 and not parts[1].endswith('/go.mod'):
        sums[(parts[0],parts[1])]=parts[2]
for row in requirements: row['go_sum']=sums.get((row['path'],row['version']),'NOASSERTION')
requirements.sort(key=lambda r:(r['path'],r['version']))
pathlib.Path(modules_path).write_text(json.dumps(requirements,sort_keys=True,indent=2)+'\n')
packages=[]
for i,m in enumerate(requirements,1):
    packages.append({'SPDXID':f'SPDXRef-Package-{i}','name':m['path'],'versionInfo':m['version'],'downloadLocation':'NOASSERTION','filesAnalyzed':False,'licenseConcluded':'NOASSERTION','licenseDeclared':'NOASSERTION','copyrightText':'NOASSERTION','externalRefs':[{'referenceCategory':'PACKAGE-MANAGER','referenceType':'purl','referenceLocator':f"pkg:golang/{m['path']}@{m['version']}"}]})
doc={'spdxVersion':'SPDX-2.3','dataLicense':'CC0-1.0','SPDXID':'SPDXRef-DOCUMENT','name':f'anza-{release_version}','documentNamespace':f'https://spdx.org/spdxdocs/anza-{release_version}','creationInfo':{'creators':['Tool: anza-release.sh'],'created':'2000-01-01T00:00:00Z'},'packages':packages}
pathlib.Path(sbom_path).write_text(json.dumps(doc,sort_keys=True,indent=2)+'\n')
licenses=['Anza dependency license inventory','', 'License expressions are reported as NOASSERTION until separately reviewed. This inventory is not a license grant or a substitute for required license texts.']
licenses.extend(f"{m['path']} {m['version']}: NOASSERTION" for m in requirements)
pathlib.Path(licenses_path).write_text('\n'.join(licenses)+'\n')
PYMOD
    python3 - "$outdir/$version" "$version" <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]); version=sys.argv[2]
manifests={}
for path in (root/'manifest.json',root/'windows/manifest.json'):
    manifests[str(path.relative_to(root))]=hashlib.sha256(path.read_bytes()).hexdigest()
metadata={'version':version,'payload_manifests':manifests,'signature_algorithm':'Ed25519','signature_file':'release-metadata.sig'}
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
