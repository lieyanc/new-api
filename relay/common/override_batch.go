package common

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/MoeForge/moego/ojson"
	appcommon "github.com/QuantumNous/new-api/common"
)

// Keep complex path/condition/header behavior on the existing v0.13.2 engine.
// The common unconditional set/delete case can edit a single document instead
// of allocating a complete body per operation (upstream #8148's approach).
var batchParamPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.(?:[A-Za-z_][A-Za-z0-9_]*|[0-9]+))*$`)

func canBatchParamOperations(ops []ParamOperation) bool {
	for _, op := range ops {
		if (op.Mode != "set" && op.Mode != "delete") || len(op.Conditions) != 0 || !batchParamPath.MatchString(op.Path) {
			return false
		}
	}
	return true
}

func applyBatchedParamOverride(data []byte, legacy map[string]interface{}, ops []ParamOperation, recorder *paramOverrideAuditRecorder) ([]byte, error) {
	if !ojson.ValidBytes(data) {
		return nil, fmt.Errorf("invalid JSON request body")
	}
	root := ojson.GetBytesNoCopy(data, "@this")
	if root.Type == ojson.Null {
		data = []byte(`{}`)
	} else if !root.IsObject() {
		return nil, fmt.Errorf("request body must be an object")
	}
	// Empty and colon-prefixed literal keys are not ordinary Doc paths.
	// Keep their legacy map semantics; raw values avoid decoding image strings.
	for key := range legacy {
		if key == "" || strings.HasPrefix(key, ":") {
			var fields map[string]json.RawMessage
			if err := appcommon.Unmarshal(data, &fields); err != nil {
				return nil, err
			}
			for k, v := range legacy {
				encoded, err := appcommon.Marshal(v)
				if err != nil {
					return nil, err
				}
				fields[k] = encoded
				recorder.recordOperation("set", k, "", "", v)
			}
			encoded, err := appcommon.Marshal(fields)
			if err != nil {
				return nil, err
			}
			if len(ops) == 0 {
				return encoded, nil
			}
			return applyBatchedParamOverride(encoded, nil, ops, recorder)
		}
	}
	// Count duplicate legacy keys once on the immutable original. Calling
	// doc.Get("@this") after each edit would flatten/copy the entire document.
	counts := make(map[string]int, len(legacy))
	root.ForEach(func(key, _ ojson.Result) bool {
		if _, ok := legacy[key.Str]; ok {
			counts[key.Str]++
		}
		return true
	})
	doc := ojson.NewDoc(data)
	keys := make([]string, 0, len(legacy))
	for key := range legacy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := ojson.Escape(key)
		if counts[key] > 1 {
			if err := DeleteJSONMember(doc, path); err != nil {
				return nil, err
			}
		}
		if err := doc.Set(path, legacy[key]); err != nil {
			return nil, err
		}
		recorder.recordOperation("set", key, "", "", legacy[key])
	}
	for _, op := range ops {
		if op.Mode == "set" {
			if op.KeepOrigin && doc.Get(op.Path).Exists() {
				continue
			}
			if err := doc.Set(op.Path, op.Value); err != nil {
				return nil, err
			}
			recorder.recordOperation("set", op.Path, "", "", op.Value)
		} else {
			if err := doc.Delete(op.Path); err != nil {
				return nil, err
			}
			recorder.recordOperation("delete", op.Path, "", "", nil)
		}
	}
	return doc.Bytes(), nil
}
