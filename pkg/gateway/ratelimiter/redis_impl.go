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

package ratelimiter

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"
)

// pkg/gateway/limiter/redis_cache.go
type RedisRateLimter struct {
	client redis.UniversalClient
	keyGen *CacheKeyGenerator
	config *LimitConfig
	jitter time.Duration
}

func NewRedisRateLimter(client redis.UniversalClient, prefix string) RateLimterInterface {
	return &RedisRateLimter{
		client: client,
		keyGen: NewCacheKeyGenerator(prefix),
		config: newLimitConfig(), // TODO: config from other source
		jitter: time.Duration(rand.Int63n(1000)) * time.Millisecond,
	}
}

func (c *RedisRateLimter) CheckLimit(ctx context.Context, requests []*RateLimitRequest) ([]*RateLimitResponse, error) {
	pipe := c.client.Pipeline()
	responses := make([]*RateLimitResponse, len(requests))
	type checkItem struct {
		req    *RateLimitRequest
		key    string
		getCmd *redis.StringCmd
		ttlCmd *redis.DurationCmd
		rule   *LimitRuleConfig
	}
	items := make([]*checkItem, len(requests))
	now := time.Now()
	// 1. Build pipeline to get current values
	for i, req := range requests {
		rule := c.config.GetRule(req.RuleName)
		if rule == nil {
			klog.Errorf("rate limite rule %s not found", req.RuleName)
			continue
			// TODO: handle error here
		}
		key := c.keyGen.Generate(req, rule, now.Unix())

		items[i] = &checkItem{
			req:    req,
			key:    key,
			getCmd: pipe.Get(ctx, key),
			ttlCmd: pipe.TTL(ctx, key),
			rule:   rule,
		}
	}

	// 2. Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, err
	}

	// 3. Process results
	for i, item := range items {
		if item == nil {
			continue
		}

		var currentValue int64
		if val, err := item.getCmd.Result(); err == redis.Nil {
			currentValue = 0
		} else if err != nil { // TODO: handle error here
			currentValue = 0
		} else {
			currentValue, _ = strconv.ParseInt(val, 10, 64)
		}

		ttl, err := item.ttlCmd.Result()
		if err != nil || ttl < 0 { // When key doesn't exist, TTL returns -2; when key exists but has no expiration, TTL returns -1
			ttl = 0
		}

		responses[i] = &RateLimitResponse{
			RuleName:     item.req.RuleName,
			OverLimit:    currentValue+item.req.Request > item.req.Limit,
			CurrentUsage: currentValue,
			LimitMax:     item.req.Limit,
			ExpiresAt:    now.Add(ttl),
		}
	}

	return responses, nil
}

func (c *RedisRateLimter) DoLimit(ctx context.Context, requests []*RateLimitRequest) error {
	pipe := c.client.Pipeline()

	type limitItem struct {
		req  *RateLimitRequest
		key  string
		rule *LimitRuleConfig
	}
	items := make([]*limitItem, len(requests))
	now := time.Now()

	for i, req := range requests {
		rule := c.config.GetRule(req.RuleName)
		if rule == nil {
			klog.Errorf("rate limit rule %s not found", req.RuleName)
			continue
		}
		key := c.keyGen.Generate(req, rule, now.Unix())

		items[i] = &limitItem{
			req:  req,
			key:  key,
			rule: rule,
		}

		// incr
		pipe.IncrBy(ctx, key, req.Request)

		// TODO: use ExpireNX? Starting with Redis version 7.0.0
		ttl, err := c.client.TTL(ctx, key).Result()
		if err != nil {
			return fmt.Errorf("redis get ttl failed, err: %v", err)
		}
		if ttl < 0 {
			// fmt.Printf("ttl: %v\n", ttl)
			// set exp time
			expiration := time.Duration(rule.Period*UnitToSeconds(rule.Unit)) * time.Second
			if c.jitter > 0 {
				expiration += time.Duration(rand.Int63n(int64(c.jitter)))
			}
			// fmt.Printf("key: %s, exp: %v", key, expiration)
			pipe.Expire(ctx, key, expiration)
		}

	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (c *RedisRateLimter) GetRuleByName(name string) *LimitRuleConfig {
	return c.config.GetRule(name)
}
