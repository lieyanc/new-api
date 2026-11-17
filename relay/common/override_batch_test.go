package common

import (
	"bytes"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
)

func TestBatchedParamOverrideMatchesOperations(t *testing.T) {
	input := []byte(`{"model":"old","input":[{"type":"input_image","url":"AAAA"}],"a":{"value":1},"store":false}`)
	ops := []ParamOperation{
		{Mode: "set", Path: "model", Value: "new"},
		{Mode: "set", Path: "a.value", Value: 2},
		{Mode: "set", Path: "store", Value: true, KeepOrigin: true},
		{Mode: "delete", Path: "input.0.url"},
		{Mode: "set", Path: "missing.0.value", Value: 0},
	}
	expected, err := applyOperations(input, ops, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := applyBatchedParamOverride(input, nil, ops, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, string(expected), string(actual))
	if !bytes.Contains(input, []byte(`"model":"old"`)) {
		t.Fatal("mutated source")
	}
}

func TestBatchedParamOverrideLiteralKeysAndRawValues(t *testing.T) {
	input := []byte(`{"input":{"id":9007199254740993,"n":1e+09},"model":"first","model":"last"}`)
	legacy := map[string]interface{}{"model": "new", "a.b": 1, "a*b": 2, "a?b": 3, `a\b`: 4, "123": 5, "": 6, "@this": 7, ":colon": 8}
	got, err := ApplyParamOverride(input, legacy, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = appcommon.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	for key, value := range legacy {
		if key == "model" {
			if decoded[key] != value {
				t.Fatal("model override failed")
			}
			continue
		}
		if decoded[key] != float64(value.(int)) {
			t.Fatalf("literal key %q: got %#v, body %s", key, decoded[key], got)
		}
	}
	if bytes.Count(got, []byte(`"model"`)) != 1 {
		t.Fatalf("duplicate override survived: %s", got)
	}
	if !bytes.Contains(got, []byte(`"input":{"id":9007199254740993,"n":1e+09}`)) {
		t.Fatalf("unmodified payload rewritten: %s", got)
	}
}

func TestBatchedParamOverrideKeepsConditionalFallback(t *testing.T) {
	rules := map[string]interface{}{"model": "mapped", "operations": []interface{}{
		map[string]interface{}{"mode": "set", "path": "store", "value": false, "conditions": []interface{}{map[string]interface{}{"path": "model", "mode": "full", "value": "mapped"}}},
	}}
	got, err := ApplyParamOverride([]byte(`{"input":[1],"model":"original"}`), rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, `{"input":[1],"model":"mapped","store":false}`, string(got))
}

func TestBatchedParamOverrideNoopAndNullFallback(t *testing.T) {
	for _, raw := range []string{`null`, `{ "model": "test" }`, `[]`} {
		got, err := ApplyParamOverride([]byte(raw), map[string]interface{}{"operations": []interface{}{}}, nil)
		if err != nil || string(got) != raw {
			t.Fatalf("noop changed %s to %s: %v", raw, got, err)
		}
	}
	got, err := ApplyParamOverride([]byte(`null`), map[string]interface{}{"operations": []interface{}{map[string]interface{}{"mode": "delete", "path": "unused"}}}, nil)
	if err != nil || string(got) != "null" {
		t.Fatalf("delete null: %s, %v", got, err)
	}
}
