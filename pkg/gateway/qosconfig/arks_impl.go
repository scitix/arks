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

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	arksv1 "github.com/scitix/arks/api/v1"
	"github.com/scitix/arks/pkg/gateway/quota"
)

// ArksProvider
type ArksProvider struct {
	mgr          ctrl.Manager
	client       client.Client
	quotaService quota.QuotaService
}

func setupScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	if err := arksv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	return scheme, nil
}

// set index for ArksToken
func setupIndexes(cache cache.Cache) error {
	// use spec.token as index
	if err := cache.IndexField(
		context.Background(),
		&arksv1.ArksToken{},
		"spec.token",
		func(rawObj client.Object) []string {
			token := rawObj.(*arksv1.ArksToken)
			return []string{token.Spec.Token}
		},
	); err != nil {
		return fmt.Errorf("failed to index ArksToken by token: %w", err)
	}
	return nil
}

// NewArksProvider
func NewArksProvider(config *rest.Config, quotaService quota.QuotaService) (*ArksProvider, error) {

	scheme, err := setupScheme()
	if err != nil {
		return nil, fmt.Errorf("failed to setup scheme: %w", err)
	}

	// create manager
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"}, // don't use metrics
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	// set up index
	if err := setupIndexes(mgr.GetCache()); err != nil {
		return nil, fmt.Errorf("failed to setup indexes: %w", err)
	}

	return &ArksProvider{
		mgr:          mgr,
		client:       mgr.GetClient(),
		quotaService: quotaService,
	}, nil
}

func (p *ArksProvider) Start(ctx context.Context) error {

	predicates := predicate.Or(
		predicate.GenerationChangedPredicate{}, // watch spec change
		// predicate.ResourceVersionChangedPredicate{}, // watch all fields change
		predicate.Funcs{
			DeleteFunc: func(e event.DeleteEvent) bool {
				// TODO: delete quota usage in redis?
				return true // deletion watch
			},
		},
	)

	err := ctrl.NewControllerManagedBy(p.mgr).
		Named("arks-qos-provider").
		Watches(
			&arksv1.ArksToken{},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			&arksv1.ArksQuota{},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			&arksv1.ArksEndpoint{},
			&handler.EnqueueRequestForObject{},
		).
		WithEventFilter(predicates).Complete(p)
	// WithEventFilter(
	// 	predicate.Funcs{
	// 		CreateFunc: func(e event.CreateEvent) bool {
	// 			return true
	// 		},
	// 		UpdateFunc: func(e event.UpdateEvent) bool {
	// 			return true
	// 		},
	// 		DeleteFunc: func(e event.DeleteEvent) bool {
	// 			return true
	// 		},
	// 	},
	// ).
	// WithOptions(
	// 	controller.Options{
	// 		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](1*time.Second, 10*time.Second),
	// 	},
	// ).
	// Build(p)
	if err != nil {
		return fmt.Errorf("failed to build controller: %w", err)
	}

	ctrl.SetLogger(zap.New())

	// start controller, start cache first
	go func() {
		if err := p.mgr.Start(ctx); err != nil {
			log.FromContext(ctx).Error(err, "failed to start controller")
			return
		}
		// <-ctx.Done()
		log.FromContext(ctx).Info("controller stopped")
	}()

	// wait cache sync
	if !p.mgr.GetCache().WaitForCacheSync(ctx) {
		return fmt.Errorf("cache sync failed")
	}

	// sync quota usage to crd
	go func() {
		// sync every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.syncQuotaUsage(ctx)
			}
		}
	}()

	return nil
}

func (p *ArksProvider) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// log := log.FromContext(ctx)
	// var token arksv1.ArksToken
	// var quota arksv1.ArksQuota

	// ArksToken
	// if err := p.client.Get(ctx, req.NamespacedName, &token); err == nil {
	// 	if err := p.handleToken(ctx, &token); err != nil {
	// 		log.Error(err, "failed to handle token")
	// 		return ctrl.Result{}, err
	// 	}
	// }

	// ArksQuota
	// if err := p.client.Get(ctx, req.NamespacedName, &quota); err == nil {
	// 	if err := p.handleQuota(ctx, &quota); err != nil {
	// 		log.Error(err, "failed to handle quota")
	// 		return ctrl.Result{}, err
	// 	}
	// }

	return ctrl.Result{}, nil
}

