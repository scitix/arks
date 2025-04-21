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

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/arks-ai/arks/pkg/gateway"
	"github.com/arks-ai/arks/pkg/gateway/qosconfig"
	"github.com/arks-ai/arks/pkg/gateway/quota"
	"github.com/arks-ai/arks/pkg/gateway/ratelimiter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	healthPb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	redisClient  redis.UniversalClient
	quotaService quota.QuotaService
)

type Settings struct {
	Server    ServerSettings
	Redis     RedisSettings
	Provider  ProviderSettings
	RateLimit RateLimiterSettings
	Quota     QuotaSettings
	Metrics   MetricsSettings
}

type ServerSettings struct {
	GrpcPort int
	LogLevel int
}

type RedisSettings struct {
	// redis base config
	Mode     string // single, cluster, sentinel
	Addrs    string // 地址列表
	Username string
	Password string
	DB       int

	// sentinel config
	MasterName string

	// redis pool config
	PoolSize    int
	PoolTimeout time.Duration
	MaxRetries  int
}

type ProviderSettings struct {
	Type       string // arks, redis, file
	Kubeconfig string
}

type RateLimiterSettings struct {
	Type      string // redis, localcache, memcache
	KeyPrefix string
}

type QuotaSettings struct {
	Type      string // redis, localcache, memcache
	KeyPrefix string
}

type MetricsSettings struct {
	Port int
}

func initFlags(s *Settings) {
	// Server flags
	flag.IntVar(&s.Server.GrpcPort, "server.port", 50052, "gRPC server port")
	flag.IntVar(&s.Server.LogLevel, "server.log-level", 0, "number for the log level verbosity")

	// flag.StringVar(&s.Server.LogLevel, "server.log-level", "info", "log level (debug, info, warn, error)")
	// Provider flags
	flag.StringVar(&s.Provider.Type, "provider.type", "arks", "provider type (arks, redis, file)")
	flag.StringVar(&s.Provider.Kubeconfig, "provider.kubeconfig", "", "path to kubeconfig file")

	// Ratelimiter flags
	flag.StringVar(&s.RateLimit.Type, "ratelimiter.type", "redis", "cache type (redis, localcache, memcache)")
	flag.StringVar(&s.RateLimit.KeyPrefix, "ratelimiter.key-prefix", "arks-ratelimiter", "key prefix for rate limiter")

	// Quota flags
	flag.StringVar(&s.Quota.Type, "quota.type", "redis", "quota type (redis, localcache, memcache)")
	flag.StringVar(&s.Quota.KeyPrefix, "quota.key-prefix", "arks-quota", "key prefix for quota")

	// Metrics flags
	flag.IntVar(&s.Metrics.Port, "metrics.port", 8080, "Prometheus metrics port")

	// TODO: klog level set
	klog.InitFlags(flag.CommandLine)
	initRedisFlags(s)
	flag.Parse()
}

func initRedisFlags(s *Settings) {
	// TODO: support more options for redis
	flag.StringVar(&s.Redis.Mode, "redis.mode", "single", "redis mode (single, cluster, sentinel)")
	flag.StringVar(&s.Redis.Addrs, "redis.addrs", "127.0.0.1:6379", "redis addresses, separated by comma")
	flag.StringVar(&s.Redis.Username, "redis.username", "", "redis username")
	flag.StringVar(&s.Redis.Password, "redis.password", "", "redis password")
	flag.IntVar(&s.Redis.DB, "redis.db", 0, "redis database number")
	flag.StringVar(&s.Redis.MasterName, "redis.master-name", "", "redis master name")
	// default settings
	flag.IntVar(&s.Redis.PoolSize, "redis.pool-size", 10, "redis connection pool size")
	flag.DurationVar(&s.Redis.PoolTimeout, "redis.pool-timeout", 10*time.Second, "redis connection pool timeout")
	flag.IntVar(&s.Redis.MaxRetries, "redis.max-retries", 3, "redis connection max retries")
}

