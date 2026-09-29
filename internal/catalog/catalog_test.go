package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCatalogValidation(t *testing.T) {
	files := map[string]string{
		"recipes/go.json":       `{"id":"go","version":"1.2.3","description":"Go toolchain","purpose":"Build Go projects","supported_platforms":["linux-amd64"],"prerequisites":[],"detection":"go version","install_strategy":"manual","artifact":{"digest":"manual","size":0,"origin":"https://go.dev/dl/"},"privileges":[],"license_notes":"Go license applies","estimated_download_bytes":0,"side_effects":[],"verification":"go version","reversal_class":"manual"}`,
		"packs/anza-check.json": `{"id":"anza-check-pack","version":"1.0.0","skill_ids":["anza-check"],"prerequisites":["go"],"license_ids":["mit"],"provenance_ids":["ecc-golang-testing"],"compatible_agent_versions":{"codex":"1"},"file_digests":{"anza-check.md":"REPLACEME"}}`,
		"packs/anza-check.md":   "# Anza check\n",
	}
	files["packs/anza-check.json"] = strings.Replace(files["packs/anza-check.json"], "REPLACEME", digest(files["packs/anza-check.md"]), 1)
	manifest := manifestFor(t, []Entry{
		{Kind: "recipe", ID: "go", Path: "recipes/go.json", SHA256: digest(files["recipes/go.json"]), ProvenanceIDs: []string{"go-origin"}},
		{Kind: "pack", ID: "anza-check-pack", Path: "packs/anza-check.json", SHA256: digest(files["packs/anza-check.json"]), ProvenanceIDs: []string{"ecc-golang-testing", "mit"}},
		{Kind: "skill", ID: "anza-check", Path: "packs/anza-check.md", SHA256: digest(files["packs/anza-check.md"]), ProvenanceIDs: []string{"mit"}},
	}, []Provenance{
		{ID: "go-origin", Source: "https://go.dev/dl/", License: "BSD-3-Clause"},
		{ID: "ecc-golang-testing", Source: "docs/research/content-sources.json#ecc-golang-testing", License: "MIT"},
		{ID: "mit", Source: "LICENSE", License: "MIT"},
	})
	files["manifest.json"] = manifest

	cat, err := Load(fstest.MapFS(mapFiles(files)))
	if err != nil {
		t.Fatalf("Load(valid catalog): %v", err)
	}
	if _, ok := cat.Recipe("not-in-catalog"); ok {
		t.Fatal("unknown recipe unexpectedly resolved")
	}
	if _, ok := cat.Recipe("go"); !ok {
		t.Fatal("known recipe did not resolve")
	}
	if _, ok := cat.Pack("anza-check-pack"); !ok {
		t.Fatal("known pack did not resolve")
	}
	if !cat.HasSkill("anza-check") {
		t.Fatal("known skill did not resolve")
	}
}

func TestCatalogDigest(t *testing.T) {
	files := map[string]string{"recipes/z.json": recipeJSON("z"), "recipes/a.json": recipeJSON("a")}
	entries := []Entry{
		{Kind: "recipe", ID: "z", Path: "recipes/z.json", SHA256: digest(files["recipes/z.json"]), ProvenanceIDs: []string{"p"}},
		{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest(files["recipes/a.json"]), ProvenanceIDs: []string{"p"}},
	}
	p := []Provenance{{ID: "p", Source: "local fixture", License: "MIT"}}
	files["manifest.json"] = manifestFor(t, entries, p)
	first, err := Load(fstest.MapFS(mapFiles(files)))
	if err != nil {
		t.Fatal(err)
	}
	entries[0], entries[1] = entries[1], entries[0]
	files["manifest.json"] = manifestFor(t, entries, p)
	second, err := Load(fstest.MapFS(mapFiles(files)))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("digest depends on manifest order: %s != %s", first.Digest(), second.Digest())
	}
}

func TestCatalogCycle(t *testing.T) {
	files := map[string]string{"recipes/a.json": recipeJSONWithPrereq("a", "b"), "recipes/b.json": recipeJSONWithPrereq("b", "a")}
	entries := []Entry{
		{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest(files["recipes/a.json"]), ProvenanceIDs: []string{"p"}},
		{Kind: "recipe", ID: "b", Path: "recipes/b.json", SHA256: digest(files["recipes/b.json"]), ProvenanceIDs: []string{"p"}},
	}
	files["manifest.json"] = manifestFor(t, entries, []Provenance{{ID: "p", Source: "fixture", License: "MIT"}})
	if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
		t.Fatal("dependency cycle accepted")
	}
}

func TestMissingProvenance(t *testing.T) {
	files := map[string]string{"recipes/a.json": recipeJSON("a")}
	files["manifest.json"] = manifestFor(t, []Entry{{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest(files["recipes/a.json"]), ProvenanceIDs: []string{"missing"}}}, nil)
	if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
		t.Fatal("missing provenance accepted")
	}
}

func manifestFor(t *testing.T, entries []Entry, provenance []Provenance) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`{"schema_version":1,"version":"1.0.0","entries":[`))
	for i, entry := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf(`{"kind":%q,"id":%q,"path":%q,"sha256":%q,"provenance_ids":`, entry.Kind, entry.ID, entry.Path, entry.SHA256))
		b.WriteString(jsonArray(entry.ProvenanceIDs))
		b.WriteString(`,"dependencies":` + jsonArray(entry.Dependencies) + `}`)
	}
	b.WriteString(`],"provenance":[`)
	for i, p := range provenance {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf(`{"id":%q,"source":%q,"license":%q}`, p.ID, p.Source, p.License))
	}
	b.WriteString(`]}`)
	return b.String()
}

