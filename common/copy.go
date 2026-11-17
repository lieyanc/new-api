package common

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/jinzhu/copier"
)

func DeepCopy[T any](src *T) (*T, error) {
	if src == nil {
		return nil, fmt.Errorf("copy source cannot be nil")
	}
	var dst T
	err := copier.CopyWithOption(&dst, src, copier.Option{
		DeepCopy:    true,
		IgnoreEmpty: true,
		Converters: []copier.TypeConverter{{
			SrcType: json.RawMessage{},
			DstType: json.RawMessage{},
			Fn: func(src interface{}) (interface{}, error) {
				// Raw JSON can contain large base64 images. Copy the bytes in bulk
				// instead of reflecting over every byte, retaining retry isolation.
				return json.RawMessage(bytes.Clone(src.(json.RawMessage))), nil
			},
		}},
	})
	if err != nil {
		return nil, err
	}
	return &dst, nil
}
