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

package quota

import (
	"context"
	"encoding/json"
)

type QuotaService interface {
	IncrUsage(ctx context.Context, requests []*QuotaRequest) error
	SetUsage(ctx context.Context, requests []*QuotaRequest) error
	GetUsage(ctx context.Context, requests []*QuotaRequest) ([]*QuotaResult, error)
}

type Label struct {
	Key   string
	Value string
}

type QuotaRequest struct {
	Identifier []Label
	Limit      int64
	Request    int64
}

type QuotaResult struct {
	Identifier   []Label
	OverLimit    bool  `json:"overLimit"`
	CurrentUsage int64 `json:"currentUsage"`
	LimitMax     int64 `json:"limitMax"`
}

func (r *QuotaResult) JSON() string {
	json, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return string(json)
}
