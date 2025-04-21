/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gateway

import (
	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	jsoniter "github.com/json-iterator/go"
	"github.com/scitix/arks/pkg/gateway/qosconfig"
	"github.com/scitix/arks/pkg/gateway/ratelimiter"
)

// jsonfast is a faster json parser
var jsonfast = jsoniter.ConfigFastest

func jsonUnmarshal(data []byte, v interface{}) error {
	return jsonfast.Unmarshal(data, v)
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return jsonfast.Marshal(v)
}

// generateErrorResponse construct envoy proxy error response
func generateErrorResponse(statusCode envoyTypePb.StatusCode, headers []*configPb.HeaderValueOption, body string) *extProcPb.ProcessingResponse {
	// Set the Content-Type header to application/json
	if headers == nil {
		headers = make([]*configPb.HeaderValueOption, 0)
	}
	headers = append(headers, &configPb.HeaderValueOption{
		Header: &configPb.HeaderValue{
			Key:   "Content-Type",
			Value: "application/json",
		},
	})

	return &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extProcPb.ImmediateResponse{
				Status: &envoyTypePb.HttpStatus{
					Code: statusCode,
				},
				Headers: &extProcPb.HeaderMutation{
					SetHeaders: headers,
				},
				Body: generateErrorMessage(body, int(statusCode)),
			},
		},
	}
}

// generateErrorMessage constructs a JSON error message
func generateErrorMessage(message string, code int) []byte {
	errorStruct := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"code":    code,
		},
	}
	jsonData, _ := jsonMarshal(errorStruct)
	return jsonData
}

func toRateLimitRequest(trl *qosconfig.UserQos, ruleName string, limit int64, request int64) *ratelimiter.RateLimitRequest {
	return &ratelimiter.RateLimitRequest{
		Identifier: []ratelimiter.Label{
			{
				Key:   "namespace",
				Value: trl.Namespace,
			},
			{
				Key:   "user",
				Value: trl.User,
			},
			{
				Key:   "model",
				Value: trl.Model,
			},
		},
		RuleName: ruleName,
		Limit:    limit,
		Request:  request,
	}
}
