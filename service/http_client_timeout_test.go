package service

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func TestResponseHeaderTimeoutConfiguration(t *testing.T) {
	previous := common.RelayResponseHeaderTimeout
	oldClient := httpClient
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = previous; httpClient = oldClient; ResetProxyClientCache() })
	for _, seconds := range []int{0, -1, 37} {
		common.RelayResponseHeaderTimeout = seconds
		want := time.Duration(max(seconds, 0)) * time.Second
		InitHttpClient()
		if got := httpClient.Transport.(*http.Transport).ResponseHeaderTimeout; got != want {
			t.Fatalf("default timeout: %s", got)
		}
		ResetProxyClientCache()
		for _, proxyURL := range []string{"http://127.0.0.1:9", "https://127.0.0.1:9", "socks5://127.0.0.1:9", "socks5h://127.0.0.1:9"} {
			client, err := NewProxyHttpClient(proxyURL)
			if err != nil {
				t.Fatal(err)
			}
			if got := client.Transport.(*http.Transport).ResponseHeaderTimeout; got != want {
				t.Fatalf("%s timeout: %s", proxyURL, got)
			}
		}
	}
	if strconv.IntSize == 64 {
		common.RelayResponseHeaderTimeout = int(^uint(0) >> 1)
		if got := relayResponseHeaderTimeout(); got <= 0 || got/time.Second != time.Duration((1<<63-1)/int64(time.Second)) {
			t.Fatalf("timeout overflow: %s", got)
		}
	}
}

func TestResponseHeaderTimeoutDoesNotLimitStreaming(t *testing.T) {
	const timeout = 60 * time.Millisecond
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/headers" {
			<-release
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(3 * timeout)
		_, _ = io.WriteString(w, "complete")
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: timeout}}
	defer client.CloseIdleConnections()
	_, err := client.Get(server.URL + "/headers")
	close(release)
	if err == nil {
		t.Fatal("unresponsive upstream did not time out")
	}
	if timeoutErr, ok := err.(net.Error); !ok || !timeoutErr.Timeout() {
		t.Fatalf("wrong error: %v", err)
	}
	resp, err := client.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(got) != "complete" {
		t.Fatalf("stream after headers was interrupted: %s, %v", got, err)
	}
}
