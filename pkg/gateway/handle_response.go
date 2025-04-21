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
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/arks-ai/arks/pkg/gateway/qosconfig"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
	"k8s.io/klog/v2"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

func (s *Server) HandleResponseHeaders(ctx context.Context, requestID string, req *extProcPb.ProcessingRequest) (*extProcPb.ProcessingResponse, int) {
	klog.InfoS("-- In ResponseHeaders processing ...", "requestID", requestID)
	b := req.Request.(*extProcPb.ProcessingRequest_ResponseHeaders)

	headers := []*configPb.HeaderValueOption{{
		Header: &configPb.HeaderValue{
			Key:      HeaderWentIntoRespHeaders,
			RawValue: []byte("true"),
		},
	}}

	var statusCode int

	for _, headerValue := range b.ResponseHeaders.Headers.Headers {
		if headerValue.Key == ":status" {
			statusCode, _ = strconv.Atoi(string(headerValue.RawValue))
			// if code != 200 {
			// 	isProcessingError = true
			// 	statusCode = code
			// }
		}
		headers = append(headers, &configPb.HeaderValueOption{
			Header: &configPb.HeaderValue{
				Key:      headerValue.Key,
				RawValue: headerValue.RawValue,
			},
		})
	}

	return &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extProcPb.HeadersResponse{
				Response: &extProcPb.CommonResponse{
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: headers,
					},
					ClearRouteCache: true,
				},
			},
		},
	}, statusCode
}

