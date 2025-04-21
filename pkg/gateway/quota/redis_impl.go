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
	"strconv"

	"github.com/redis/go-redis/v9"
)

type RedisQuotaService struct {
	client redis.UniversalClient
	keyGen *CacheKeyGenerator
}

func NewRedisQuotaService(client redis.UniversalClient, prefix string) QuotaService {
	return &RedisQuotaService{
		client: client,
		keyGen: NewCacheKeyGenerator(prefix),
	}
}

func (s *RedisQuotaService) IncrUsage(ctx context.Context, requests []*QuotaRequest) error {
	pipe := s.client.Pipeline()

	for _, req := range requests {
		key := s.keyGen.Generate(req)
		pipe.IncrBy(ctx, key, req.Request)
	}

	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisQuotaService) SetUsage(ctx context.Context, requests []*QuotaRequest) error {
	pipe := s.client.Pipeline()

	for _, req := range requests {
		key := s.keyGen.Generate(req)
		pipe.Set(ctx, key, req.Request, 0)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// TODO: get all at once
func (s *RedisQuotaService) GetUsage(ctx context.Context, requests []*QuotaRequest) ([]*QuotaResult, error) {
	pipe := s.client.Pipeline()
	results := make([]*QuotaResult, len(requests))

	type getItem struct {
		req    *QuotaRequest
		key    string
		getCmd *redis.StringCmd
	}
	items := make([]*getItem, len(requests))

	for i, req := range requests {
		key := s.keyGen.Generate(req)
		items[i] = &getItem{
			req:    req,
			key:    key,
			getCmd: pipe.Get(ctx, key),
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, err
	}

	for i, item := range items {
		var currentValue int64
		if val, err := item.getCmd.Result(); err == redis.Nil {
			currentValue = 0
		} else if err != nil {
			return nil, err
		} else {
			currentValue, _ = strconv.ParseInt(val, 10, 64)
		}

		results[i] = &QuotaResult{
			Identifier:   item.req.Identifier,
			OverLimit:    currentValue > item.req.Limit,
			CurrentUsage: currentValue,
			LimitMax:     item.req.Limit,
		}
	}

	return results, nil
}