func jsonArray(values []string) string {
	out := "["
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%q", v)
	}
	return out + "]"
}
func digest(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func mapFiles(values map[string]string) map[string]*fstest.MapFile {
	out := make(map[string]*fstest.MapFile, len(values))
	for path, value := range values {
		out[path] = &fstest.MapFile{Data: []byte(value)}
	}
	return out
}
func recipeJSON(id string) string { return recipeJSONWithPrereq(id) }
func recipeJSONWithPrereq(id string, prereq ...string) string {
	return fmt.Sprintf(`{"id":%q,"version":"1.0.0","description":"A tool","purpose":"Build projects","supported_platforms":["linux-amd64"],"prerequisites":%s,"detection":"tool --version","install_strategy":"manual","artifact":{"digest":"manual","size":0,"origin":"https://example.invalid/source"},"privileges":[],"license_notes":"MIT","estimated_download_bytes":0,"side_effects":[],"verification":"tool --version","reversal_class":"manual"}`, id, jsonArray(prereq))
}

func TestCatalogDigestMismatch(t *testing.T) {
	data := recipeJSON("a")
	files := map[string]string{"recipes/a.json": data}
	files["manifest.json"] = manifestFor(t, []Entry{{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest("altered"), ProvenanceIDs: []string{"p"}}}, []Provenance{{ID: "p", Source: "fixture", License: "MIT"}})
	if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
		t.Fatal("digest mismatch accepted")
	}
}

func TestMissingRecipeLicenseOrSource(t *testing.T) {
	cases := []struct{ name, mutate string }{
		{name: "license"},
		{name: "source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := recipeJSON("a")
			if tc.name == "license" {
				data = strings.Replace(data, `"license_notes":"MIT"`, `"license_notes":""`, 1)
			}
			if tc.name == "source" {
				data = strings.Replace(data, `"origin":"https://example.invalid/source"`, `"origin":""`, 1)
			}
			files := map[string]string{"recipes/a.json": data}
			files["manifest.json"] = manifestFor(t, []Entry{{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest(data), ProvenanceIDs: []string{"p"}}}, []Provenance{{ID: "p", Source: "fixture", License: "MIT"}})
			if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
				t.Fatal("missing license or source accepted")
			}
		})
	}
}

func TestUnsupportedExerciseDeferred(t *testing.T) {
	files := map[string]string{"exercise.json": `{}`}
	files["manifest.json"] = manifestFor(t, []Entry{{Kind: "exercise", ID: "demo", Path: "exercise.json", SHA256: digest("{}"), ProvenanceIDs: []string{"p"}}}, []Provenance{{ID: "p", Source: "fixture", License: "MIT"}})
	if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
		t.Fatal("exercise accepted without a versioned contract schema")
	}
}

func TestUnsupportedPlatformPredicate(t *testing.T) {
	data := strings.Replace(recipeJSON("a"), `"linux-amd64"`, `"linux-imaginary-amd64"`, 1)
	files := map[string]string{"recipes/a.json": data}
	files["manifest.json"] = manifestFor(t, []Entry{{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest(data), ProvenanceIDs: []string{"p"}}}, []Provenance{{ID: "p", Source: "fixture", License: "MIT"}})
	if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
		t.Fatal("unsupported platform predicate accepted")
	}
}

func TestCatalogAccessorsReturnCopies(t *testing.T) {
	data := recipeJSON("a")
	files := map[string]string{"recipes/a.json": data}
	files["manifest.json"] = manifestFor(t, []Entry{{Kind: "recipe", ID: "a", Path: "recipes/a.json", SHA256: digest(data), ProvenanceIDs: []string{"p"}}}, []Provenance{{ID: "p", Source: "fixture", License: "MIT"}})
	cat, err := Load(fstest.MapFS(mapFiles(files)))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := cat.Recipe("a")
	first.SupportedPlatforms[0] = "windows-amd64"
	second, _ := cat.Recipe("a")
	if second.SupportedPlatforms[0] != "linux-amd64" {
		t.Fatal("caller mutation changed catalog snapshot")
	}
}

func TestPackAssetDigestMismatch(t *testing.T) {
	asset := "# pack asset\n"
	pack := fmt.Sprintf(`{"id":"demo-pack","version":"1","skill_ids":["anza-demo"],"prerequisites":[],"license_ids":["mit"],"provenance_ids":["source"],"compatible_agent_versions":{},"file_digests":{"SKILL.md":%q}}`, digest("different"))
	files := map[string]string{"packs/demo.json": pack, "packs/SKILL.md": asset, "skills/anza-demo.md": "# skill\n"}
	files["manifest.json"] = manifestFor(t, []Entry{
		{Kind: "pack", ID: "demo-pack", Path: "packs/demo.json", SHA256: digest(pack), ProvenanceIDs: []string{"source", "mit"}},
		{Kind: "skill", ID: "anza-demo", Path: "skills/anza-demo.md", SHA256: digest(files["skills/anza-demo.md"]), ProvenanceIDs: []string{"mit"}},
	}, []Provenance{{ID: "source", Source: "fixture", License: "MIT"}, {ID: "mit", Source: "fixture", License: "MIT"}})
	if _, err := Load(fstest.MapFS(mapFiles(files))); err == nil {
		t.Fatal("pack asset checksum mismatch accepted")
	}
}
