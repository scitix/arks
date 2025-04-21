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

package qosconfig

import "github.com/arks-ai/arks/pkg/gateway/quota"

type RateLimit struct {
	Type  string `json:"type"`
	Value int64  `json:"value"`
}

type UserQos struct {
	User       string      `json:"user"`
	Namespace  string      `json:"namespace"`
	Model      string      `json:"model"`
	QuotaName  string      `json:"quotaName"`
	RateLimits []RateLimit `json:"rateLimits"`
}

type QuotaItem struct {
	Type  string `json:"type"`
	Value int64  `json:"value"`
}

type QuotaConfig struct {
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
	Limits    []QuotaItem `json:"limits"`
}

func QosToQuotaRequests(conf *QuotaConfig, countMap map[string]int64) []*quota.QuotaRequest {
	requests := make([]*quota.QuotaRequest, 0)
	if countMap == nil {
		countMap = make(map[string]int64)
	}
	for _, limit := range conf.Limits {
		req := &quota.QuotaRequest{
			Identifier: []quota.Label{
				{
					Key:   "namespace",
					Value: conf.Namespace,
				},
				{
					Key:   "quotaname",
					Value: conf.Name,
				},
				{
					Key:   "type", // prompt, response, total etc.
					Value: limit.Type,
				},
			},
			Limit:   limit.Value,
			Request: countMap[limit.Type],
		}
		requests = append(requests, req)
	}
	return requests
}
