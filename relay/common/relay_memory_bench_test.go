package common

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

type preparedMemoryRequest struct {
	original dto.OpenAIResponsesRequest
	body     io.Reader
	incoming appcommon.BodyStorage
	outgoing io.Closer
}

func (r *preparedMemoryRequest) close() {
	if r.outgoing != nil {
		_ = r.outgoing.Close()
	}
	if r.incoming != nil {
		_ = r.incoming.Close()
	}
}

// Compare this branch before/after the storage and override backport. Both sides
// use the phase-one fast clone and typed filter. No real upstream is contacted.
func prepareMemoryRequest(path string, backport bool) (out *preparedMemoryRequest, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	storage, err := appcommon.CreateBodyStorageFromReader(f, st.Size(), 128<<20)
	if err != nil {
		return nil, err
	}
	out = &preparedMemoryRequest{incoming: storage}
	defer func() {
		if err != nil {
			out.close()
		}
	}()
	var meta struct{ Model string }
	if backport {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(appcommon.KeyBodyStorage, storage)
		if err = appcommon.UnmarshalBodyReusable(c, &meta); err != nil {
			return out, err
		}
		if err = appcommon.UnmarshalBodyReusable(c, &out.original); err != nil {
			return out, err
		}
	} else {
		var data []byte
		data, err = storage.Bytes()
		if err != nil {
			return out, err
		}
		if err = appcommon.Unmarshal(data, &meta); err != nil {
			return out, err
		}
		data, err = storage.Bytes()
		if err != nil {
			return out, err
		}
		if err = appcommon.Unmarshal(data, &out.original); err != nil {
			return out, err
		}
	}
	request, err := appcommon.DeepCopy(&out.original)
	if err != nil {
		return out, err
	}
	data, err := MarshalResponsesRequest(*request, dto.ChannelOtherSettings{}, false)
	if err != nil {
		return out, err
	}
	overrides := map[string]interface{}{"store": false, "temperature": 0, "max_output_tokens": 4096}
	if backport {
		data, err = ApplyParamOverride(data, overrides, nil)
	} else {
		var fields map[string]interface{}
		if err = appcommon.Unmarshal(data, &fields); err != nil {
			return out, err
		}
		for k, v := range overrides {
			fields[k] = v
		}
		data, err = appcommon.Marshal(fields)
	}
	if err != nil {
		return out, err
	}
	if backport {
		out.body, out.outgoing, err = NewOutboundJSONBody(data)
	} else {
		out.body = bytes.NewReader(data)
	}
	return out, err
}

func BenchmarkLargeResponsesRelayMemory(b *testing.B) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"phase1", "backport"} {
		for _, concurrency := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/concurrency=%d", mode, concurrency), func(b *testing.B) {
				previous := appcommon.GetDiskCacheConfig()
				appcommon.SetDiskCacheConfig(appcommon.DiskCacheConfig{Enabled: true, ThresholdMB: 1, MaxSizeMB: 4096, Path: b.TempDir()})
				b.Cleanup(func() { appcommon.SetDiskCacheConfig(previous) })
				path := responsesBenchmarkFixture(b)
				runtime.GC()
				runtime.GC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)
				b.ReportAllocs()
				b.SetBytes(int64(71674579 * concurrency))
				b.ResetTimer()
				var held uint64
				for n := 0; n < b.N; n++ {
					type result struct {
						request *preparedMemoryRequest
						err     error
					}
					ready := make(chan result, concurrency)
					start := make(chan struct{})
					for i := 0; i < concurrency; i++ {
						go func() { <-start; r, e := prepareMemoryRequest(path, mode == "backport"); ready <- result{r, e} }()
					}
					close(start)
					requests := make([]*preparedMemoryRequest, 0, concurrency)
					var firstErr error
					for i := 0; i < concurrency; i++ {
						r := <-ready
						if r.err != nil {
							firstErr = r.err
						} else {
							requests = append(requests, r.request)
						}
					}
					if firstErr != nil {
						for _, r := range requests {
							r.close()
						}
						b.Fatal(firstErr)
					}
					// Simulate outstanding upstream requests: originals and outbound bodies
					// stay live. Remove transient buffers/pools before measuring retained heap.
					runtime.GC()
					runtime.GC()
					var current runtime.MemStats
					runtime.ReadMemStats(&current)
					if current.HeapAlloc > before.HeapAlloc {
						held += current.HeapAlloc - before.HeapAlloc
					}
					for _, r := range requests {
						if _, err := io.Copy(io.Discard, r.body); err != nil {
							r.close()
							b.Fatal(err)
						}
						runtime.KeepAlive(r.original)
						r.close()
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(held)/float64(b.N)/(1<<20), "held-MiB/batch")
			})
		}
	}
}
