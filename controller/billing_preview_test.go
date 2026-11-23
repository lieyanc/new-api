package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBillingPreviewUsesSettlementUnitsAndRequestRules(t *testing.T) {
	for _, tc := range []struct {
		name, expr string
		input      map[string]any
		dollars    float64
	}{
		{"cache", `tier("standard",img_cr*2)`, map[string]any{"tokens": map[string]int{"ImgCR": 200}}, 0.0004},
		{"fixed", `tier("request",fixed(0.00075))`, nil, 0.00075},
		{"images", `tier("image",fixed(0.04))*image_count`, map[string]any{"image_count": 3}, 0.12},
		{"usage", `tier("base",u("seconds")*0.4)`, map[string]any{"usage": map[string]int{"seconds": 5}}, 2},
		{"legacy", `v1:tier("中文|||has",fixed(0.01))|||when(header("anthropic-beta") has "fast-mode") * 6`, map[string]any{"headers": map[string]string{"Anthropic-Beta": "fast-mode"}}, 0.06},
		{"time", `weekday("Asia/Shanghai")>=0 && hour("Asia/Shanghai")<24 ? tier("now",fixed(0.1)) : tier("other",fixed(0))`, nil, 0.1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.input
			if input == nil {
				input = map[string]any{}
			}
			input["expression"] = tc.expr
			b, err := common.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/option/billing_preview", strings.NewReader(string(b)))
			PreviewBillingExpression(c)
			var got struct {
				Success bool
				Data    struct{ Cost float64 }
			}
			if err := common.Unmarshal(w.Body.Bytes(), &got); err != nil || !got.Success || got.Data.Cost != tc.dollars*common.QuotaPerUnit {
				t.Fatalf("response %s, %v", w.Body, err)
			}
		})
	}
}
