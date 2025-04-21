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
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/scitix/arks/pkg/gateway/qosconfig"
	"k8s.io/klog/v2"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

func (s *Server) HandleRequestHeaders(ctx context.Context, requestID string, req *extProcPb.ProcessingRequest) (resp *extProcPb.ProcessingResponse, token string) {
	klog.InfoS("-- In RequestHeaders processing ...", "requestID", requestID)

	// Get API Key from Authorization header
	h := req.Request.(*extProcPb.ProcessingRequest_RequestHeaders)
	for _, n := range h.RequestHeaders.Headers.Headers {
		if strings.ToLower(n.Key) == "authorization" {
			auth := string(n.RawValue)
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
				break
			}
		}
	}

	if token == "" {
		klog.ErrorS(nil, "no token found in request headers", "requestID", requestID)
		resp = generateErrorResponse(
			envoyTypePb.StatusCode_Unauthorized,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorToken, RawValue: []byte("true"),
			}}}, "no token found in request headers")
		return
	}

	// TODO: add username, namespace to header

	// Mark that request has passed header processing stage
	resp = &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extProcPb.HeadersResponse{
				Response: &extProcPb.CommonResponse{
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: []*configPb.HeaderValueOption{
							{
								Header: &configPb.HeaderValue{
									Key:      HeaderWentIntoReqHeaders,
									RawValue: []byte("true"),
								},
							},
						},
					},
					ClearRouteCache: true,
				},
			},
		},
	}
	return
}

func (s *Server) HandleRequestBody(ctx context.Context, requestID string, req *extProcPb.ProcessingRequest, token string) (resp *extProcPb.ProcessingResponse, qos *qosconfig.UserQos, model string, stream bool) {
	klog.InfoS("-- In RequestBody processing ...", "requestID", requestID)
	var err error
	// parse request body
	var reqBody struct {
		Model         string `json:"model"`
		Stream        *bool  `json:"stream,omitempty"`
		StreamOptions *struct {
			IncludeUsage *bool `json:"include_usage,omitempty"`
		} `json:"stream_options,omitempty"`
	}

	body := req.Request.(*extProcPb.ProcessingRequest_RequestBody)

	if err = jsonUnmarshal(body.RequestBody.GetBody(), &reqBody); err != nil {
		klog.ErrorS(err, "error to unmarshal request body", "requestID", requestID)
		resp = generateErrorResponse(envoyTypePb.StatusCode_BadRequest,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorRequestBodyProcessing, RawValue: []byte("true")}}},
			"error processing request body")
		return
	}

	model = reqBody.Model
	// check model
	if model == "" {
		klog.ErrorS(nil, "model error in request", "requestID", requestID, "jsonMap", reqBody)
		resp = generateErrorResponse(envoyTypePb.StatusCode_BadRequest,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorNoModelInRequest, RawValue: []byte(model)}}},
			"no model in request body")
		return
	}

	// check qos exists
	qos, err = s.configProvider.GetQosByToken(ctx, token, model)
	if err != nil {
		klog.ErrorS(err, "error to get qos by token", "requestID", requestID, "token", token, "model", model)
		resp = generateErrorResponse(envoyTypePb.StatusCode_InternalServerError,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorToken, RawValue: []byte(err.Error())}}},
			"error to get qos by token")
		return
	}
	if qos == nil {
		klog.ErrorS(nil, "qos not found", "requestID", requestID, "token", token, "model", model)
		resp = generateErrorResponse(envoyTypePb.StatusCode_Unauthorized,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorToken, RawValue: []byte("token error")}}},
			"token error")
		return
	}

	// check model exists
	modelList, err := s.configProvider.GetModelList(ctx, qos.Namespace)
	if err != nil {
		klog.ErrorS(err, "error to get model list", "requestID", requestID, "namespace", qos.Namespace)
		resp = generateErrorResponse(envoyTypePb.StatusCode_InternalServerError,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorNoModelBackends, RawValue: []byte(model)}}},
			fmt.Sprintf("model %s does not exist", model))
		return
	}

	if !slices.Contains(modelList, model) {
		klog.ErrorS(nil, "model doesn't exist in cache, probably wrong model name", "requestID", requestID, "model", model)
		resp = generateErrorResponse(envoyTypePb.StatusCode_BadRequest,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorNoModelBackends, RawValue: []byte(model)}}},
			fmt.Sprintf("model %s does not exist", model))
		return
	}

	if reqBody.Stream != nil {
		stream = *reqBody.Stream
	}

	// check stream_options include_usage set to true
	// TODO: When stream_options is set, response will include usage information. Should we modify the request body here?
	if stream {
		if reqBody.StreamOptions == nil || reqBody.StreamOptions.IncludeUsage == nil || !*reqBody.StreamOptions.IncludeUsage {
			klog.ErrorS(nil, "stream_options include_usage not set to true", "requestID", requestID, "model", model)
			resp = generateErrorResponse(envoyTypePb.StatusCode_BadRequest,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorStreamOptionsIncludeUsage, RawValue: []byte("include_usage for stream_options not set")}}},
				"no stream with usage option available")
			return
		}
	}

	// check rate limit
	errRes, err := s.checkRateLimit(ctx, qos)
	if err != nil {
		klog.ErrorS(err, "error on checking rate limits", "requestID", requestID, "token", token, "model", model)
	}
	if errRes != nil {
		klog.InfoS("check rate limit result", "requestID", requestID, "token", token, "model", model, "result", errRes.GetResponseBody())
		resp = errRes
		return
	}

	// check quota
	if qos.QuotaName != "" {
		errResp, err := s.checkTokenQuotaLimit(ctx, qos)
		if err != nil {
			klog.ErrorS(err, "error on checking quota limits", "requestID", requestID, "token", token, "model", model)
		}
		if errResp != nil {
			klog.InfoS("check quota limit result", "requestID", requestID, "token", token, "model", model, "result", errRes.GetResponseBody())
			resp = errResp
			return
		}
	}

	// request limit
	err = s.doRequestRateLimit(ctx, qos)
	if err != nil {
		klog.ErrorS(err, "error on request limit", "requestID", requestID, "token", token, "model", model)
		resp = generateErrorResponse(envoyTypePb.StatusCode_InternalServerError,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorRateLimit, RawValue: []byte("rate limit error")}}},
			"rate limit error")
		return
	}

	// add model to header
	headers := []*configPb.HeaderValueOption{}

	headers = append(headers, &configPb.HeaderValueOption{
		Header: &configPb.HeaderValue{
			Key:      "model",
			RawValue: []byte(model),
		},
	})
	// add namespace to header
	headers = append(headers, &configPb.HeaderValueOption{
		Header: &configPb.HeaderValue{
			Key:      "namespace",
			RawValue: []byte(qos.Namespace),
		},
	})
	// add username to header
	headers = append(headers, &configPb.HeaderValueOption{
		Header: &configPb.HeaderValue{
			Key:      "username",
			RawValue: []byte(qos.User),
		},
	})

	klog.InfoS("request start", "requestID", requestID, "model", model, "qos", qos)

	// TODO: trace term
	// term = s.cache.AddRequestCount(requestID, model)

	resp = &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_RequestBody{
			RequestBody: &extProcPb.BodyResponse{
				Response: &extProcPb.CommonResponse{
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: headers,
					},
				},
			},
		},
	}
	return
}
