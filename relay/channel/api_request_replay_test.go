package channel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func TestReplayableRequestRedirect(t *testing.T) {
	previous := appcommon.GetDiskCacheConfig()
	t.Cleanup(func() { appcommon.SetDiskCacheConfig(previous) })
	input := []byte(`{"input":"same bytes after 307","stream":false}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, input) || r.ContentLength != int64(len(input)) {
			t.Errorf("bad replay: bytes=%s length=%d err=%v", got, r.ContentLength, err)
		}
		if r.URL.Path == "/first" {
			w.Header().Set("Location", "/second")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	for _, disk := range []bool{false, true} {
		appcommon.SetDiskCacheConfig(appcommon.DiskCacheConfig{Enabled: disk, ThresholdMB: 0, MaxSizeMB: 256, Path: t.TempDir()})
		body, owner, err := relaycommon.NewOutboundJSONBody(input)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest("POST", srv.URL+"/first", body)
		if err != nil {
			t.Fatal(err)
		}
		configureReplayableBody(req, body)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = owner.Close()
		if err != nil || string(got) != "ok" {
			t.Fatalf("redirect failed: %v", err)
		}
	}
}

type replayRoundTripper func(*http.Request) (*http.Response, error)

func (f replayRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type closeTracker struct {
	io.Reader
	closed bool
}

func (r *closeTracker) Close() error { r.closed = true; return nil }

func TestReplayableRequestNotClosedOnEarlyHeaders(t *testing.T) {
	service.InitHttpClient()
	client := service.GetHttpClient()
	previous := client.Transport
	t.Cleanup(func() { client.Transport = previous })
	body := &closeTracker{Reader: strings.NewReader("still sending")}
	client.Transport = replayRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	req, _ := http.NewRequest("POST", "http://upstream.invalid/", body)
	resp, err := doRequest(c, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if body.closed {
		t.Fatal("request body closed while transport may still be uploading")
	}
}
