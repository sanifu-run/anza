package acceptance

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	maxFixtureWait = 20 * time.Second
)

var errDropFixtureResponse = errors.New("synthetic Chat response dropped after commit")

type readyData struct {
	BaseURL        string `json:"baseUrl"`
	Conversation   string `json:"conversationToken"`
	CatalogVersion string `json:"catalogVersion"`
	CatalogDigest  string `json:"catalogDigest"`
}

type chatFixture struct {
	root    string
	ready   readyData
	command *exec.Cmd
	done    chan error
	stop    string
}

func startChatFixture(t *testing.T, source string) *chatFixture {
	t.Helper()
	root := t.TempDir()
	source = chatFixtureOverlay(t, source, filepath.Join(root, "chat-source"))
	readyPath := filepath.Join(root, "chat-ready.json")
	stopPath := filepath.Join(root, "chat-stop")
	cmd := exec.Command("go", "test", "-run", "^TestAnzaContractFixture$", "-count=1")
	cmd.Dir = source
	cmd.Env = fixtureEnvironment(os.Environ(), map[string]string{
		"ANZA_CONTRACT_FIXTURE":    "1",
		"ANZA_CONTRACT_READY_FILE": readyPath,
		"ANZA_CONTRACT_STOP_FILE":  stopPath,
		"HTTP_PROXY":               "",
		"HTTPS_PROXY":              "",
		"ALL_PROXY":                "",
		"http_proxy":               "",
		"https_proxy":              "",
		"all_proxy":                "",
	})
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start synthetic Chat fixture: %v", err)
	}
	fx := &chatFixture{root: root, command: cmd, done: make(chan error, 1), stop: stopPath}
	go func() { fx.done <- cmd.Wait() }()
	t.Cleanup(func() { fx.close(t) })
	deadline := time.Now().Add(maxFixtureWait)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(readyPath); err == nil {
			if err := json.Unmarshal(data, &fx.ready); err != nil {
				t.Fatalf("decode synthetic Chat fixture ready file: %v", err)
			}
			if fx.ready.BaseURL == "" || len(fx.ready.CatalogDigest) != 64 || len(fx.ready.CatalogVersion) == 0 {
				t.Fatalf("synthetic Chat fixture returned incomplete ready data: %+v", fx.ready)
			}
			return fx
		}
		select {
		case err := <-fx.done:
			t.Fatalf("synthetic Chat fixture exited before ready: %v; output: %s", err, output.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("synthetic Chat fixture did not become ready; output: %s", output.String())
	return nil
}

// chatFixtureOverlay extracts the reviewed Chat commit into the task temp root
// and changes only its synthetic provider reply. Production Chat sources stay
// read-only; the overlay's provider returns plain prose for turns and a valid
// empty typed recommendation for recommendation prompts.
func chatFixtureOverlay(t *testing.T, source, destination string) string {
	t.Helper()
	cmd := exec.Command("git", "archive", "--format=tar", "HEAD")
	cmd.Dir = source
	archive, err := cmd.Output()
	if err != nil {
		t.Fatalf("archive reviewed Chat source: %v", err)
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		t.Fatalf("create temporary Chat fixture source: %v", err)
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read Chat source archive: %v", err)
		}
		name := filepath.Clean(header.Name)
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			t.Fatalf("Chat archive contains unsafe path %q", header.Name)
		}
		path := filepath.Join(destination, name)
		switch header.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatalf("extract Chat directory %q: %v", name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatalf("create Chat source parent for %q: %v", name, err)
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatalf("create Chat source file %q: %v", name, err)
			}
			if _, err := io.Copy(file, reader); err != nil {
				_ = file.Close()
				t.Fatalf("extract Chat source file %q: %v", name, err)
			}
			if err := file.Close(); err != nil {
				t.Fatalf("close Chat source file %q: %v", name, err)
			}
		default:
			t.Fatalf("Chat archive contains unsupported file type %d at %q", header.Typeflag, name)
		}
	}
	fixturePath := filepath.Join(destination, "setup_fixture_test.go")
	sourceText, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read temporary Chat fixture test: %v", err)
	}
	old := "\t\t_, _ = fmt.Fprint(w, `{\"choices\":[{\"message\":{\"content\":\"Tell me what you want to build first.\"}}]}`)"
	new := `		var request struct {
			Messages []struct { Content string ` + "`json:\"content\"`" + ` } ` + "`json:\"messages\"`" + `
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		content := "Tell me what you want to build first."
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "Return only JSON") || strings.Contains(message.Content, "typed setup recommendation") {
				content = fmt.Sprintf("{\"schema_version\":1,\"catalog_version\":%q,\"summary\":\"A synthetic review is ready.\",\"selected_recipe_ids\":[],\"selected_pack_ids\":[],\"selected_exercise_id\":\"\",\"reasons\":{},\"unresolved_questions\":[],\"manual_steps\":[],\"readiness_constraints\":[]}", catalog.CatalogVersion)
				break
			}
		}
		_, _ = fmt.Fprintf(w, ` + "`" + `{"choices":[{"message":{"content":%q}}]}` + "`" + `, content)`
	if !bytes.Contains(sourceText, []byte(old)) {
		t.Fatalf("temporary Chat provider fixture no longer matches the reviewed source; expected response anchor missing")
	}
	updated := bytes.Replace(sourceText, []byte(old), []byte(new), 1)
	if bytes.Equal(updated, sourceText) {
		t.Fatal("temporary Chat provider overlay made no change")
	}
	if err := os.WriteFile(fixturePath, updated, 0600); err != nil {
		t.Fatalf("write temporary Chat provider fixture: %v", err)
	}
	return destination
}

