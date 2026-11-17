package common

import (
	"bytes"
	"io"
	"os"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func TestOutboundBodyDiskLifetime(t *testing.T) {
	previous := appcommon.GetDiskCacheConfig()
	appcommon.SetDiskCacheConfig(appcommon.DiskCacheConfig{Enabled: true, ThresholdMB: 0, MaxSizeMB: 256, Path: t.TempDir()})
	t.Cleanup(func() { appcommon.SetDiskCacheConfig(previous) })
	input := []byte(`{"model":"test","input":"base64-shaped"}`)
	body, owner, err := NewOutboundJSONBody(input)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if body.Size() != int64(len(input)) {
		t.Fatal("wrong content length")
	}
	entries, err := os.ReadDir(appcommon.GetDiskCacheDir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected disk body: %v", err)
	}
	got, err := io.ReadAll(body)
	if err != nil || !bytes.Equal(got, input) {
		t.Fatalf("body: %s %v", got, err)
	}
	_ = body.(io.Closer).Close()
	replay, err := body.NewReader()
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(replay)
	_ = replay.Close()
	if err != nil || !bytes.Equal(got, input) {
		t.Fatal("transport close broke replay")
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(appcommon.GetDiskCacheDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("cache file leaked: %v", err)
	}
}

func TestDisabledFieldEditsPreserveRawPayload(t *testing.T) {
	// Unknown payload values must retain precision, key order and raw spelling.
	input := []byte(`{"input":{"b":9007199254740993,"a":1e+09},"service_tier":"priority","service_tier":"flex","stream_options":{"include_obfuscation":true},"store":false}`)
	got, err := RemoveDisabledFields(input, dto.ChannelOtherSettings{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`"input":{"b":9007199254740993,"a":1e+09}`)) {
		t.Fatalf("payload rewritten: %s", got)
	}
	if bytes.Contains(got, []byte("service_tier")) || bytes.Contains(got, []byte("stream_options")) {
		t.Fatalf("disabled field survived: %s", got)
	}
	if !bytes.Contains(input, []byte("priority")) {
		t.Fatal("input mutated")
	}
	emptyOptions := []byte(`{"stream_options":{},"input":null}`)
	got, err = RemoveDisabledFields(emptyOptions, dto.ChannelOtherSettings{}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, `{"input":null}`, string(got))
}
