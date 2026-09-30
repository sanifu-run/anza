package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

type testSource struct {
	release SignedRelease
	calls   int
}

func (s *testSource) Latest(context.Context) (SignedRelease, error) {
	s.calls++
	return s.release, nil
}

type testVerifier struct{ err error }

func (v testVerifier) Verify(_, _ []byte) error { return v.err }

func signedFixture(t *testing.T, m ReleaseMetadata) SignedRelease {
	t.Helper()
	if m.CatalogVersion == "" {
		m.CatalogVersion = "1.0.0"
	}
	if m.CatalogDigest == "" {
		m.CatalogDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	}
	if m.PayloadManifests == nil {
		m.PayloadManifests = map[string]string{
			"manifest.json":         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"windows/manifest.json": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}
	}
	manifestSet, err := json.Marshal(m.PayloadManifests)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := sha256.Sum256(manifestSet)
	m.ArtifactDigest = "sha256:" + hex.EncodeToString(manifestDigest[:])
	m.SignatureAlgorithm = "Ed25519"
	m.SignatureFile = "release-metadata.sig"
	return rawSignedFixture(t, m)
}

func rawSignedFixture(t *testing.T, m ReleaseMetadata) SignedRelease {
	t.Helper()
	payload, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	d := sha256.Sum256(payload)
	return SignedRelease{Payload: payload, Signature: []byte("signature"), MetadataDigest: hex.EncodeToString(d[:])}
}

func TestUpdateCheckReadOnly(t *testing.T) {
	before := InstalledState{Version: "1.0.0", CatalogDigest: "catalog-old", Receipt: "receipt-1"}
	state := before
	source := &testSource{release: signedFixture(t, ReleaseMetadata{
		Version: "1.1.0", ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProtocolMin: 1, ProtocolMax: 2,
	})}
	checker := Checker{Source: source, Verifier: testVerifier{}}
	_, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if source.calls != 1 {
		t.Fatalf("metadata fetches = %d, want 1", source.calls)
	}
	if state.Version != before.Version || state.CatalogDigest != before.CatalogDigest || state.Receipt != before.Receipt {
		t.Fatalf("installed state changed: before %#v after %#v", before, state)
	}
}

func TestUpdateIntegrityFailure(t *testing.T) {
	release := signedFixture(t, ReleaseMetadata{Version: "1.1.0", ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProtocolMin: 1, ProtocolMax: 2})
	release.MetadataDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	checker := Checker{Source: &testSource{release: release}, Verifier: testVerifier{}}
	if _, err := checker.Check(context.Background()); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Check() error = %v, want ErrIntegrity", err)
	}

	release = signedFixture(t, ReleaseMetadata{Version: "1.1.0", ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProtocolMin: 1, ProtocolMax: 2})
	checker = Checker{Source: &testSource{release: release}, Verifier: testVerifier{err: errors.New("bad signature")}}
	if _, err := checker.Check(context.Background()); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Check() signature error = %v, want ErrIntegrity", err)
	}
}

func TestUpdateCheckRequiresValidCatalogIdentity(t *testing.T) {
	manifestDigests := map[string]string{
		"manifest.json":         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"windows/manifest.json": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	manifestSet, err := json.Marshal(manifestDigests)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := sha256.Sum256(manifestSet)
	base := ReleaseMetadata{
		Version: "1.1.0", CatalogVersion: "1.0.0",
		CatalogDigest:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ArtifactDigest: "sha256:" + hex.EncodeToString(manifestDigest[:]),
		ProtocolMin:    1, ProtocolMax: 1, PayloadManifests: manifestDigests,
		SignatureAlgorithm: "Ed25519", SignatureFile: "release-metadata.sig",
	}
	if _, err := (Checker{Source: &testSource{release: rawSignedFixture(t, base)}, Verifier: testVerifier{}}).Check(context.Background()); err != nil {
		t.Fatalf("Check(valid catalog identity) error = %v", err)
	}
	for name, mutate := range map[string]func(*ReleaseMetadata){
		"empty version":     func(m *ReleaseMetadata) { m.CatalogVersion = "" },
		"malformed version": func(m *ReleaseMetadata) { m.CatalogVersion = "catalog-1.0" },
		"empty digest":      func(m *ReleaseMetadata) { m.CatalogDigest = "" },
		"malformed digest": func(m *ReleaseMetadata) {
			m.CatalogDigest = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		},
	} {
		t.Run(name, func(t *testing.T) {
			metadata := base
			mutate(&metadata)
			release := rawSignedFixture(t, metadata)
			if _, err := (Checker{Source: &testSource{release: release}, Verifier: testVerifier{}}).Check(context.Background()); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("Check() error = %v, want ErrIntegrity", err)
			}
		})
	}
}

func TestUpdateDriftConflict(t *testing.T) {
	verified := VerifiedRelease{Metadata: ReleaseMetadata{
		Version: "1.1.0", ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProtocolMin: 1, ProtocolMax: 2,
		ManagedFiles: map[string]string{"skills/example.md": "new"},
	}}
	installed := InstalledState{
		Version: "1.0.0", Protocol: 1,
		ManagedFiles: map[string]ManagedFile{"skills/example.md": {RecordedDigest: "old", CurrentDigest: "user-edit"}},
	}
	if _, err := PlanUpdate(installed, verified, Selection{}); !errors.Is(err, ErrDriftConflict) {
		t.Fatalf("PlanUpdate() error = %v, want ErrDriftConflict", err)
	}
}

func TestUpdateIncompatibleStatePreservesReceipt(t *testing.T) {
	installed := InstalledState{Version: "1.0.0", Protocol: 1, Receipt: "verified-receipt"}
	verified := VerifiedRelease{Metadata: ReleaseMetadata{
		Version: "1.1.0", ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProtocolMin: 2, ProtocolMax: 3,
	}}
	if _, err := PlanUpdate(installed, verified, Selection{}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("PlanUpdate() error = %v, want ErrIncompatible", err)
	}
	if installed.Version != "1.0.0" || installed.Receipt != "verified-receipt" {
		t.Fatalf("installed state changed after incompatible plan: %#v", installed)
	}
}

func TestUpdatePlanCarriesCompatibilityAndMigrationEffects(t *testing.T) {
	installed := InstalledState{Version: "1.0.0", CatalogVersion: "catalog-1", CatalogDigest: "old", Protocol: 2}
	verified := VerifiedRelease{Metadata: ReleaseMetadata{
		Version: "1.2.0", CatalogVersion: "catalog-2", CatalogDigest: "new",
		ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProtocolMin:    1, ProtocolMax: 3,
		CompatibilityChanges: []string{"new recipe schema"},
		Migrations:           []Migration{{ID: "state-v2", Description: "migrate local receipt"}},
	}}
	plan, err := PlanUpdate(installed, verified, Selection{})
	if err != nil {
		t.Fatalf("PlanUpdate() error = %v", err)
	}
	if plan.FromVersion != installed.Version || plan.ToVersion != "1.2.0" ||
		plan.FromCatalogVersion != "catalog-1" || plan.ToCatalogVersion != "catalog-2" ||
		len(plan.CompatibilityChanges) != 1 || len(plan.Migrations) != 1 || plan.Migrations[0].ID != "state-v2" {
		t.Fatalf("incomplete review plan: %#v", plan)
	}
}
