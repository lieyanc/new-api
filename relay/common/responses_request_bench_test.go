package common

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/jinzhu/copier"
)

// Synthetic JSON only: 25 base64-shaped image strings, never sent upstream.
// Match the size of the observed request without retaining any user data.
func responsesBenchmarkFixture(b *testing.B) string {
	b.Helper()
	const bodySize = 71674579
	prefix := `{"model":"test","stream":true,"service_tier":"priority","input":[`
	suffix := `]}`
	headers := make([]string, 25)
	const tail = `"}]}`
	overhead := len(prefix) + len(suffix)
	for i := range headers {
		comma := ""
		if i > 0 {
			comma = ","
		}
		headers[i] = fmt.Sprintf(`%s{"type":"custom_tool_call_output","call_id":"synthetic-%d","output":[{"type":"input_image","image_url":"data:image/png;base64,`, comma, i)
		overhead += len(headers[i]) + len(tail)
	}
	path := filepath.Join(b.TempDir(), "synthetic.json")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	write := func(data []byte) {
		if _, err := f.Write(data); err != nil {
			b.Fatal(err)
		}
	}
	write([]byte(prefix))
	block := bytes.Repeat([]byte("A"), 64*1024)
	remaining := bodySize - overhead
	for i, header := range headers {
		write([]byte(header))
		n := remaining / (len(headers) - i)
		remaining -= n
		for n > 0 {
			k := min(n, len(block))
			write(block[:k])
			n -= k
		}
		write([]byte(tail))
	}
	write([]byte(suffix))
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

func runResponsesBenchmarkRequest(path string, optimized bool) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var original dto.OpenAIResponsesRequest
	if err = appcommon.Unmarshal(body, &original); err != nil {
		return err
	}
	// The file read buffer is temporary, as in disk-backed BodyStorage.Bytes().
	body = nil
	var request *dto.OpenAIResponsesRequest
	if optimized {
		request, err = appcommon.DeepCopy(&original)
	} else {
		request = new(dto.OpenAIResponsesRequest)
		err = copier.CopyWithOption(request, &original, copier.Option{DeepCopy: true, IgnoreEmpty: true})
	}
	if err != nil {
		return err
	}
	var result []byte
	if optimized {
		result, err = MarshalResponsesRequest(*request, dto.ChannelOtherSettings{}, false)
	} else {
		result, err = appcommon.Marshal(*request)
		if err == nil {
			result, err = RemoveDisabledFields(result, dto.ChannelOtherSettings{}, false)
		}
	}
	if err != nil {
		return err
	}
	if len(result) == 0 {
		return fmt.Errorf("empty body")
	}
	// Both the original request (for retries) and outbound body remain live.
	runtime.KeepAlive(original)
	runtime.KeepAlive(result)
	return nil
}

func BenchmarkResponsesBodyPipeline(b *testing.B) {
	for _, mode := range []string{"legacy", "optimized"} {
		for _, concurrency := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/concurrency=%d", mode, concurrency), func(b *testing.B) {
				path := responsesBenchmarkFixture(b)
				runtime.GC()
				b.ReportAllocs()
				b.SetBytes(int64(71674579 * concurrency))
				b.ResetTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					start := make(chan struct{})
					errs := make(chan error, concurrency)
					var wg sync.WaitGroup
					for i := 0; i < concurrency; i++ {
						wg.Add(1)
						go func() { defer wg.Done(); <-start; errs <- runResponsesBenchmarkRequest(path, mode == "optimized") }()
					}
					close(start)
					wg.Wait()
					close(errs)
					for err := range errs {
						if err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}
