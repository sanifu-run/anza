package lifecycle

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Ed25519Verifier verifies the exact bytes returned as the signed payload.
type Ed25519Verifier struct{ key ed25519.PublicKey }

// NewEd25519Verifier accepts only a correctly sized Ed25519 public key.
func NewEd25519Verifier(key []byte) (Ed25519Verifier, error) {
	if len(key) != ed25519.PublicKeySize {
		return Ed25519Verifier{}, fmt.Errorf("Ed25519 public key must be %d bytes", ed25519.PublicKeySize)
	}
	return Ed25519Verifier{key: append(ed25519.PublicKey(nil), key...)}, nil
}

// NewEd25519VerifierBase64 decodes the build-time public-key slot.
func NewEd25519VerifierBase64(encoded string) (Ed25519Verifier, error) {
	if strings.TrimSpace(encoded) == "" {
		return Ed25519Verifier{}, errors.New("release Ed25519 public key is not configured")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Ed25519Verifier{}, fmt.Errorf("decode release Ed25519 public key: %w", err)
	}
	return NewEd25519Verifier(key)
}

func (v Ed25519Verifier) Verify(payload, signature []byte) error {
	if len(v.key) != ed25519.PublicKeySize || !ed25519.Verify(v.key, payload, signature) {
		return errors.New("invalid Ed25519 signature")
	}
	return nil
}

// HTTPReleaseSource downloads the signed metadata and its detached signature.
// Redirects are rejected so the configured HTTPS origin remains the trust path.
type HTTPReleaseSource struct {
	MetadataURL string
	Client      *http.Client
}

func (s HTTPReleaseSource) Latest(ctx context.Context) (SignedRelease, error) {
	metadataURL, err := url.Parse(s.MetadataURL)
	if err != nil || metadataURL.Scheme != "https" || metadataURL.Host == "" || metadataURL.User != nil || metadataURL.Fragment != "" || !strings.HasSuffix(metadataURL.Path, "/release-metadata.json") {
		return SignedRelease{}, errors.New("release metadata URL must be an HTTPS URL without credentials or fragment")
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	payload, err := getReleaseBytes(ctx, client, metadataURL.String())
	if err != nil {
		return SignedRelease{}, fmt.Errorf("fetch signed release metadata: %w", err)
	}
	// The release contract fixes this name, avoiding path traversal or an
	// attacker-directed signature fetch before the payload is authenticated.
	signatureURL := *metadataURL
	signatureURL.Path = strings.TrimSuffix(signatureURL.Path, "/release-metadata.json") + "/release-metadata.sig"
	signatureURL.RawPath = ""
	signature, err := getReleaseBytes(ctx, client, signatureURL.String())
	if err != nil {
		return SignedRelease{}, fmt.Errorf("fetch release signature: %w", err)
	}
	digest := sha256.Sum256(payload)
	return SignedRelease{Payload: payload, Signature: signature, MetadataDigest: hex.EncodeToString(digest[:])}, nil
}

func getReleaseBytes(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	const maxMetadataBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxMetadataBytes {
		return nil, errors.New("release metadata response exceeds 1 MiB")
	}
	return data, nil
}
