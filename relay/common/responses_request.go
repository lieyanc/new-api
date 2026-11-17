package common

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

// MarshalResponsesRequest filters the known Responses DTO before encoding it.
// Decoding the encoded body into map[string]any merely to remove control fields
// duplicates potentially large image strings and loses precision in JSON numbers.
// Other adaptor result types retain the existing generic filtering path.
func MarshalResponsesRequest(request any, settings dto.ChannelOtherSettings, passThrough bool) ([]byte, error) {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || passThrough {
		return common.Marshal(request)
	}
	switch req := request.(type) {
	case dto.OpenAIResponsesRequest:
		return marshalFilteredResponsesRequest(req, settings)
	case *dto.OpenAIResponsesRequest:
		if req != nil {
			return marshalFilteredResponsesRequest(*req, settings)
		}
	}
	data, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return RemoveDisabledFields(data, settings, passThrough)
}

func marshalFilteredResponsesRequest(req dto.OpenAIResponsesRequest, settings dto.ChannelOtherSettings) ([]byte, error) {
	// req is a value copy. Do not mutate any data shared with the caller.
	if !settings.AllowServiceTier {
		req.ServiceTier = ""
	}
	if settings.DisableStore {
		req.Store = nil
	}
	if !settings.AllowSafetyIdentifier {
		req.SafetyIdentifier = nil
	}
	if !settings.AllowIncludeObfuscation && req.StreamOptions != nil {
		options := *req.StreamOptions
		options.IncludeObfuscation = false
		// Match RemoveDisabledFields: omit stream_options if it becomes {}.
		if options == (dto.StreamOptions{}) {
			req.StreamOptions = nil
		} else {
			req.StreamOptions = &options
		}
	}
	return common.Marshal(req)
}
