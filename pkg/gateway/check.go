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

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/scitix/arks/pkg/gateway/qosconfig"
	"github.com/scitix/arks/pkg/gateway/ratelimiter"
)

// in request body
func (s *Server) doRequestRateLimit(ctx context.Context, qos *qosconfig.UserQos) error {
	// to ratelimit reqeust
	requests := make([]*ratelimiter.RateLimitRequest, 0)
	for _, limit := range qos.RateLimits {
		rule := s.ratelimiter.GetRuleByName(limit.Type)
		if rule == nil {
			return fmt.Errorf("rule %s not found", limit.Type)
		}
		if rule.Type == ratelimiter.TypeRequest {
			requests = append(requests, toRateLimitRequest(qos, limit.Type, limit.Value, 1))
		}
	}
	return s.ratelimiter.DoLimit(ctx, requests)
}

// in response body
func (s *Server) doTokenRateLimit(ctx context.Context, qos *qosconfig.UserQos, count int64) error {
	requests := make([]*ratelimiter.RateLimitRequest, 0)
	for _, limit := range qos.RateLimits {
		rule := s.ratelimiter.GetRuleByName(limit.Type)
		if rule == nil {
			return fmt.Errorf("rule %s not found", limit.Type)
		}
		if rule.Type == ratelimiter.TypeToken {
			requests = append(requests, toRateLimitRequest(qos, limit.Type, limit.Value, count))
		}
	}
	return s.ratelimiter.DoLimit(ctx, requests)
}

// // in response body
func (s *Server) doTokenQuotaLimit(ctx context.Context, qos *qosconfig.UserQos, countMap map[string]int64) error {
	quotaConf, err := s.configProvider.GetQuotaConfig(ctx, qos.Namespace, qos.QuotaName)
	if err != nil {
		return err
	}
	requests := qosconfig.QosToQuotaRequests(quotaConf, countMap)
	return s.quotaService.IncrUsage(ctx, requests)
}

// in request
func (s *Server) checkTokenQuotaLimit(ctx context.Context, qos *qosconfig.UserQos) (errResp *extProcPb.ProcessingResponse, err error) {
	quotaConf, err := s.configProvider.GetQuotaConfig(ctx, qos.Namespace, qos.QuotaName)
	if err != nil {
		return generateErrorResponse(
			envoyTypePb.StatusCode_InternalServerError,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorQuota, RawValue: []byte("true"),
			}}},
			err.Error()), err
	}
	requests := qosconfig.QosToQuotaRequests(quotaConf, map[string]int64{})
	usage, err := s.quotaService.GetUsage(ctx, requests)
	if err != nil {
		return generateErrorResponse(
			envoyTypePb.StatusCode_InternalServerError,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorQuota, RawValue: []byte("true"),
			}}},
			err.Error()), err
	}
	for _, usage := range usage {
		if usage.OverLimit {
			return generateErrorResponse(
				envoyTypePb.StatusCode_TooManyRequests,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorQuota, RawValue: []byte("true"),
				}}},
				usage.JSON()), err
		}
	}
	return nil, nil
}

func (s *Server) checkRateLimit(ctx context.Context, qos *qosconfig.UserQos) (*extProcPb.ProcessingResponse, error) {

	requests := make([]*ratelimiter.RateLimitRequest, 0)
	for _, limit := range qos.RateLimits {
		rule := s.ratelimiter.GetRuleByName(limit.Type)
		if rule == nil {
			err := fmt.Errorf("rule %s not found", limit.Type)
			return generateErrorResponse(
				envoyTypePb.StatusCode_InternalServerError,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorRateLimit, RawValue: []byte("true"),
				}}},
				err.Error()), err
		}
		if rule.Type == ratelimiter.TypeRequest {
			requests = append(requests, toRateLimitRequest(qos, limit.Type, limit.Value, 1))
		} else if rule.Type == ratelimiter.TypeToken {
			// token is not caculated in request
			requests = append(requests, toRateLimitRequest(qos, limit.Type, limit.Value, 0))
		}
	}

	resp, err := s.ratelimiter.CheckLimit(ctx, requests)
	if err != nil {
		return generateErrorResponse(
			envoyTypePb.StatusCode_InternalServerError,
			[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
				Key: HeaderErrorRateLimit, RawValue: []byte("true"),
			}}},
			err.Error()), err
	}

	for _, r := range resp {
		if r == nil {
			continue
		}
		if r.OverLimit {
			s.collector.RecordRateLimitHit(qos.Namespace, qos.User, qos.Model, r.RuleName)
			return generateErrorResponse(
				envoyTypePb.StatusCode_TooManyRequests,
				[]*configPb.HeaderValueOption{{Header: &configPb.HeaderValue{
					Key: HeaderErrorRateLimit, RawValue: []byte("true"),
				}}},
				r.JSON()), err
		}
	}

	return nil, nil
}
