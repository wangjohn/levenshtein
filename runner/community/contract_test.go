package community

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// roundTrips reads member of runner/testdata/file, decodes it into value
// refusing unknown fields, and checks that value encodes back to the same JSON
// and that the fixture sets every field value's type declares. The other side
// of the contract, internal/verify's contract_test.go, checks the same
// fixture, so a field one side adds and the other lacks fails one of them.
func roundTrips(t *testing.T, file, member string, value any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(fixture[member]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		t.Fatalf("decoding %s's %s: %v", file, member, err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var want, got any
	if err := json.Unmarshal(fixture[member], &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s's %s encodes back as %s", file, member, encoded)
	}
	set := map[string]bool{}
	fixtureKeys(want, "", set)
	for _, key := range jsonKeys(reflect.TypeOf(value), "") {
		if !set[key] {
			t.Errorf("%s's %s does not set %s", file, member, key)
		}
	}
}

// jsonKeys lists the keys typ encodes, a nested key as "outer.inner", through
// pointers, slices and maps.
func jsonKeys(typ reflect.Type, prefix string) []string {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}

	var keys []string
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		keys = append(keys, prefix+name)
		keys = append(keys, jsonKeys(typ.Field(i).Type, prefix+name+".")...)
	}
	return keys
}

// fixtureKeys records the keys a decoded JSON value sets, as jsonKeys spells
// them.
func fixtureKeys(value any, prefix string, set map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, inner := range v {
			set[prefix+key] = true
			fixtureKeys(inner, prefix+key+".", set)
		}
	case []any:
		for _, inner := range v {
			fixtureKeys(inner, prefix, set)
		}
	}
}

// The community linter reads a rule module as the CLI plans it, so
// ModuleConfig and internal/verify's PlannedRuleModule round-trip one fixture.
func TestModuleConfigRoundTripsTheSharedFixture(t *testing.T) {
	t.Parallel()
	roundTrips(t, "rule-module.json", "module", &ModuleConfig{})
}
