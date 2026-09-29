#!/bin/sh
set -eu
root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/anza-posix-fixture.XXXXXX")
server_pid=
cleanup() { [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || :; rm -rf "$tmp"; }
trap cleanup EXIT HUP INT TERM
mkdir -p "$tmp/tools" "$tmp/home with spaces"
python=${PYTHON:-python3}
cat >"$tmp/server.py" <<'PY'
import hashlib, http.server, json, os, socketserver
import ssl, threading
payload = b'#!/bin/sh\nprintf "called\\n" >> "$ANZA_CALL_LOG"\n'
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        host = self.headers.get('Host')
        case = open(os.environ['CASE_FILE']).read() if os.path.exists(os.environ['CASE_FILE']) else ''
        if self.path.endswith('/manifest.json'):
            body = json.dumps({'version':'0.1.0','assets':{
                'darwin-amd64':{'url':f'http://{host}/artifact','sha256':hashlib.sha256(payload).hexdigest()},
                'darwin-arm64':{'url':f'http://{host}/artifact','sha256':hashlib.sha256(payload).hexdigest()},
                'linux-amd64':{'url':f'http://{host}/artifact','sha256':hashlib.sha256(payload).hexdigest()},
                'linux-arm64':{'url':f'http://{host}/artifact','sha256':hashlib.sha256(payload).hexdigest()}}}).encode()
            self.send_response(200); self.send_header('Content-Length',str(len(body))); self.end_headers(); self.wfile.write(body)
        elif self.path == '/downgrade':
            self.send_response(302); self.send_header('Location',f"http://127.0.0.1:{os.environ['HTTP_PORT']}/artifact"); self.end_headers()
        elif self.path == '/artifact':
            body = b'#!/bin/sh\nexit 9\n' if case=='bad-artifact' else payload
            self.send_response(200); self.send_header('Content-Length',str(len(body)+50 if case=='interrupted' else len(body))); self.end_headers()
            self.wfile.write(body); self.wfile.flush()
            if case=='interrupted': self.close_connection = True
        else: self.send_error(404)
    def log_message(self,*args): pass
class Server(socketserver.TCPServer): allow_reuse_address=True
with Server(('127.0.0.1',0),Handler) as server:
    os.environ['HTTP_PORT']=str(server.server_address[1])
    context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(os.environ['TLS_CERT'],os.environ['TLS_KEY'])
    with Server(('127.0.0.1',0),Handler) as tls_server:
        tls_server.socket=context.wrap_socket(tls_server.socket,server_side=True)
        open(os.environ['PORT_FILE'],'w').write(str(server.server_address[1]))
        open(os.environ['TLS_PORT_FILE'],'w').write(str(tls_server.server_address[1]))
        threading.Thread(target=tls_server.serve_forever,daemon=True).start()
        server.serve_forever()
PY

portfile=$tmp/port
tlsportfile=$tmp/tlsport
casefile=$tmp/case
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$tmp/tls.key" -out "$tmp/tls.crt" -days 1 -subj '/CN=localhost' -addext 'subjectAltName=DNS:localhost' >/dev/null 2>&1
PORT_FILE=$portfile TLS_PORT_FILE=$tlsportfile CASE_FILE=$casefile TLS_CERT=$tmp/tls.crt TLS_KEY=$tmp/tls.key "$python" "$tmp/server.py" >/dev/null 2>&1 & server_pid=$!
i=0; while [ ! -s "$portfile" ] || [ ! -s "$tlsportfile" ]; do i=$((i+1)); [ "$i" -lt 100 ] || { echo 'fixture server did not start' >&2; exit 1; }; sleep 0.05; done
base=http://127.0.0.1:$(cat "$portfile")

# Build a controlled PATH. Missing-tool cases omit exactly one required tool.
for tool in sh uname sed awk head mkdir rmdir rm cp chmod mv curl sha256sum shasum; do
  case $tool in uname) continue ;; esac
  found=$(command -v "$tool" || true)
  [ -z "$found" ] || ln -s "$found" "$tmp/tools/$tool"
done
cat >"$tmp/tools/uname" <<'EOF'
#!/bin/sh
case $1 in
  -s) printf '%s\n' "${FIXTURE_OS:-Linux}" ;;
  -m) printf '%s\n' "${FIXTURE_ARCH:-x86_64}" ;;
esac
EOF
chmod +x "$tmp/tools/uname"
cat >"$tmp/tools/wget" <<EOF
#!/bin/sh
printf used >"$tmp/wget-used"
exit 0
EOF
chmod +x "$tmp/tools/wget"
hash=$(curl -fsS "$base/manifest.json" | shasum -a 256 | awk '{print $1}')
calls=$tmp/calls
run() {
  printf '%s' "${FIXTURE_CASE:-}" >"$casefile"
  env PATH="$tmp/tools" HOME="$tmp/home with spaces" ANZA_VERSION=0.1.0 \
    ANZA_BOOTSTRAP_TESTING=1 ANZA_TEST_BASE_URL="$base" ANZA_TEST_MANIFEST_SHA256="${ANZA_TEST_MANIFEST_SHA256:-$hash}" \
    ANZA_CALL_LOG="$calls" FIXTURE_OS="${FIXTURE_OS:-Linux}" FIXTURE_ARCH="${FIXTURE_ARCH:-x86_64}" \
    FIXTURE_CASE="${FIXTURE_CASE:-}" /bin/sh "$root/install.sh" </dev/null
}
pass=0
check_ok() { label=$1; shift; if ! "$@" >"$tmp/run.out" 2>&1; then echo "FAIL: $label" >&2; cat "$tmp/run.out" >&2; exit 1; fi; pass=$((pass+1)); }
check_fail() { label=$1; shift; if "$@" >"$tmp/run.out" 2>&1; then echo "FAIL: $label unexpectedly passed" >&2; exit 1; fi; pass=$((pass+1)); }

