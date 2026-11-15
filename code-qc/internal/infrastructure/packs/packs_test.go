package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"millennium-code-qc/internal/domain"
)

func TestBuiltinPacksAreValid(t *testing.T) {
	builtin, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	catalog, err := domain.NewCatalog(builtin, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	clean, react := catalog["clean-architecture"], catalog["react-feature-based"]
	if _, ok := clean.Paths[domain.LanguageGo]; !ok || len(clean.Layers) != 4 || clean.Origin != domain.PackBuiltin {
		t.Fatalf("clean-architecture pack = %+v", clean)
	}
	if len(react.Layers) != 2 || !react.Layers[1].Isolated || react.Layers[1].Name != "features" || react.Suggest[0] != "react" {
		t.Fatalf("react-feature-based pack = %+v", react)
	}
	for _, pack := range builtin {
		if pack.Limits != domain.DefaultLimits() || !strings.HasPrefix(pack.Guide, "# ") {
			t.Errorf("%s: limits %+v, guide %.20q", pack.Name, pack.Limits, pack.Guide)
		}
	}
}

const validPack = `{"format": 1, "name": "flat", "languages": ["go"],
  "layers": [{"name": "core", "mayImport": [], "thirdParty": false, "tested": true, "isolated": false}],
  "paths": {"go": {"core": ["core"]}}, "limits": {"cognitive": 10}, "suggest": {"dependencies": ["chi"]}}`

func TestDecode(t *testing.T) {
	pack, err := Decode("flat", Files{Pack: []byte(validPack), Guide: []byte("Keep it flat.")}, domain.PackProject)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if pack.Limits.Cognitive != 10 || pack.Limits.Nesting != 3 || pack.Origin != domain.PackProject || pack.Suggest[0] != "chi" {
		t.Fatalf("pack = %+v", pack)
	}
}

func TestDecodeRejects(t *testing.T) {
	tests := []struct {
		name, folder, pack, guide, want string
	}{
		{"unknown field", "flat", `{"format":1,"name":"flat","languages":["go"],"layers":[],"paths":{},"extra":1}`, "g", "unknown field"},
		{"duplicate key", "flat", `{"format":1,"format":1,"name":"flat"}`, "g", "appears more than once"},
		{"trailing data", "flat", validPack + `{}`, "g", "unexpected data after the object"},
		{"format", "flat", `{"format":2,"name":"flat"}`, "g", "format 2 is not supported"},
		{"folder name", "other", validPack, "g", `the pack is named "flat" but its folder is "other"`},
		{"invalid pack", "flat", `{"format":1,"name":"flat","languages":["go"],"layers":[{"name":"core","mayImport":["web"]}],"paths":{}}`, "g", `"web" is not a layer`},
		{"no guide", "flat", validPack, "", "every pack needs a guide.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.folder, Files{Pack: []byte(tt.pack), Guide: []byte(tt.guide)}, domain.PackProject)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestLoadFSReportsBrokenPacks(t *testing.T) {
	broken := fstest.MapFS{"builtin/flat/pack.json": {Data: []byte(validPack)}}
	if _, err := loadFS(broken, "builtin"); err == nil || !strings.Contains(err.Error(), "read builtin/flat") {
		t.Fatalf("a pack without a guide: %v", err)
	}
	noPack := fstest.MapFS{"builtin/flat/guide.md": {Data: []byte("g")}}
	if _, err := loadFS(noPack, "builtin"); err == nil {
		t.Fatal("a pack without pack.json must be refused")
	}
	invalid := fstest.MapFS{"builtin/flat/pack.json": {Data: []byte("{")}, "builtin/flat/guide.md": {Data: []byte("g")}}
	if _, err := loadFS(invalid, "builtin"); err == nil {
		t.Fatal("an invalid pack must be refused")
	}
	if _, err := loadFS(fstest.MapFS{}, "builtin"); err == nil {
		t.Fatal("a missing root must be reported")
	}
}

func writePack(t *testing.T, dir, name, pack, guide string) {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{PackFile: pack, GuideFile: guide} {
		if err := os.WriteFile(filepath.Join(folder, file), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	if packs, err := ReadDir(filepath.Join(dir, "missing"), domain.PackProject); err != nil || packs != nil {
		t.Fatalf("a missing folder holds no packs: %v, %v", packs, err)
	}
	writePack(t, dir, "flat", validPack, "Keep it flat.")
	packs, err := ReadDir(dir, domain.PackLocal)
	if err != nil || len(packs) != 1 || packs[0].Name != "flat" || packs[0].Origin != domain.PackLocal {
		t.Fatalf("packs = %+v, %v", packs, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDir(dir, domain.PackProject); err == nil || !strings.Contains(err.Error(), "must be a pack folder") {
		t.Fatalf("a stray file must be refused: %v", err)
	}
}

func TestReadDirBounds(t *testing.T) {
	dir := t.TempDir()
	for i := range maxPacks + 1 {
		if err := os.Mkdir(filepath.Join(dir, strings.Repeat("a", i+1)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReadDir(dir, domain.PackProject); err == nil || !strings.Contains(err.Error(), "at most 32 packs") {
		t.Fatalf("err = %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDir(file, domain.PackProject); err == nil {
		t.Fatal("a file in place of the folder must be refused")
	}
}

func TestReadFolderRefusesSymlinksAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	writePack(t, dir, "flat", validPack, "Keep it flat.")
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(filepath.Join(dir, "flat"), link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if _, _, err := ReadFolder(link, domain.PackProject); err == nil || !strings.Contains(err.Error(), "must be a pack folder") {
		t.Fatalf("a symlinked folder: %v", err)
	}
	if _, _, err := ReadFolder(filepath.Join(dir, "missing"), domain.PackProject); err == nil {
		t.Fatal("a missing folder must be refused")
	}
	writePack(t, dir, "badname", validPack, "Keep it flat.")
	if _, _, err := ReadFolder(filepath.Join(dir, "badname"), domain.PackProject); err == nil || !strings.Contains(err.Error(), "badname") {
		t.Fatalf("a mismatched name: %v", err)
	}
	noGuide := filepath.Join(dir, "noguide")
	if err := os.Mkdir(noGuide, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noGuide, PackFile), []byte(validPack), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFolder(noGuide, domain.PackProject); err == nil {
		t.Fatal("a folder without guide.md must be refused")
	}
	if _, _, err := ReadFolder(filepath.Join(dir, "empty-does-not-exist"), domain.PackProject); err == nil {
		t.Fatal("must fail")
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	builtin, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, pack := range builtin {
		files, err := Encode(pack)
		if err != nil {
			t.Fatalf("Encode %s: %v", pack.Name, err)
		}
		again, err := Decode(string(pack.Name), files, domain.PackBuiltin)
		if err != nil {
			t.Fatalf("Decode %s: %v\n%s", pack.Name, err, files.Pack)
		}
		if again.Limits != pack.Limits || len(again.Layers) != len(pack.Layers) || again.Guide != pack.Guide || len(again.Suggest) != len(pack.Suggest) {
			t.Fatalf("%s did not round-trip:\n%s", pack.Name, files.Pack)
		}
		if !strings.Contains(string(files.Pack), `"cognitive": 15`) {
			t.Errorf("%s: every limit is spelled out:\n%s", pack.Name, files.Pack)
		}
	}
}
