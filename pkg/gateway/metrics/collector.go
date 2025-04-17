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

package metrics

type MetricsCollector interface {
	RecordRequest(namespace, user, model string, duration float64, status string)
	RecordTokenUsage(namespace, user, model string, inputTokens, outputTokens int64)
	RecordRateLimitHit(namespace, user, model, ruleType string)
	UpdateRateLimitTokens(namespace, user, model, ruleType string, tokens float64)
	UpdateQuotaUsage(namespace, model, quotaName, quotaType string, usage float64)
	UpdateQuotaLimit(namespace, model, quotaName, quotaType string, limit float64)
	RecordError(namespace, model, errorType string)
}

type DefaultMetricsCollector struct{}

func NewMetricsCollector() MetricsCollector {
	return &DefaultMetricsCollector{}
}

func (m *DefaultMetricsCollector) RecordRequest(namespace, user, model string, duration float64, status string) {
	requestTotal.WithLabelValues(namespace, user, model, status).Inc()
	requestDuration.WithLabelValues(namespace, user, model).Observe(duration)
}

func (m *DefaultMetricsCollector) RecordTokenUsage(namespace, user, model string, inputTokens, outputTokens int64) {
	tokenUsage.WithLabelValues(namespace, user, model, "input").Add(float64(inputTokens))
	tokenUsage.WithLabelValues(namespace, user, model, "output").Add(float64(outputTokens))
	tokenDistribution.WithLabelValues(namespace, user, model, "input").Observe(float64(inputTokens))
	tokenDistribution.WithLabelValues(namespace, user, model, "output").Observe(float64(outputTokens))
}

func (m *DefaultMetricsCollector) RecordRateLimitHit(namespace, user, model, ruleType string) {
	rateLimitHits.WithLabelValues(namespace, user, model, ruleType).Inc()
}

// TODO:
func (m *DefaultMetricsCollector) UpdateRateLimitTokens(namespace, user, model, ruleType string, tokens float64) {
	rateLimitTokens.WithLabelValues(namespace, user, model, ruleType).Set(tokens)
}

// TODO:
func (m *DefaultMetricsCollector) UpdateQuotaUsage(namespace, model, quotaName, quotaType string, usage float64) {
	quotaUsage.WithLabelValues(namespace, model, quotaName, quotaType).Set(usage)
}

// TODO:
func (m *DefaultMetricsCollector) UpdateQuotaLimit(namespace, model, quotaName, quotaType string, limit float64) {
	quotaLimit.WithLabelValues(namespace, model, quotaName, quotaType).Set(limit)
}

// TODO:
func (m *DefaultMetricsCollector) RecordError(namespace, model, errorType string) {
	errorTotal.WithLabelValues(namespace, model, errorType).Inc()
}
