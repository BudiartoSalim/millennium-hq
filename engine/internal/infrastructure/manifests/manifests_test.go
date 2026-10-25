package manifests

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func read(t *testing.T, path, content string) (facts []string, issues []string) {
	t.Helper()
	got, ok, err := Reader{}.Read(path, []byte(content))
	if err != nil || !ok {
		t.Fatalf("Read(%s) = %v, %v", path, ok, err)
	}
	for _, p := range got.Packages {
		entry := p.Key()
		if p.InstallScript {
			entry += " (install script)"
		}
		facts = append(facts, entry)
	}
	for _, i := range got.Issues {
		issues = append(issues, i.Code+":"+strings.Fields(i.Message)[0]+":"+strconv.Itoa(i.Line))
	}
	return facts, issues
}

func TestNpmLock(t *testing.T) {
	lock := `{"lockfileVersion": 3, "packages": {
		"": {"name": "app"},
		"node_modules/express": {"version": "4.21.2"},
		"node_modules/@scope/pkg": {"version": "1.0.0", "hasInstallScript": true},
		"node_modules/express/node_modules/ms": {"version": "2.0.0"},
		"packages/shared": {"version": "0.0.0"},
		"node_modules/shared": {"link": true}
	}}`
	got, _ := read(t, "web/package-lock.json", lock)
	if want := []string{"@scope/pkg@1.0.0 (install script)", "express@4.21.2", "ms@2.0.0"}; !slices.Equal(got, want) {
		t.Fatalf("packages = %q, want %q", got, want)
	}
	if _, _, err := (Reader{}).Read("package-lock.json", []byte(`{"lockfileVersion": 1}`)); err == nil {
		t.Fatal("lockfile v1 must be refused")
	}
	if _, _, err := (Reader{}).Read("package-lock.json", []byte(`nope`)); err == nil {
		t.Fatal("broken JSON must be an error")
	}
}

func TestPackageJSON(t *testing.T) {
	manifest := "{\n  \"dependencies\": {\n    \"express\": \"4.21.2\",\n    \"lodash\": \"^4.17.21\"\n  },\n  \"devDependencies\": {\n    \"local\": \"file:../local\",\n    \"ts\": \"latest\"\n  },\n  \"peerDependencies\": {\"react\": \">=18\"}\n}\n"
	_, issues := read(t, "package.json", manifest)
	if want := []string{"unpinned:lodash:4", "unpinned:ts:8"}; !slices.Equal(issues, want) {
		t.Fatalf("issues = %q, want %q", issues, want)
	}
	if _, _, err := (Reader{}).Read("package.json", []byte(`{"dependencies": ["x"]}`)); err == nil {
		t.Fatal("malformed dependencies must be an error")
	}
}

func TestGoSum(t *testing.T) {
	sum := "github.com/redis/go-redis/v9 v9.7.0 h1:abc=\ngithub.com/redis/go-redis/v9 v9.7.0/go.mod h1:def=\ngolang.org/x/sys v0.1.0/go.mod h1:x=\n"
	got, _ := read(t, "svc/go.sum", sum)
	if want := []string{"github.com/redis/go-redis/v9@v9.7.0"}; !slices.Equal(got, want) {
		t.Fatalf("packages = %q", got)
	}
}

func TestRequirements(t *testing.T) {
	txt := "# resolved\nonnxruntime==1.20.1 \\\n    --hash=sha256:aaa\nNumPy==2.1.0\nrequests>=2\n-r other.txt\n"
	got, issues := read(t, "app/requirements.txt", txt)
	if want := []string{"numpy@2.1.0", "onnxruntime@1.20.1"}; !slices.Equal(sortedCopy(got), want) {
		t.Fatalf("packages = %q", got)
	}
	if want := []string{"unhashed:NumPy:4", "unpinned:\"requests>=2\":5"}; !slices.Equal(issues, want) {
		t.Fatalf("issues = %q, want %q", issues, want)
	}
	_, inIssues := read(t, "app/requirements.in", "kokoro==0.9.4\n")
	if len(inIssues) != 0 {
		t.Fatalf("a .in file needs no hashes: %q", inIssues)
	}
}

func TestWorkflowDockerfileCompose(t *testing.T) {
	wf := "jobs:\n  x:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/setup-go@0a12ed9d6a96ab950c8f026ed9f722fe0da7ef32\n      - uses: ./local-action\n"
	_, issues := read(t, ".github/workflows/ci.yml", wf)
	if want := []string{"unpinned-action:actions/checkout@v4:4"}; !slices.Equal(issues, want) {
		t.Fatalf("workflow issues = %q", issues)
	}
	df := "FROM golang:1.25@sha256:abc AS build\nFROM build AS test\nFROM gcr.io/distroless/static:nonroot\nFROM scratch\n"
	_, issues = read(t, "svc/Dockerfile", df)
	if want := []string{"unpinned-image:gcr.io/distroless/static:nonroot:3"}; !slices.Equal(issues, want) {
		t.Fatalf("dockerfile issues = %q", issues)
	}
	cp := "services:\n  db:\n    image: postgres:17\n  app:\n    image: \"ghcr.io/x/app@sha256:abc\"\n"
	_, issues = read(t, "docker-compose.yml", cp)
	if want := []string{"unpinned-image:postgres:17:3"}; !slices.Equal(issues, want) {
		t.Fatalf("compose issues = %q", issues)
	}
	if _, ok, _ := (Reader{}).Read("main.go", nil); ok {
		t.Fatal("main.go is not a manifest")
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
