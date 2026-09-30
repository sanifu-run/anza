package lifecycle

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEd25519VerifierAuthenticatesExactPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewEd25519VerifierBase64(base64.StdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"version":"1.2.3"}`)
	signature := ed25519.Sign(privateKey, payload)
	if err := verifier.Verify(payload, signature); err != nil {
		t.Fatalf("Verify(valid) error = %v", err)
	}
	if err := verifier.Verify([]byte(`{"version":"1.2.4"}`), signature); err == nil {
		t.Fatal("Verify(tampered payload) succeeded")
	}
	if _, err := NewEd25519VerifierBase64(""); err == nil {
		t.Fatal("empty build-time key accepted")
	}
	if _, err := NewEd25519VerifierBase64("not base64"); err == nil {
		t.Fatal("invalid build-time key accepted")
	}
}

type releaseRoundTripper func(*http.Request) (*http.Response, error)

func (f releaseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestHTTPReleaseSourceFetchesMetadataAndDetachedSignature(t *testing.T) {
	client := &http.Client{Transport: releaseRoundTripper(func(req *http.Request) (*http.Response, error) {
		body := `{"signed":true}`
		if strings.HasSuffix(req.URL.Path, ".sig") {
			body = "signature-bytes"
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	release, err := (HTTPReleaseSource{MetadataURL: "https://updates.example/release-metadata.json", Client: client}).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(release.Payload) != `{"signed":true}` || string(release.Signature) != "signature-bytes" || !validSHA256(release.MetadataDigest) {
		t.Fatalf("unexpected fetched release: %#v", release)
	}
}

func TestCheckerRejectsNonContractMetadataAfterValidSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewEd25519Verifier(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(ReleaseMetadata{Version: "1.2.3", ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	signed := SignedRelease{Payload: payload, Signature: ed25519.Sign(privateKey, payload), MetadataDigest: hex.EncodeToString(digest[:])}
	_, err = (Checker{Source: &testSource{release: signed}, Verifier: verifier}).Check(context.Background())
	if !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Check() error = %v, want ErrIntegrity for payload outside contract", err)
	}
}