func (s *Server) HandleResponseBody(ctx context.Context, requestID string, req *extProcPb.ProcessingRequest, qos *qosconfig.UserQos, model string, stream bool, hasCompleted bool) (*extProcPb.ProcessingResponse, bool) {
	// Get response body data
	b := req.Request.(*extProcPb.ProcessingRequest_ResponseBody)
	klog.InfoS("-- In ResponseBody processing ...", "requestID", requestID, "endOfStream", b.ResponseBody.EndOfStream)
	pt, _ := s.processingTimes.LoadOrStore(requestID, &processingTime{})
	timing := pt.(*processingTime)
	if timing.startTime.IsZero() {
		timing.startTime = time.Now()
	}
	// Initialize variables
	var res struct {
		Model string                 `json:"model"`
		Usage openai.CompletionUsage `json:"usage"`
	}
	var usage openai.CompletionUsage
	var promptTokens, completionTokens, totalTokens int64
	var headers []*configPb.HeaderValueOption
	complete := hasCompleted

	// TODO: Handle request tracing
	defer func() {
		elapsed := time.Since(timing.startTime)
		timing.duration += elapsed
		if !hasCompleted && complete && b.ResponseBody.EndOfStream {
			s.collector.RecordTokenUsage(qos.Namespace, qos.User, model, promptTokens, completionTokens)
			s.collector.RecordRespProcessingTime(qos.Namespace, qos.User, model, float64(timing.duration.Milliseconds()))
			s.processingTimes.Delete(requestID)
			klog.V(5).InfoS("process response", "requestID", requestID, "input", promptTokens, "output", completionTokens, "duration", timing.duration)
		}
		timing.startTime = time.Now()
	}()

	// Handle streaming response
	if stream {
		t := &http.Response{
			Body: io.NopCloser(bytes.NewReader(b.ResponseBody.GetBody())),
		}
		streaming := ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(t), nil)
		for streaming.Next() {
			evt := streaming.Current()
			if len(evt.Choices) == 0 {
				usage = evt.Usage
			}
		}
		if err := streaming.Err(); err != nil {
			klog.ErrorS(err, "error to unmarshal response", "requestID", requestID, "responseBody", string(b.ResponseBody.GetBody()))
			complete = true
			return generateErrorResponse(
				envoyTypePb.StatusCode_InternalServerError,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorStreaming, RawValue: []byte("true"),
				}}},
				err.Error()), complete
		}
	} else {
		// Handle non-streaming response
		// Use request ID as key to store buffer for each request
		buf, _ := requestBuffers.LoadOrStore(requestID, &bytes.Buffer{})
		buffer := buf.(*bytes.Buffer)
		buffer.Write(b.ResponseBody.Body)

		// Return common response if data is not fully received
		if !b.ResponseBody.EndOfStream {
			return &extProcPb.ProcessingResponse{
				Response: &extProcPb.ProcessingResponse_ResponseBody{
					ResponseBody: &extProcPb.BodyResponse{
						Response: &extProcPb.CommonResponse{},
					},
				},
			}, complete
		}

		// Process complete response data
		finalBody := buffer.Bytes()
		requestBuffers.Delete(requestID)

		// Parse response data
		if err := jsonUnmarshal(finalBody, &res); err != nil {
			klog.ErrorS(err, "error to unmarshal response", "requestID", requestID, "responseBody", string(b.ResponseBody.GetBody()))
			complete = true
			return generateErrorResponse(
				envoyTypePb.StatusCode_InternalServerError,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorResponseUnmarshal, RawValue: []byte("true"),
				}}},
				err.Error()), complete
		} else if len(res.Model) == 0 {
			msg := ErrorUnknownResponse.Error()
			responseBodyContent := string(b.ResponseBody.GetBody())
			if len(responseBodyContent) != 0 {
				msg = responseBodyContent
			}
			klog.ErrorS(err, "unexpected response", "requestID", requestID, "responseBody", responseBodyContent)
			complete = true
			return generateErrorResponse(
				envoyTypePb.StatusCode_InternalServerError,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorResponseUnknown, RawValue: []byte("true"),
				}}},
				msg), complete
		}
		usage = res.Usage
	}

	// Handle token statistics
	var requestEnd string
	if usage.TotalTokens != 0 {
		complete = true
		// input tokens
		promptTokens = usage.PromptTokens
		// output tokens
		completionTokens = usage.CompletionTokens
		// total
		totalTokens = usage.TotalTokens

		tokenCountMap := map[string]int64{
			"prompt":   promptTokens,
			"response": completionTokens,
			"total":    totalTokens,
		}

		klog.InfoS("request token", "token", tokenCountMap, "requestID", requestID)

		// do rate limit
		err := s.doTokenRateLimit(ctx, qos, totalTokens)
		if err != nil {
			return generateErrorResponse(
				envoyTypePb.StatusCode_InternalServerError,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorRateLimit, RawValue: []byte("true"),
				}}},
				err.Error()), complete
		}

		// do quota limit
		err = s.doTokenQuotaLimit(ctx, qos, tokenCountMap)
		if err != nil {
			return generateErrorResponse(
				envoyTypePb.StatusCode_InternalServerError,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorQuota, RawValue: []byte("true"),
				}}},
				err.Error()), complete
		}

		// Count tokens by user
		// if user.Name != "" {
		// 	tpm, err := s.ratelimiter.Incr(ctx, fmt.Sprintf("%v_TPM_CURRENT", user), res.Usage.TotalTokens)
		// 	if err != nil {
		// 		return generateErrorResponse(
		// 			envoyTypePb.StatusCode_InternalServerError,
		// 			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
		// 				Key: HeaderErrorIncrTPM, RawValue: []byte("true"),
		// 			}}},
		// 			err.Error()), complete
		// 	}

		// 	headers = append(headers,
		// 		&configPb.HeaderValueOption{
		// 			Header: &configPb.HeaderValue{
		// 				Key:      HeaderUpdateRPM,
		// 				RawValue: []byte(fmt.Sprintf("%d", rpm)),
		// 			},
		// 		},
		// 		&configPb.HeaderValueOption{
		// 			Header: &configPb.HeaderValue{
		// 				Key:      HeaderUpdateTPM,
		// 				RawValue: []byte(fmt.Sprintf("%d", tpm)),
		// 			},
		// 		},
		// 	)
		// 	requestEnd = fmt.Sprintf(requestEnd+"rpm: %s, tpm: %s, ", rpm, tpm)
		// }

		klog.Infof("request end, requestID: %s - %s", requestID, requestEnd)
	}

	return &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_ResponseBody{
			ResponseBody: &extProcPb.BodyResponse{
				Response: &extProcPb.CommonResponse{
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: headers,
					},
				},
			},
		},
	}, complete
}
