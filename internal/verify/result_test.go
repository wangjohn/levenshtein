package verify

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestResultCopiesPreserveDiagnosticsAndMetadata(t *testing.T) {
	original := Result{
		ID:          "check",
		Status:      StatusPassed,
		DurationMS:  100,
		VerifiedAt:  time.Unix(123, 0).UTC(),
		Stdout:      "output",
		Stderr:      "warning",
		Error:       "detail",
		Cache:       CacheInfo{Status: CacheMiss, Key: "key", Reason: "reason", LookupMS: 10},
		ExecutionMS: 90,
		Stages:      []StageResult{{Kind: StagePreparation, Key: "stage", Reused: true, DurationMS: 5}},
		Details:     json.RawMessage(`{"finding":"example"}`),
	}
	for _, tc := range []struct {
		name    string
		result  Result
		changed map[string]any
	}{
		{"outcome", original.withOutcome(StatusError, "new error"), map[string]any{"Status": StatusError, "Error": "new error"}},
		{"cache", original.withCache(CacheInfo{Status: CacheHit}), map[string]any{"Cache": CacheInfo{Status: CacheHit}}},
		{"stages", original.withStages(nil), map[string]any{"Stages": []StageResult(nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := reflect.ValueOf(original), reflect.ValueOf(tc.result)
			for i := 0; i < before.NumField(); i++ {
				name := before.Type().Field(i).Name
				want, changed := tc.changed[name]
				if !changed {
					want = before.Field(i).Interface()
				}
				if got := after.Field(i).Interface(); !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %#v, want %#v", name, got, want)
				}
			}
		})
	}
}

func TestTypedStatusesKeepJSONSpellings(t *testing.T) {
	data, err := json.Marshal(Result{Status: StatusPassed, Cache: CacheInfo{Status: CacheHit}})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["status"] != "passed" || wire["cache"].(map[string]any)["status"] != "hit" {
		t.Fatalf("wire format changed: %s", data)
	}
}