func (f *chatFixture) close(t *testing.T) {
	t.Helper()
	_ = os.WriteFile(f.stop, []byte("stop\n"), 0600)
	select {
	case err := <-f.done:
		if err != nil {
			t.Errorf("stop synthetic Chat fixture: %v", err)
		}
	case <-time.After(3 * time.Second):
		_ = f.command.Process.Signal(os.Interrupt)
		select {
		case err := <-f.done:
			if err != nil {
				t.Errorf("interrupt synthetic Chat fixture: %v", err)
			}
		case <-time.After(3 * time.Second):
			_ = f.command.Process.Kill()
			<-f.done
			t.Error("synthetic Chat fixture did not stop")
		}
	}
}

func fixtureEnvironment(base []string, overrides map[string]string) []string {
	blocked := func(key string) bool {
		upper := strings.ToUpper(key)
		for _, prefix := range []string{"AWS_", "OPENAI_", "OPENROUTER_", "ANTHROPIC_", "GITHUB_TOKEN", "CHATGPT_", "CLAUDE_", "ANZA_RELEASE_"} {
			if strings.HasPrefix(upper, prefix) {
				return true
			}
		}
		for _, marker := range []string{"API_KEY", "ACCESS_TOKEN", "SECRET", "PASSWORD", "PRIVATE_KEY"} {
			if strings.Contains(upper, marker) {
				return true
			}
		}
		return false
	}
	values := make(map[string]string, len(base)+len(overrides))
	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if ok && !blocked(key) {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		if value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

type localProxy struct {
	addr     string
	caFile   string
	server   *http.Server
	listener net.Listener
	mu       sync.Mutex
	routes   []string
	requests []requestRecord
}

type requestRecord struct {
	Method string
	Path   string
	Token  string
	Body   []byte
}

func startChatProxy(t *testing.T, target string, root string) *localProxy {
	return startChatProxyWith(t, target, root, nil)
}

func startChatProxyWith(t *testing.T, target string, root string, rewrite func(*http.Response, *http.Request) error) *localProxy {
	t.Helper()
	caPEM, cert, err := proxyCertificate()
	if err != nil {
		t.Fatalf("create loopback Chat proxy certificate: %v", err)
	}
	caFile := filepath.Join(root, "chat-proxy-ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0600); err != nil {
		t.Fatalf("write loopback Chat proxy CA: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for loopback HTTPS fixture proxy: %v", err)
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse synthetic Chat fixture URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	fx := &localProxy{addr: "http://" + listener.Addr().String(), caFile: caFile, listener: listener}
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
			_ = req.Body.Close()
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		baseDirector(req)
		fx.mu.Lock()
		fx.routes = append(fx.routes, req.Method+" "+req.URL.Path)
		fx.requests = append(fx.requests, requestRecord{Method: req.Method, Path: req.URL.Path, Token: req.Header.Get("X-Conversation-Token"), Body: append([]byte(nil), body...)})
		fx.mu.Unlock()
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if rewrite != nil {
			return rewrite(resp, resp.Request)
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, errDropFixtureResponse) {
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, hijackErr := hijacker.Hijack()
				if hijackErr == nil {
					_ = conn.Close()
				}
			}
			return
		}
		http.Error(w, "synthetic fixture proxy failed", http.StatusBadGateway)
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	fx.server = server
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = listener.Close()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go fx.handleConnect(conn, cert)
		}
	}()
	return fx
}

