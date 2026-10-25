package configfile

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"millennium/internal/domain"
)

// The JSON Schema is what users' editors validate against; the engine is what
// actually decides. These tests fail when the two drift apart.

const schemaPath = "../../../../schema/millennium.schema.json"

type schemaNode = map[string]any

func loadSchema(t *testing.T) schemaNode {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var root schemaNode
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return root
}

func child(t *testing.T, node schemaNode, path ...string) schemaNode {
	t.Helper()
	for _, key := range path {
		next, ok := node[key].(schemaNode)
		if !ok {
			t.Fatalf("schema has no object at %s", strings.Join(path, "."))
		}
		node = next
	}
	return node
}

func stringArray(t *testing.T, node schemaNode, key string) []string {
	t.Helper()
	raw, ok := node[key].([]any)
	if !ok {
		t.Fatalf("schema node has no array %q: %v", key, node)
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i] = v.(string)
	}
	return out
}

func asStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func jsonTags(t reflect.Type) []string {
	tags := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tags = append(tags, strings.Split(t.Field(i).Tag.Get("json"), ",")[0])
	}
	slices.Sort(tags)
	return tags
}

func propertyNames(t *testing.T, node schemaNode) []string {
	t.Helper()
	if node["additionalProperties"] != false {
		t.Errorf("schema object %v must set additionalProperties to false", node["description"])
	}
	props := child(t, node, "properties")
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestSchemaPropertiesMatchDecoder(t *testing.T) {
	root := loadSchema(t)
	objects := []struct {
		name string
		node schemaNode
		dto  reflect.Type
	}{
		{"root", root, reflect.TypeFor[configDTO]()},
		{"python", child(t, root, "properties", "python"), reflect.TypeFor[pythonDTO]()},
		{"paths", child(t, root, "properties", "paths"), reflect.TypeFor[pathsDTO]()},
		{"run", child(t, root, "properties", "run"), reflect.TypeFor[runDTO]()},
	}
	for _, obj := range objects {
		if got, want := propertyNames(t, obj.node), jsonTags(obj.dto); !slices.Equal(got, want) {
			t.Errorf("%s: schema properties %q, decoder fields %q", obj.name, got, want)
		}
	}
}

func TestSchemaEnumsMatchDomain(t *testing.T) {
	props := child(t, loadSchema(t), "properties")
	enums := []struct {
		name   string
		node   schemaNode
		domain []string
	}{
		{"languages", child(t, props, "languages", "items"), asStrings(domain.Languages())},
		{"conventions", child(t, props, "conventions", "items"), asStrings(domain.Conventions())},
		{"testing", child(t, props, "testing"), asStrings(domain.TestingProfiles())},
		{"dependencyPolicy", child(t, props, "dependencyPolicy"), asStrings(domain.DependencyPolicies())},
		{"ci", child(t, props, "ci"), asStrings(domain.CIProviders())},
		{"python.typeCheck", child(t, props, "python", "properties", "typeCheck"), asStrings(domain.PythonTypeChecks())},
	}
	for _, e := range enums {
		if got := stringArray(t, e.node, "enum"); !slices.Equal(got, e.domain) {
			t.Errorf("%s: schema enum %q, domain %q", e.name, got, e.domain)
		}
	}
}

func TestSchemaDefaultsAndRequiredMatchDomain(t *testing.T) {
	root := loadSchema(t)
	props := child(t, root, "properties")
	defaults := []struct {
		name string
		node schemaNode
		want string
	}{
		{"testing", child(t, props, "testing"), string(domain.DefaultTesting)},
		{"dependencyPolicy", child(t, props, "dependencyPolicy"), string(domain.DefaultDependencyPolicy)},
		{"ci", child(t, props, "ci"), string(domain.DefaultCI)},
		{"python.typeCheck", child(t, props, "python", "properties", "typeCheck"), string(domain.DefaultPythonTypeCheck)},
		{"paths.acceptanceTests", child(t, props, "paths", "properties", "acceptanceTests"), domain.DefaultAcceptanceTests},
	}
	for _, d := range defaults {
		if got := d.node["default"]; got != d.want {
			t.Errorf("%s: schema default %v, domain default %q", d.name, got, d.want)
		}
	}
	if got := child(t, props, "version")["const"]; got != float64(domain.ConfigVersion) {
		t.Errorf("version: schema const %v, domain %d", got, domain.ConfigVersion)
	}
	if got := stringArray(t, root, "required"); !slices.Equal(got, []string{"version", "languages"}) {
		t.Errorf("root required = %q", got)
	}
	if got := stringArray(t, child(t, props, "run"), "required"); !slices.Equal(got, []string{"start", "healthUrl"}) {
		t.Errorf("run required = %q", got)
	}
}
