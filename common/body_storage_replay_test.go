package common

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func bodyTestConfig(t *testing.T, disk bool) {
	t.Helper()
	previous := GetDiskCacheConfig()
	SetDiskCacheConfig(DiskCacheConfig{Enabled: disk, ThresholdMB: 0, MaxSizeMB: 256, Path: t.TempDir()})
	t.Cleanup(func() { SetDiskCacheConfig(previous) })
}

func TestBodyReplayIndependentCursors(t *testing.T) {
	for _, disk := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "disk"}[disk], func(t *testing.T) {
			bodyTestConfig(t, disk)
			input := bytes.Repeat([]byte("0123456789abcdef"), 4096)
			storage, err := CreateBodyStorage(input)
			if err != nil {
				t.Fatal(err)
			}
			defer storage.Close()
			if storage.IsDisk() != disk {
				t.Fatal("unexpected storage mode")
			}
			first := NewReplayableBodyReader(storage)
			defer first.(io.Closer).Close()
			prefix := make([]byte, 137)
			if _, err = io.ReadFull(first, prefix); err != nil {
				t.Fatal(err)
			}
			second := NewReplayableBodyReader(storage)
			defer second.(io.Closer).Close()
			_, _ = storage.Seek(storage.Size()/2, io.SeekStart)
			var wg sync.WaitGroup
			wg.Add(2)
			for _, tc := range []struct {
				r    io.Reader
				want []byte
			}{{first, input[137:]}, {second, input}} {
				go func() {
					defer wg.Done()
					got, e := io.ReadAll(tc.r)
					if e != nil || !bytes.Equal(got, tc.want) {
						t.Errorf("interleaved replay lost bytes: %v", e)
					}
				}()
			}
			wg.Wait()
			if err = first.(io.Closer).Close(); err != nil {
				t.Fatal(err)
			}
			replay, err := first.NewReader()
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(replay)
			_ = replay.Close()
			if err != nil || !bytes.Equal(got, input) {
				t.Fatalf("replay after transport close: %v", err)
			}
			_ = storage.Close()
			if _, err = storage.NewReader(); !errors.Is(err, ErrStorageClosed) {
				t.Fatalf("new reader after owner close: %v", err)
			}
		})
	}
}

type noBodyBytes struct{ BodyStorage }

func (noBodyBytes) Bytes() ([]byte, error) { return nil, errors.New("whole-body read is forbidden") }

func TestDiskBodyDecodeCompatibility(t *testing.T) {
	bodyTestConfig(t, true)
	for _, raw := range []string{
		`{"model":"first","model":"last","value":9007199254740993,"stream":false,"temperature":0,"input":[{"image_url":"data:image/png;base64,AAAA"}]}`,
		`{"model":"escaped\u0020name","unknown":{"deep":[1,true,null]},"input":null}`,
		`null`,
	} {
		t.Run(raw[:min(len(raw), 30)], func(t *testing.T) {
			type request struct {
				Model       string
				Input       []map[string]any
				Stream      *bool
				Temperature *float64
				Value       uint64
			}
			var expected, actual request
			if err := Unmarshal([]byte(raw), &expected); err != nil {
				t.Fatal(err)
			}
			storage, err := CreateBodyStorage([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			defer storage.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(KeyBodyStorage, noBodyBytes{storage})
			if err = UnmarshalBodyReusable(c, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(expected, actual) {
				t.Fatalf("different decode: %#v != %#v", expected, actual)
			}
			replay, _ := io.ReadAll(c.Request.Body)
			if string(replay) != raw {
				t.Fatal("body was not reset for retries")
			}
		})
	}
	for _, raw := range []string{`{} {}`, `{} null`, `{} garbage`, `{"model":`, ``} {
		f, err := os.CreateTemp(t.TempDir(), "json")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(raw)
		_, _ = f.Seek(0, io.SeekStart)
		var v map[string]any
		err = DecodeJsonBody(f, &v)
		_ = f.Close()
		if err == nil {
			t.Fatalf("accepted trailing/invalid input %q", raw)
		}
	}
}

func TestDiskBodySkipsLargeUnusedField(t *testing.T) {
	bodyTestConfig(t, true)
	raw := `{"input":"` + strings.Repeat("A", 2<<20) + `","model":"test","group":"default"}`
	storage, err := CreateBodyStorage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(KeyBodyStorage, noBodyBytes{storage})
	var meta struct{ Model, Group string }
	if err = UnmarshalBodyReusable(c, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Model != "test" || meta.Group != "default" {
		t.Fatalf("metadata: %+v", meta)
	}
}