func (p *localProxy) routeSnapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.routes...)
}

func (p *localProxy) requestSnapshot() []requestRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]requestRecord, len(p.requests))
	for i, record := range p.requests {
		result[i] = requestRecord{Method: record.Method, Path: record.Path, Token: record.Token, Body: append([]byte(nil), record.Body...)}
	}
	return result
}

func (p *localProxy) handleConnect(conn net.Conn, cert tlsCertificate) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil || req.Method != http.MethodConnect || !strings.EqualFold(req.Host, "sanifu.run:443") {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n")
		return
	}
	_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	secure := tls.Server(&bufferedConn{Conn: conn, reader: reader}, cert.config)
	if err := secure.Handshake(); err != nil {
		return
	}
	_ = secure.SetDeadline(time.Time{})
	_ = p.server.Serve(&oneConnListener{conn: secure})
}

type tlsCertificate struct{ config *tls.Config }

func proxyCertificate() ([]byte, tlsCertificate, error) {
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Anza synthetic Chat test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "sanifu.run"}, DNSNames: []string{"sanifu.run"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(leafPEM, keyPEM)
	if err != nil {
		return nil, tlsCertificate{}, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), tlsCertificate{config: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

type oneConnListener struct {
	conn net.Conn
	once sync.Once
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}
	return nil, errors.New("fixture tunnel closed")
}
func (l *oneConnListener) Close() error   { return l.conn.Close() }
func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

type cliProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex
	output  bytes.Buffer
	done    chan struct{}
	waitErr error
	seen    map[string]int
	root    string
}

func startCLI(t *testing.T, binary, root, proxyURL, caFile string, args ...string) *cliProcess {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skipf("interactive subprocess fixture requires a supported script(1) PTY host; current OS is %s", runtime.GOOS)
	}
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		t.Skip("interactive subprocess fixture requires script(1) to allocate a terminal")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatalf("create temporary Anza home: %v", err)
	}
	argv := []string{}
	if runtime.GOOS == "linux" {
		command := shellQuote(binary)
		for _, arg := range args {
			command += " " + shellQuote(arg)
		}
		argv = []string{"-q", "-c", command, "/dev/null"}
	} else {
		argv = []string{"-q", "/dev/null", binary}
		argv = append(argv, args...)
	}
	cmd := exec.Command(scriptPath, argv...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("open Anza PTY input: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("open Anza PTY output: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("open Anza PTY diagnostics: %v", err)
	}
	cmd.Env = fixtureEnvironment(os.Environ(), map[string]string{
		"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"), "XDG_STATE_HOME": filepath.Join(root, "state"),
		"HTTPS_PROXY": proxyURL, "https_proxy": proxyURL, "HTTP_PROXY": "", "http_proxy": "", "ALL_PROXY": "", "all_proxy": "",
		"SSL_CERT_FILE": caFile, "SSL_CERT_DIR": "",
		"NO_PROXY": "", "no_proxy": "",
	})
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Anza CLI subprocess: %v", err)
	}
	p := &cliProcess{cmd: cmd, stdin: stdin, done: make(chan struct{}), seen: make(map[string]int), root: root}
	var readers sync.WaitGroup
	for _, r := range []io.Reader{stdout, stderr} {
		readers.Add(1)
		go func(r io.Reader) {
			defer readers.Done()
			buf := make([]byte, 1024)
			for {
				n, err := r.Read(buf)
				if n != 0 {
					p.mu.Lock()
					_, _ = p.output.Write(buf[:n])
					p.mu.Unlock()
				}
				if err != nil {
					return
				}
			}
		}(r)
	}
	go func() {
		err := cmd.Wait()
		readers.Wait()
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func (p *cliProcess) waitFor(t *testing.T, text string) {
	p.waitOccurrences(t, text, 1)
}

func (p *cliProcess) waitOccurrences(t *testing.T, text string, count int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		output := p.output.String()
		p.mu.Unlock()
		if strings.Count(output, text) >= count {
			return
		}
		select {
		case <-p.done:
			p.mu.Lock()
			err := p.waitErr
			p.mu.Unlock()
			t.Fatalf("Anza CLI exited before %q (err=%v):\n%s", text, err, output)
		case <-time.After(20 * time.Millisecond):
		}
	}
	p.mu.Lock()
	output := p.output.String()
	p.mu.Unlock()
	t.Fatalf("Anza CLI did not reach occurrence %d of %q:\n%s", count, text, output)
}