# macOS/Linux x64 and arm64 mappings.
for platform in 'Darwin x86_64' 'Darwin arm64' 'Linux x86_64' 'Linux aarch64'; do
  # Intentional splitting maps the two words into their fixture fields.
  # shellcheck disable=SC2086
  set -- $platform; FIXTURE_OS=$1; FIXTURE_ARCH=$2; export FIXTURE_OS FIXTURE_ARCH
  rm -f "$tmp/home with spaces/.local/bin/anza"
  check_ok "supported $platform" run
done
FIXTURE_OS=FreeBSD; FIXTURE_ARCH=x86_64; export FIXTURE_OS FIXTURE_ARCH
check_fail unsupported_os run
FIXTURE_OS=Linux; FIXTURE_ARCH=mips; export FIXTURE_OS FIXTURE_ARCH
check_fail unsupported_arch run
FIXTURE_ARCH=x86_64; export FIXTURE_ARCH

rm -f "$tmp/home with spaces/.local/bin/anza"
ANZA_TEST_MANIFEST_SHA256=$(printf '%064d' 0); export ANZA_TEST_MANIFEST_SHA256
check_fail bad_manifest_digest run
ANZA_TEST_MANIFEST_SHA256=$hash; export ANZA_TEST_MANIFEST_SHA256
FIXTURE_CASE=interrupted; export FIXTURE_CASE
rm -f "$tmp/home with spaces/.local/bin/anza"
check_fail interrupted_download run
FIXTURE_CASE=; export FIXTURE_CASE

FIXTURE_CASE=bad-artifact; export FIXTURE_CASE
rm -f "$tmp/home with spaces/.local/bin/anza"
check_fail bad_artifact_digest run
FIXTURE_CASE=; export FIXTURE_CASE

# Preserve an existing binary on default install and on failed artifact hash.
dest="$tmp/home with spaces/.local/bin/anza"
mkdir -p "$(dirname "$dest")"; printf '#!/bin/sh\nexit 77\n' >"$dest"; chmod +x "$dest"
check_fail preserve_existing run
[ "$(head -n 1 "$dest")" = "#!/bin/sh" ] || { echo 'FAIL: existing binary changed' >&2; exit 1; }
pass=$((pass+1))
rm -f "$dest"

# Missing fetch and digest tools fail before downloads. Keep core commands.
mv "$tmp/tools/curl" "$tmp/tools/curl.disabled"
check_fail missing_fetch_tool run
[ ! -e "$tmp/wget-used" ] || { echo 'FAIL: installer fell back to wget' >&2; exit 1; }
mv "$tmp/tools/curl.disabled" "$tmp/tools/curl"
mv "$tmp/tools/shasum" "$tmp/tools/shasum.disabled"
mv "$tmp/tools/sha256sum" "$tmp/tools/sha256sum.disabled"
check_fail missing_hash_tool run
mv "$tmp/tools/shasum.disabled" "$tmp/tools/shasum"
mv "$tmp/tools/sha256sum.disabled" "$tmp/tools/sha256sum"

# Exercise curl's production redirect policy against a trusted local HTTPS
# endpoint that redirects to HTTP. The downgrade must be rejected.
tlsport=$(cat "$tlsportfile")
if curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
  --cacert "$tmp/tls.crt" "https://localhost:$tlsport/downgrade" -o "$tmp/downgrade.out" 2>"$tmp/downgrade.err"; then
  echo 'FAIL: HTTPS-to-HTTP redirect was followed' >&2; exit 1
fi
case $(cat "$tmp/downgrade.err") in *redirect*|*protocol*) pass=$((pass+1)) ;; *) echo 'FAIL: curl downgrade rejection unclear' >&2; cat "$tmp/downgrade.err" >&2; exit 1 ;; esac

# Verified artifact installs to the spaced path. A piped run must not invoke
# the wizard; a controlling-tty run must invoke it exactly once.
rm -f "$dest" "$calls"
check_ok verified_install run
[ -x "$dest" ] || { echo 'FAIL: installed executable missing' >&2; exit 1; }
[ ! -s "$calls" ] || { echo 'FAIL: noninteractive installer invoked wizard' >&2; exit 1; }
pass=$((pass+1))
if command -v script >/dev/null 2>&1; then
  rm -f "$calls" "$dest"
  script_cmd=$(command -v script)
  env PATH="$tmp/tools" HOME="$tmp/home with spaces" ANZA_VERSION=0.1.0 \
    ANZA_BOOTSTRAP_TESTING=1 ANZA_TEST_BASE_URL="$base" ANZA_TEST_MANIFEST_SHA256="$hash" \
    ANZA_CALL_LOG="$calls" FIXTURE_OS=Linux FIXTURE_ARCH=x86_64 \
    "$script_cmd" -q /dev/null /bin/sh "$root/install.sh" >"$tmp/tty.out" 2>&1 || { echo 'FAIL: tty bootstrap' >&2; cat "$tmp/tty.out" >&2; exit 1; }
  [ "$(wc -l <"$calls" | tr -d ' ')" = 1 ] || { echo 'FAIL: verified tty run did not invoke exactly once' >&2; exit 1; }
  pass=$((pass+1))
fi
printf 'bootstrap-posix: %s fixture checks passed\n' "$pass"
