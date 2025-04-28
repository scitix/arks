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

import "context"

type ConfigType string

const (
	ConfigTypeArks  ConfigType = "arks"
	ConfigTypeRedis ConfigType = "redis"
	ConfigTypeFile  ConfigType = "file"
)

type ConfigProvider interface {
	GetQosByToken(ctx context.Context, token string, model string) (*UserQos, error)
	GetQuotaConfig(ctx context.Context, namespace string, quotaName string) (*QuotaConfig, error)
	GetModelList(ctx context.Context, namespace string) ([]string, error)
	GetModelsByToken(ctx context.Context, token string) ([]string, error)
	Start(ctx context.Context) error

	// Refresh(ctx context.Context) error
}
