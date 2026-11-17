package common_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func TestDeepCopyResponsesRetryIsolation(t *testing.T) {
	var original dto.OpenAIResponsesRequest
	if err := common.Unmarshal([]byte(`{"model":"test","input":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}],"tools":[{"type":"function","name":"example"}],"reasoning":{"effort":"low"},"stream":false,"temperature":0,"max_output_tokens":0,"store":false,"stream_options":{"include_usage":true,"include_obfuscation":true}}`), &original); err != nil {
		t.Fatal(err)
	}
	before, err := common.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := common.DeepCopy(&original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, *clone) {
		t.Fatal("clone changed request values, including explicit zero/false")
	}
	clone.Input[0] = ' '
	clone.Tools[0] = ' '
	clone.Store[0] = ' '
	clone.Reasoning.Effort = "high"
	*clone.Stream = true
	*clone.Temperature = 1
	*clone.MaxOutputTokens = 10
	clone.StreamOptions.IncludeUsage = false
	after, err := common.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("channel attempt modified the original request used by retries")
	}
	retry, err := common.DeepCopy(&original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, *retry) {
		t.Fatal("retry did not receive the original request")
	}
}

func TestDeepCopyNestedRawMessages(t *testing.T) {
	type item struct{ Data json.RawMessage }
	type request struct {
		Items   []item
		Fields  map[string]json.RawMessage
		Missing json.RawMessage
	}
	original := request{Items: []item{{Data: json.RawMessage(`false`)}}, Fields: map[string]json.RawMessage{"x": json.RawMessage(`null`)}}
	clone, err := common.DeepCopy(&original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, *clone) {
		t.Fatal("nested values or nil semantics changed")
	}
	clone.Items[0].Data[0] = 'x'
	clone.Fields["x"][0] = 'x'
	if string(original.Items[0].Data) != "false" || string(original.Fields["x"]) != "null" {
		t.Fatal("nested raw JSON shares mutable storage")
	}
	if _, err := common.DeepCopy[request](nil); err == nil {
		t.Fatal("nil source must return an error")
	}
}
