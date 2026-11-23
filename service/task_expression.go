package service

import (
	"fmt"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"
)

// TaskExpressionPrice freezes the rate and the adapter's estimated quantities.
// A provider-only fact is first available on completion. Reserve the normal
// minimum in that case and evaluate the real amount when polling succeeds.
func TaskExpressionPrice(info *relaycommon.RelayInfo, expr string, group types.GroupRatioInfo, facts map[string]any) (types.PriceData, error) {
	vars := billingexpr.UsedVars(expr)
	if billingexpr.UsesChatQuantities(expr) || vars["u"] && billingexpr.UsesFixedPricing(expr) {
		return types.PriceData{}, fmt.Errorf("task pricing cannot mix token, fixed and usage units")
	}
	snap := &billingexpr.BillingSnapshot{ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), ModelName: info.OriginModelName, BillingMode: "tiered_expr", GroupRatio: group.GroupRatio, QuotaPerUnit: common.QuotaPerUnit, TaskUsageBilling: vars["u"], UsageFacts: facts}
	input := billingexpr.RequestInput{Usage: facts, Headers: info.RequestHeaders}
	quota := 0
	missing := false
	for key := range billingexpr.UsedUsageKeys(expr) {
		if facts[key] == nil {
			missing = true
		}
	}
	if missing {
		quota = billingexpr.QuotaRound(float64(common.PreConsumedQuota) * group.GroupRatio)
	} else {
		result, err := billingexpr.ComputeTieredQuotaWithRequest(snap, billingexpr.TokenParams{}, input)
		if err != nil {
			return types.PriceData{}, err
		}
		quota = result.ActualQuotaAfterGroup
	}
	snap.EstimatedQuotaAfterGroup = quota
	info.TieredBillingSnapshot = snap
	return types.PriceData{Quota: quota, GroupRatioInfo: group, FreeModel: group.GroupRatio == 0}, nil
}

func completedTaskExpressionQuota(task *model.Task, result *relaycommon.TaskInfo, snap *billingexpr.BillingSnapshot) (int, error) {
	facts := map[string]any{}
	for key, v := range snap.UsageFacts {
		facts[key] = v
	}
	usage := gjson.GetBytes(task.Data, "usage")
	if !usage.Exists() {
		usage = gjson.GetBytes(task.Data, "data.data.usage")
	}
	raw := usage.Map()
	for key := range billingexpr.UsedUsageKeys(snap.ExprString) {
		value := raw[key]
		if !value.Exists() {
			switch key {
			case "seconds":
				value = raw["duration"]
				if !value.Exists() {
					value = gjson.GetBytes(task.Data, "seconds")
				}
				if !value.Exists() {
					value = gjson.GetBytes(task.Data, "duration")
				}
			case "clips":
				value = raw["video_count"]
			case "tokens":
				value = raw["completion_tokens"]
				if !value.Exists() && result.TotalTokens > 0 {
					facts[key] = float64(result.TotalTokens)
				}
			case "image_count":
				value = raw["output_image_count"]
			}
		}
		switch value.Type {
		case gjson.Number:
			if value.Num < 0 || math.IsInf(value.Num, 0) {
				return 0, fmt.Errorf("invalid usage %s", key)
			}
			facts[key] = value.Num
		case gjson.String:
			if number, err := strconv.ParseFloat(value.Str, 64); err == nil && (key == "seconds" || key == "tokens" || key == "clips" || key == "image_count") {
				if number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
					return 0, fmt.Errorf("invalid usage %s", key)
				}
				facts[key] = number
			} else {
				facts[key] = value.Str
			}
		}
	}
	tr, err := billingexpr.ComputeTieredQuotaWithRequest(snap, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: facts})
	return tr.ActualQuotaAfterGroup, err
}
