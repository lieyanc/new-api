package common

import (
	"fmt"
	"strings"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

func TestMarshalResponsesRequestFilteringCompatibility(t *testing.T) {
	global := model_setting.GetGlobalSettings()
	previous := global.PassThroughRequestEnabled
	global.PassThroughRequestEnabled = false
	t.Cleanup(func() { global.PassThroughRequestEnabled = previous })
	for flags := 0; flags < 16; flags++ {
		settings := dto.ChannelOtherSettings{AllowServiceTier: flags&1 != 0, DisableStore: flags&2 != 0, AllowSafetyIdentifier: flags&4 != 0, AllowIncludeObfuscation: flags&8 != 0}
		for _, options := range []string{`null`, `{}`, `{"include_usage":true}`, `{"include_obfuscation":true}`, `{"include_usage":true,"include_obfuscation":true}`} {
			t.Run(fmt.Sprintf("flags=%d/options=%s", flags, options), func(t *testing.T) {
				var req dto.OpenAIResponsesRequest
				raw := `{"model":"test","input":[{"service_tier":"nested","store":true}],"service_tier":"priority","store":false,"safety_identifier":"user","stream":false,"temperature":0,"max_output_tokens":0,"stream_options":` + options + `}`
				if err := appcommon.Unmarshal([]byte(raw), &req); err != nil {
					t.Fatal(err)
				}
				before, err := appcommon.Marshal(req)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := RemoveDisabledFields(before, settings, false)
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []any{req, &req} {
					actual, err := MarshalResponsesRequest(value, settings, false)
					if err != nil {
						t.Fatal(err)
					}
					assertJSONEqual(t, string(expected), string(actual))
				}
				after, err := appcommon.Marshal(req)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("filter mutated caller's request")
				}
			})
		}
	}
}

func TestMarshalResponsesRequestPassThroughAndFallback(t *testing.T) {
	global := model_setting.GetGlobalSettings()
	previous := global.PassThroughRequestEnabled
	global.PassThroughRequestEnabled = false
	t.Cleanup(func() { global.PassThroughRequestEnabled = previous })
	var req dto.OpenAIResponsesRequest
	if err := appcommon.Unmarshal([]byte(`{"model":"test","service_tier":"priority","store":false,"stream_options":{"include_obfuscation":true}}`), &req); err != nil {
		t.Fatal(err)
	}
	before, _ := appcommon.Marshal(req)
	for _, globalPass := range []bool{false, true} {
		global.PassThroughRequestEnabled = globalPass
		got, err := MarshalResponsesRequest(req, dto.ChannelOtherSettings{DisableStore: true}, !globalPass)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, string(before), string(got))
	}
	global.PassThroughRequestEnabled = false
	fallback := map[string]any{"model": "test", "inference_geo": "us", "speed": "fast", "service_tier": "priority", "custom": map[string]any{"store": true}}
	got, err := MarshalResponsesRequest(fallback, dto.ChannelOtherSettings{}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, `{"model":"test","custom":{"store":true}}`, string(got))
	var nilRequest *dto.OpenAIResponsesRequest
	got, err = MarshalResponsesRequest(nilRequest, dto.ChannelOtherSettings{}, false)
	if err != nil || string(got) != "null" {
		t.Fatalf("nil request: %s, %v", got, err)
	}
}

func TestMarshalResponsesRequestPreservesRawValues(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	if err := appcommon.Unmarshal([]byte(`{"model":"test","input":[{"id":9007199254740993,"value":null,"enabled":false}],"metadata":{"n":9007199254740993}}`), &req); err != nil {
		t.Fatal(err)
	}
	got, err := MarshalResponsesRequest(req, dto.ChannelOtherSettings{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "9007199254740993") != 2 {
		t.Fatalf("raw JSON numbers lost precision: %s", got)
	}
	// Malformed raw payloads must still fail instead of producing an invalid body.
	req.Input = []byte(`{"broken"`)
	if _, err := MarshalResponsesRequest(req, dto.ChannelOtherSettings{}, false); err == nil {
		t.Fatal("invalid input accepted")
	}
}