func initRedisClient(r *RedisSettings) (redis.UniversalClient, error) {

	opts := &redis.UniversalOptions{
		Addrs:       strings.Split(r.Addrs, ","),
		Username:    r.Username,
		Password:    r.Password,
		DB:          r.DB,
		PoolSize:    r.PoolSize,
		PoolTimeout: r.PoolTimeout,
		MaxRetries:  r.MaxRetries,
	}

	switch strings.ToLower(r.Mode) {
	case "single":
		if len(opts.Addrs) != 1 {
			return nil, fmt.Errorf("redis address should be 1 for single mode")
		}
	case "cluster":
		if len(opts.Addrs) < 3 {
			return nil, fmt.Errorf("at least 3 addresses are required for cluster mode")
		}
	case "sentinel":
		if r.MasterName == "" {
			return nil, fmt.Errorf("master name is required for sentinel mode")
		}
		if len(opts.Addrs) == 0 {
			return nil, fmt.Errorf("sentinel addresses are required")
		}
	default:
		return nil, fmt.Errorf("unsupported redis mode: %s", r.Mode)
	}

	return redis.NewUniversalClient(opts), nil
}

func main() {
	var settings Settings
	var err error
	initFlags(&settings)

	if settings.RateLimit.Type == "redis" || settings.Quota.Type == "redis" {

		redisClient, err = initRedisClient(&settings.Redis)
		if err != nil {
			klog.Fatalf("failed to initialize redis client: %v", err)
		}
	}

	ratelimiter, err := createRateLimiter(&settings.RateLimit)
	if err != nil {
		klog.Fatalf("failed to create cache: %v", err)
	}

	quotaService, err = createQuotaService(&settings.Quota)
	if err != nil {
		klog.Fatalf("failed to create quota service: %v", err)
	}

	provider, err := createConfigProvider(&settings.Provider)
	if err != nil {
		klog.Fatalf("failed to create config provider: %v", err)
	}

	// run server

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// TODO: start in server??
	if err := provider.Start(ctx); err != nil {
		klog.Fatalf("start config provider failed: %v", err)
	}

	klog.Infof("config provider start")

	// grpc server init
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", settings.Server.GrpcPort))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	extProcPb.RegisterExternalProcessorServer(s, gateway.NewServer(ratelimiter, quotaService, provider))
	healthPb.RegisterHealthServer(s, gateway.NewHealthCheckServer())

	// metrics server
	startMetricsServer(settings.Metrics.Port)
	// shutdown graceful
	shutdownComplete := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		klog.Info("Received shutdown signal, initiating graceful shutdown...")
		cancel()
		stopped := make(chan struct{})
		go func() {
			s.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			klog.Info("gRPC server stopped gracefully")
		case <-time.After(1 * time.Second):
			klog.Warning("Forcing gRPC server shutdown after timeout")
			s.Stop()
		}

		close(shutdownComplete)
	}()

	klog.Infof("Starting gRPC server on port %d", settings.Server.GrpcPort)
	if err := s.Serve(lis); err != nil {
		klog.Errorf("gRPC server failed: %v", err)
		cancel()
	}

	<-shutdownComplete
	klog.Info("Server shutdown completed")
}

func createRateLimiter(s *RateLimiterSettings) (ratelimiter.RateLimterInterface, error) {
	switch s.Type {
	case "redis":
		return ratelimiter.NewRedisRateLimter(redisClient, s.KeyPrefix), nil
	}
	return nil, fmt.Errorf("invalid rate limiter type: %s", s.Type)
}

func createQuotaService(s *QuotaSettings) (quota.QuotaService, error) {
	switch s.Type {
	case "redis":
		return quota.NewRedisQuotaService(redisClient, s.KeyPrefix), nil
	}
	return nil, fmt.Errorf("invalid quota type: %s", s.Type)
}

func createConfigProvider(s *ProviderSettings) (qosconfig.ConfigProvider, error) {
	switch s.Type {
	case "arks":
		// rest config
		kubeConfig := s.Kubeconfig
		var kc *rest.Config
		var err error
		if kubeConfig == "" {
			klog.Info("using in-cluster configuration")
			kc, err = rest.InClusterConfig()
		} else {
			klog.Infof("using configuration from '%s'", kubeConfig)
			kc, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		}
		if err != nil {
			return nil, err
		}
		return qosconfig.NewArksProvider(kc, quotaService)
		// todo: file, redis config
	}
	return nil, fmt.Errorf("invalid config type: %s", s.Type)
}

func startMetricsServer(port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		klog.Infof("Starting metrics server on port %d", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Errorf("Metrics server failed: %v", err)
		}
	}()
}