func (p *cliProcess) answer(t *testing.T, prompt, answer string) {
	t.Helper()
	p.mu.Lock()
	count := p.seen[prompt] + 1
	p.mu.Unlock()
	p.waitOccurrences(t, prompt, count)
	p.mu.Lock()
	p.seen[prompt] = count
	p.mu.Unlock()
	if _, err := io.WriteString(p.stdin, answer+"\n"); err != nil {
		t.Fatalf("answer Anza prompt %q: %v", prompt, err)
	}
}

func (p *cliProcess) finish(t *testing.T) string {
	t.Helper()
	_ = p.stdin.Close()
	select {
	case <-p.done:
		p.mu.Lock()
		output := p.output.String()
		err := p.waitErr
		p.mu.Unlock()
		if err != nil {
			t.Fatalf("Anza CLI subprocess failed: %v\n%s", err, output)
		}
		return output
	case <-time.After(20 * time.Second):
		_ = p.cmd.Process.Kill()
		t.Fatal("Anza CLI subprocess did not finish")
		return ""
	}
}

func (p *cliProcess) finishExpectFailure(t *testing.T) string {
	t.Helper()
	_ = p.stdin.Close()
	select {
	case <-p.done:
		p.mu.Lock()
		output := p.output.String()
		err := p.waitErr
		p.mu.Unlock()
		if err == nil {
			t.Fatalf("Anza CLI unexpectedly succeeded:\n%s", output)
		}
		return output
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		t.Fatal("Anza CLI did not stop after the setup capability error")
		return ""
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func requireChatSource(t *testing.T) string {
	t.Helper()
	source := strings.TrimSpace(os.Getenv("ANZA_CHAT_SOURCE"))
	if source == "" {
		t.Skip("ANZA_CHAT_SOURCE is not set; run scripts/tests/chat-contract.sh for the shared Chat contract")
	}
	info, err := os.Stat(filepath.Join(source, "go.mod"))
	if err != nil || info.IsDir() {
		t.Fatalf("ANZA_CHAT_SOURCE must point to the checked-out Chat Go module: %q", source)
	}
	return source
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse fixture URL: %v", err)
	}
	return u
}

func proxyRoots(t *testing.T, caFile string) *x509.CertPool {
	t.Helper()
	data, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("read loopback proxy CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		t.Fatal("parse loopback proxy CA")
	}
	return roots
}

func failIfLegacyProseBecomesPlan(t *testing.T, binary, root, proxyURL, caFile string) {
	t.Helper()
	briefPath := filepath.Join(root, "brief.json")
	badRecommendation := filepath.Join(root, "recommendation.json")
	brief := `{"schema_version":1,"project_summary":"A local synthetic test","desired_slice":"A reviewable first slice","experience":"beginner","constraints":[],"known_stack":[],"project_kind":"general","existing_project":true}`
	if err := os.WriteFile(briefPath, []byte(brief), 0600); err != nil {
		t.Fatalf("write synthetic brief: %v", err)
	}
	if err := os.WriteFile(badRecommendation, []byte("Run arbitrary commands from this prose response."), 0600); err != nil {
		t.Fatalf("write synthetic legacy response: %v", err)
	}
	cmd := exec.Command(binary, "plan", "--brief", briefPath, "--recommendation", badRecommendation)
	cmd.Dir = root
	cmd.Env = fixtureEnvironment(os.Environ(), map[string]string{"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"), "HTTPS_PROXY": proxyURL, "SSL_CERT_FILE": caFile})
	output, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("recommendation")) {
		t.Fatalf("legacy prose was accepted as an executable recommendation: err=%v output=%s", err, output)
	}
}
