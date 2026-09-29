package catalog

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	content "github.com/sanifu-run/anza/catalog"
)

func TestBundledCatalog(t *testing.T) {
	loaded, err := LoadBundled()
	if err != nil {
		t.Fatalf("LoadBundled(): %v", err)
	}
	if loaded.Version() != "1.0.0" {
		t.Fatalf("catalog version = %q, want 1.0.0", loaded.Version())
	}
	if len(loaded.recipes) != 26 || len(loaded.packs) != 7 || len(loaded.skills) != 6 || len(loaded.exercises) != 1 {
		t.Fatalf("unexpected bundled content counts: %d recipes, %d packs, %d skills, %d exercises", len(loaded.recipes), len(loaded.packs), len(loaded.skills), len(loaded.exercises))
	}
	if !loaded.HasSkill("anza-scope") {
		t.Fatal("core skill missing from bundled catalog")
	}
	if _, ok := loaded.Pack("base"); !ok {
		t.Fatal("base pack missing from bundled catalog")
	}
	exercise, ok := loaded.Exercise("mobile-desktop-exercise")
	if !ok {
		t.Fatal("versioned exercise missing from bundled catalog")
	}
	statuses := make(map[string]bool)
	for _, scenario := range exercise.Scenarios {
		statuses[scenario.Status] = true
	}
	if !statuses["manual"] || !statuses["unsupported"] {
		t.Fatalf("manual and unsupported exercise states were not preserved: %v", statuses)
	}
	if len(loaded.Digest()) != 64 {
		t.Fatalf("catalog digest is not SHA-256: %q", loaded.Digest())
	}
	if loaded.Digest() != "aa6a9c6b46c2e0401d7af7d2d49c3a9be6e5e9678027ebc411d6d3cd122a948c" {
		t.Fatalf("bundled catalog digest %q does not match the sanitized setup export", loaded.Digest())
	}
}

func TestManifestDrift(t *testing.T) {
	root, err := fs.Sub(content.Assets, ".")
	if err != nil {
		t.Fatalf("sub catalog assets: %v", err)
	}
	files := fstest.MapFS{}
	err = fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		if path == "recipes/base/codex-cli.json" {
			data = []byte(strings.Replace(string(data), "0.157.1", "0.157.2", 1))
		}
		files[path] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("copy embedded catalog fixture: %v", err)
	}
	missing := fstest.MapFS{}
	for path, file := range files {
		missing[path] = &fstest.MapFile{Data: append([]byte(nil), file.Data...)}
	}
	delete(missing, "recipes/base/codex-cli.json")
	if _, err := Load(missing); err == nil || !strings.Contains(err.Error(), "reading catalog entry") {
		t.Fatalf("missing bundled asset accepted or wrong failure: %v", err)
	}
	if _, err := Load(files); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("stale manifest accepted or wrong failure: %v", err)
	}
}
