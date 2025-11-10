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
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arks-ai/arks/pkg/gateway/metrics"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"github.com/arks-ai/arks/pkg/gateway/qosconfig"
	"github.com/arks-ai/arks/pkg/gateway/quota"
	"github.com/arks-ai/arks/pkg/gateway/ratelimiter"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	healthPb "google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	ratelimiter     ratelimiter.RateLimterInterface
	quotaService    quota.QuotaService
	configProvider  qosconfig.ConfigProvider
	collector       *metrics.DefaultMetricsCollector
	processingTimes sync.Map

	httpServer    *http.Server
	metricsServer *http.Server
	grpcServer    *grpc.Server
}

type processingTime struct {
	startTime time.Time
	duration  time.Duration
}

func NewServer(
	ratelimiter ratelimiter.RateLimterInterface,
	quotaService quota.QuotaService,
	configProvider qosconfig.ConfigProvider,
) *Server {

	return &Server{
		ratelimiter:    ratelimiter,
		quotaService:   quotaService,
		configProvider: configProvider,
		collector:      metrics.NewMetricsCollector(),
	}
}

func (s *Server) Process(srv extProcPb.ExternalProcessor_ProcessServer) error {
	var qos *qosconfig.UserQos
	// var rpm, traceTerm int64
	var statusCode int
	var model, token string
	var stream bool
	var requestStart time.Time
	ctx := srv.Context()
	requestID := uuid.New().String()
	completed := false

	klog.InfoS("Processing request", "requestID", requestID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		resp := &extProcPb.ProcessingResponse{}
		switch v := req.Request.(type) {

		case *extProcPb.ProcessingRequest_RequestHeaders:
			requestStart = time.Now()
			resp, token = s.HandleRequestHeaders(ctx, requestID, req)

		case *extProcPb.ProcessingRequest_RequestBody:
			resp, qos, model, stream = s.HandleRequestBody(ctx, requestID, req, token)

		case *extProcPb.ProcessingRequest_ResponseHeaders:
			resp, statusCode = s.HandleResponseHeaders(ctx, requestID, req)
			if statusCode == 500 {
				s.collector.RecordRequest(qos.Namespace, qos.User, model, float64(time.Since(requestStart).Milliseconds())/1000, strconv.Itoa(statusCode))
				// for error code 500, ProcessingRequest_ResponseBody is not invoked
				resp = s.responseErrorProcessing(ctx, resp, statusCode, model, requestID, "")
			}
		case *extProcPb.ProcessingRequest_ResponseBody:
			if statusCode != 200 {
				resp = s.responseErrorProcessing(ctx, resp, statusCode, model, requestID,
					string(req.Request.(*extProcPb.ProcessingRequest_ResponseBody).ResponseBody.GetBody()))
			} else {
				resp, completed = s.HandleResponseBody(ctx, requestID, req, qos, model, stream, completed)
			}
			s.collector.RecordRequest(qos.Namespace, qos.User, model, float64(time.Since(requestStart).Milliseconds())/1000, strconv.Itoa(statusCode))
		default:
			klog.Infof("Unknown Request type %+v\n", v)
		}

		if err := srv.Send(resp); err != nil {
			klog.Infof("send error %v", err)
		}
	}
}

func (s *Server) StartHttpServer(port int) {
	// r := mux.NewRouter()
	// r.HandleFunc("/v1/models", s.handleGetModels)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleGetModels)
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		klog.Infof("Starting http server on port %d", port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("http server failed: %v", err)
		}
	}()
}

func (s *Server) StartMetricsServer(port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	s.metricsServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		klog.Infof("Starting metrics server on port %d", port)
		if err := s.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("Metrics server failed: %v", err)
		}
	}()
}

func (s *Server) StartGrpcServer(port int) {

	s.grpcServer = grpc.NewServer()
	extProcPb.RegisterExternalProcessorServer(s.grpcServer, s)
	healthPb.RegisterHealthServer(s.grpcServer, NewHealthCheckServer())

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}

	go func() {
		klog.Infof("Starting gRPC server on port %d", port)
		if err := s.grpcServer.Serve(listener); err != nil {
			klog.Fatalf("gRPC server failed: %v", err)
		}
	}()
}

func (s *Server) GracefullyShutdown(ctx context.Context) error {

	var wg sync.WaitGroup
	errChan := make(chan error, 3) // collecting errors

	if s.httpServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			klog.Info("Shutting down HTTP server...")
			if err := s.httpServer.Shutdown(ctx); err != nil {
				errChan <- fmt.Errorf("HTTP server shutdown error: %v", err)
			}
		}()
	}

	if s.metricsServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			klog.Info("Shutting down metrics server...")
			if err := s.metricsServer.Shutdown(ctx); err != nil {
				errChan <- fmt.Errorf("Metrics server shutdown error: %v", err)
			}
		}()
	}

	if s.grpcServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			klog.Info("Shutting down gRPC server...")
			stopped := make(chan struct{})
			go func() {
				s.grpcServer.GracefulStop()
				close(stopped)
			}()

			select {
			case <-ctx.Done():
				s.grpcServer.Stop()
				errChan <- fmt.Errorf("gRPC server shutdown timeout")
			case <-stopped:
			}
		}()
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		var errMsg strings.Builder
		for _, err := range errors {
			errMsg.WriteString(err.Error() + "; ")
		}
		return fmt.Errorf("shutdown errors: %s", errMsg.String())
	}

	klog.Info("All servers shutdown successfully")
	return nil
}

func NewHealthCheckServer() *HealthServer {
	return &HealthServer{}
}

type HealthServer struct{}

func (s *HealthServer) Check(ctx context.Context, in *healthPb.HealthCheckRequest) (*healthPb.HealthCheckResponse, error) {
	return &healthPb.HealthCheckResponse{Status: healthPb.HealthCheckResponse_SERVING}, nil
}

func (s *HealthServer) List(ctx context.Context, in *healthPb.HealthListRequest) (*healthPb.HealthListResponse, error) {
	return &healthPb.HealthListResponse{
		Statuses: map[string]*healthPb.HealthCheckResponse{},
	}, nil
}

func (s *HealthServer) Watch(in *healthPb.HealthCheckRequest, srv healthPb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "watch is not implemented")
}

func (s *Server) responseErrorProcessing(ctx context.Context, resp *extProcPb.ProcessingResponse, respErrorCode int,
	model, requestID, errMsg string) *extProcPb.ProcessingResponse {
	// httprouteErr := s.validateHTTPRouteStatus(ctx, model)
	// if errMsg != "" && httprouteErr != nil {
	// 	errMsg = fmt.Sprintf("%s. %s", errMsg, httprouteErr.Error())
	// } else if errMsg == "" && httprouteErr != nil {
	// 	errMsg = httprouteErr.Error()
	// }
	klog.ErrorS(nil, "request end", "requestID", requestID, "errorCode", respErrorCode, "errorMessage", errMsg)
	return generateErrorResponse(
		envoyTypePb.StatusCode(respErrorCode),
		resp.GetResponseHeaders().GetResponse().GetHeaderMutation().GetSetHeaders(),
		errMsg)
}