// sync quota usage to cr
// set usage to quotaService if outdated
func (p *ArksProvider) syncQuotaUsage(ctx context.Context) error {
	// get quota crd
	// debug sync time
	var quotaList arksv1.ArksQuotaList
	if err := p.client.List(ctx, &quotaList); err != nil {
		klog.ErrorS(err, "failed to list quota")
		return err
	}

	for _, quota := range quotaList.Items {
		limits := make([]QuotaItem, 0)
		for _, limit := range quota.Spec.Quotas {
			limits = append(limits, QuotaItem{
				Type:  limit.Type,
				Value: limit.Value,
			})
		}
		req := QosToQuotaRequests(&QuotaConfig{quota.Name, quota.Namespace, limits}, nil)
		// quotaRequests = append(quotaRequests, req...)
		usage, err := p.quotaService.GetUsage(ctx, req)
		if err != nil {
			klog.ErrorS(err, "failed to get quota usage", "namespace", quota.Namespace, "name", quota.Name)
			continue
		}
		// update quota status
		var shouldUpdateCR bool
		var shouldUpdateQuota bool
		for _, u := range usage {
			// find quota type
			var quotaType string
			for _, label := range u.Identifier {
				if label.Key == "type" {
					quotaType = label.Value
					break
				}
			}

			// find status
			found := false
			for i, status := range quota.Status.QuotaStatus {
				if status.Type == quotaType {

					if status.Used < u.CurrentUsage {
						shouldUpdateCR = true
						quota.Status.QuotaStatus[i].Used = u.CurrentUsage
						quota.Status.QuotaStatus[i].LastUpdateTime = &metav1.Time{Time: time.Now()}
					} else if status.Used > u.CurrentUsage {
						// update in quotaService
						// set usage to quotaService if outdated
						shouldUpdateQuota = true
					}
					found = true
					break
				}
			}

			// add new status
			if !found {
				shouldUpdateCR = true
				quota.Status.QuotaStatus = append(quota.Status.QuotaStatus, arksv1.QuotaStatus{
					Type:           quotaType,
					Used:           u.CurrentUsage,
					LastUpdateTime: &metav1.Time{Time: time.Now()},
				})
			}
		}

		// set usage to quotaService if outdated
		if shouldUpdateQuota {
			p.quotaService.SetUsage(ctx, req)
		}

		// update status in cr
		if shouldUpdateCR {
			if err := p.client.Status().Update(ctx, &quota); err != nil {
				klog.ErrorS(err, "failed to update quota status", "namespace", quota.Namespace, "name", quota.Name)
				continue
			}
		}

	}
	// sync quota usage to redis
	return nil
}

// get qos from arks token
func (p *ArksProvider) GetQosByToken(ctx context.Context, token string, model string) (*UserQos, error) {

	var tokenList arksv1.ArksTokenList
	if err := p.client.List(ctx, &tokenList,
		client.MatchingFields{"spec.token": token}, // use index to list
	); err != nil {
		return nil, err
	}

	if len(tokenList.Items) == 0 {
		return nil, fmt.Errorf("token not found: %s", token)
	}

	obj := tokenList.Items[0]
	ret := &UserQos{
		User:       obj.Name,
		Namespace:  obj.Namespace,
		Model:      model,
		QuotaName:  "",
		RateLimits: make([]RateLimit, 0),
	}
	for _, qos := range obj.Spec.Qos {
		if qos.ArksEndpoint.Name == model {
			ret.Model = qos.ArksEndpoint.Name
			ret.QuotaName = qos.Quota.Name
			for _, rl := range qos.RateLimits {
				ret.RateLimits = append(ret.RateLimits, RateLimit{
					Type:  string(rl.Type),
					Value: rl.Value,
				})
			}
			return ret, nil
		}
	}
	return nil, fmt.Errorf("model not found: %s", model)
}

// get quota config from arks quota
func (p *ArksProvider) GetQuotaConfig(ctx context.Context, namespace string, quotaName string) (*QuotaConfig, error) {

	var obj arksv1.ArksQuota
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: quotaName}, &obj); err != nil {
		return nil, err
	}

	limits := make([]QuotaItem, 0)
	for _, limit := range obj.Spec.Quotas {
		limits = append(limits, QuotaItem{
			Type:  limit.Type,
			Value: limit.Value,
		})
	}

	return &QuotaConfig{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Limits:    limits,
	}, nil
}

// get model list from arks endpoint
func (p *ArksProvider) GetModelList(ctx context.Context, namespace string) ([]string, error) {
	var arksEndpointList arksv1.ArksEndpointList

	if err := p.client.List(ctx, &arksEndpointList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	models := make([]string, 0)
	for _, endpoint := range arksEndpointList.Items {
		models = append(models, endpoint.Name)
	}
	return models, nil
}
