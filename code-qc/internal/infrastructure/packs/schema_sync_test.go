package packs

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

// The pack schema is what editors validate pack.json against; the engine
// decides. This test fails when the two drift apart.

const schemaPath = "../../../../schema/millennium-pack.schema.json"

type schemaNode = map[string]any

func properties(t *testing.T, node schemaNode, path ...string) schemaNode {
	t.Helper()
	for _, key := range path {
		next, ok := node[key].(schemaNode)
		if !ok {
			t.Fatalf("schema has no object at %s", strings.Join(path, "."))
		}
		node = next
	}
	if node["additionalProperties"] != false {
		t.Errorf("schema object %s must set additionalProperties to false", strings.Join(path, "."))
	}
	props, ok := node["properties"].(schemaNode)
	if !ok {
		t.Fatalf("schema object %s has no properties", strings.Join(path, "."))
	}
	return props
}

func keys(m schemaNode) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
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

func TestPackSchemaMatchesDecoder(t *testing.T) {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var root schemaNode
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	objects := []struct {
		name  string
		props schemaNode
		dto   reflect.Type
	}{
		{"root", properties(t, root), reflect.TypeFor[packDTO]()},
		{"layer", properties(t, root, "properties", "layers", "items"), reflect.TypeFor[layerDTO]()},
		{"limits", properties(t, root, "properties", "limits"), reflect.TypeFor[limitsDTO]()},
		{"suggest", properties(t, root, "properties", "suggest"), reflect.TypeFor[suggestDTO]()},
	}
	for _, obj := range objects {
		if got, want := keys(obj.props), jsonTags(obj.dto); !slices.Equal(got, want) {
			t.Errorf("%s: schema properties %q, decoder fields %q", obj.name, got, want)
		}
	}
	props := properties(t, root)
	name := props["name"].(schemaNode)
	if name["pattern"] != domain.ConventionNamePattern || name["maxLength"] != float64(domain.MaxConventionName) {
		t.Errorf("name: schema %v/%v, domain %q/%d", name["pattern"], name["maxLength"], domain.ConventionNamePattern, domain.MaxConventionName)
	}
	if format := props["format"].(schemaNode)["const"]; format != float64(Format) {
		t.Errorf("format: schema %v, engine %d", format, Format)
	}
}
