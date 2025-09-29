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

package controller

import (
	"context"
	"fmt"

	arksv1 "github.com/arks-ai/arks/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ArksEndpointReconciler reconciles a ArksEndpoint object
type ArksEndpointReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	arksEndpointControllerFinalizer = "endpoint.arks.ai/controller"
)

func (r *ArksEndpointReconciler) ArksAppIndexFunc(obj client.Object) []string {
	app, ok := obj.(*arksv1.ArksApplication)
	if !ok {
		return nil
	}

	if app.Spec.ServedModelName == "" {
		return []string{app.Spec.Model.Name}
	}

	return []string{app.Spec.ServedModelName}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArksEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &arksv1.ArksApplication{}, "spec.servedModelName", r.ArksAppIndexFunc); err != nil {
		return fmt.Errorf("failed to set up ArksApplication index: %w", err)
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &arksv1.ArksDisaggregatedApplication{}, "spec.servedModelName", r.ArksAppIndexFunc); err != nil {
		return fmt.Errorf("failed to set up ArksDisaggregatedAppication index: %w", err)
	}
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 30,
		}).
		For(&arksv1.ArksEndpoint{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Named("arksendpoint").
		Watches(
			&arksv1.ArksApplication{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueFromApp),
			builder.WithPredicates(
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						return r.filterApp(e.ObjectNew, e.ObjectOld)
					},
					CreateFunc: func(e event.CreateEvent) bool {
						return r.filterApp(e.Object, nil)
					},
					DeleteFunc: func(e event.DeleteEvent) bool {
						return r.filterApp(nil, e.Object)
					},
				}),
		).
		Watches(
			&arksv1.ArksDisaggregatedApplication{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueFromApp),
			builder.WithPredicates(
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						return r.filterApp(e.ObjectNew, e.ObjectOld)
					},
					CreateFunc: func(e event.CreateEvent) bool {
						return r.filterApp(e.Object, nil)
					},
					DeleteFunc: func(e event.DeleteEvent) bool {
						return r.filterApp(nil, e.Object)
					},
				}),
		).
		Complete(r)
}

func (r *ArksEndpointReconciler) filterApp(newObj, oldObj client.Object) bool {

	if oldObj == nil {
		// create func
		epName := getArksEndpointNameFromApplication(newObj)
		if epName != "" {
			ep := &arksv1.ArksEndpoint{}
			err := r.Get(context.Background(), types.NamespacedName{Name: epName, Namespace: newObj.GetNamespace()}, ep)
			if err != nil {
				return false
			}

			if isArksApplicationReady(newObj) {
				// add in route
				return !arksEndpointIncludesAppService(ep, newObj.GetName())
			} else {
				// remove in route
				return arksEndpointIncludesAppService(ep, newObj.GetName())
			}
		}
		return false
	} else if newObj == nil {
		// delete func
		epName := getArksEndpointNameFromApplication(oldObj)
		if epName != "" {
			ep := &arksv1.ArksEndpoint{}
			err := r.Get(context.Background(), types.NamespacedName{Name: epName, Namespace: oldObj.GetNamespace()}, ep)
			if err != nil {
				return false
			}
			// should delete in route
			return arksEndpointIncludesAppService(ep, oldObj.GetName())
		}
		return false
	} else {
		// update func
		// return getArksEndpointNameFromApplication(oldObj) != getArksEndpointNameFromApplication(newObj)
		// status changed ?
		epName := getArksEndpointNameFromApplication(newObj)
		if epName != "" {
			ep := &arksv1.ArksEndpoint{}
			err := r.Get(context.Background(), types.NamespacedName{Name: epName, Namespace: newObj.GetNamespace()}, ep)
			if err != nil {
				return false
			}
			return isArksApplicationReady(oldObj) != isArksApplicationReady(newObj)
		}
		return false
	}
}

func (r *ArksEndpointReconciler) enqueueFromApp(ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	if epName := getArksEndpointNameFromApplication(obj); epName != "" {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      epName,
			},
		})
		klog.V(4).Infof("enqueue endpoint from application %s/%s", obj.GetNamespace(), obj.GetName())

	}

	return requests
}

// +kubebuilder:rbac:groups=arks.ai,resources=arksendpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arks.ai,resources=arksendpoints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arks.ai,resources=arksendpoints/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ArksEndpoint object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ArksEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	ep := &arksv1.ArksEndpoint{}
	if err := r.Client.Get(ctx, req.NamespacedName, ep); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !ep.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, ep)
	} else {
		if !hasFinalizer(ep, arksEndpointControllerFinalizer) {
			addFinalizer(ep, arksEndpointControllerFinalizer)
			if err := r.Client.Update(ctx, ep); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to add arksendpoint finalizer: %q", err)
			}
			klog.V(4).Infof("add finalizer to ArksEndpoint %s/%s", ep.Namespace, ep.Name)
			// requeue to refresh resource version
			return ctrl.Result{
				Requeue: true,
			}, nil
		}
	}

	result, err := r.reconcile(ctx, ep)

	if err != nil {
		klog.Errorf("failed to reconcile ArksEndpoint %s/%s: %v", ep.Namespace, ep.Name, err)
		return result, err
	}

	return result, nil
}

func (r *ArksEndpointReconciler) handleDelete(ctx context.Context, ep *arksv1.ArksEndpoint) (ctrl.Result, error) {

	route := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, types.NamespacedName{Name: ep.Name, Namespace: ep.Namespace}, route)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to get HTTPRoute for ArksEndpoint %s/%s: %w", ep.Namespace, ep.Name, err)
		}
	} else {
		if err := r.Delete(ctx, route); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete HTTPRoute for ArksEndpoint %s/%s: %w", ep.Namespace, ep.Name, err)
		}
	}

	removeFinalizer(ep, arksEndpointControllerFinalizer)
	if err := r.Client.Update(ctx, ep); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove arksendpoint finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *ArksEndpointReconciler) reconcile(ctx context.Context, ep *arksv1.ArksEndpoint) (ctrl.Result, error) {

	var appList arksv1.ArksApplicationList
	// selector := labels.SelectorFromSet(map[string]string{arksv1.ArksControllerKeyEndpoint: ep.Name})
	if err := r.List(ctx, &appList, &client.ListOptions{
		// LabelSelector: selector,
		Namespace: ep.Namespace,
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"spec.servedModelName": ep.Name,
		}),
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list ArksApplication: %w", err)
	}

	var disAppList arksv1.ArksDisaggregatedApplicationList
	if err := r.List(ctx, &disAppList, &client.ListOptions{
		// LabelSelector: selector,
		Namespace: ep.Namespace,
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"spec.servedModelName": ep.Name,
		}),
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list ArksDisaggregatedApplication: %w", err)
	}

	var backendRefs []gatewayv1.HTTPBackendRef

	// TODO: remove not ready services
	staticRouteMap := make(map[string]gatewayv1.HTTPBackendRef)
	for _, static := range ep.Spec.RouteConfigs {
		staticRouteMap[string(static.Name)] = static
		backendRefs = append(backendRefs, static)
	}

	// add route from app
	for _, app := range appList.Items {
		// app already in static route config
		svcName := getApplicationServiceName(app.Name)
		if _, exists := staticRouteMap[svcName]; exists {
			klog.V(4).InfoS("application service exist in static route", "service", svcName)
			continue
		}
		if app.Spec.Replicas != int(app.Status.ReadyReplicas) {
			klog.V(4).InfoS("application status not ready", "service", svcName)
			continue
		}
		// add to backendRefs
		port := gatewayv1.PortNumber(8080)
		backendRefs = append(backendRefs, gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: gatewayv1.ObjectName(svcName),
					Port: &port,
				},
				Weight: &ep.Spec.DefaultWeight,
			},
		})
		klog.V(4).InfoS("application service added in http route", "service", svcName)
	}

	for _, app := range disAppList.Items {
		// app already in static route config
		svcName := fmt.Sprintf("%s-router-svc", app.Name)
		if _, exists := staticRouteMap[svcName]; exists {
			klog.V(4).InfoS("application service exist in static route", "service", svcName)
			continue
		}

		if app.Status.Router.ReadyReplicas > 0 &&
			app.Status.Prefill.ReadyReplicas == app.Status.Prefill.Replicas &&
			app.Status.Decode.ReadyReplicas == app.Status.Decode.Replicas {
			klog.Infof("application %s/%s status is ready", app.Namespace, app.Name)
		} else {
			klog.Infof("application %s/%s status is not ready", app.Namespace, app.Name)
			continue
		}

		// add to backendRefs
		port := gatewayv1.PortNumber(8080)
		backendRefs = append(backendRefs, gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: gatewayv1.ObjectName(svcName),
					Port: &port,
				},
				Weight: &ep.Spec.DefaultWeight,
			},
		})
		klog.Infof("application service %s/%s added in http route", app.Namespace, app.Name)
	}

	headerMatch := []gatewayv1.HTTPHeaderMatch{
		{
			Name:  "namespace",
			Value: ep.Namespace,
		},
		{
			Name:  "model",
			Value: ep.Name,
		},
	}
	var ruleMatch []gatewayv1.HTTPRouteMatch
	if ep.Spec.MatchConfigs != nil {
		for _, match := range ep.Spec.MatchConfigs {
			match.Headers = append(match.Headers, headerMatch...)
			ruleMatch = append(ruleMatch, match)
		}
	} else {
		ruleMatch = []gatewayv1.HTTPRouteMatch{
			{Headers: headerMatch},
		}
	}

	routeSpec := gatewayv1.HTTPRouteSpec{
		CommonRouteSpec: gatewayv1.CommonRouteSpec{
			ParentRefs: []gatewayv1.ParentReference{
				ep.Spec.GatewayRef,
			},
		},
		Rules: []gatewayv1.HTTPRouteRule{
			{
				BackendRefs: backendRefs,
				Matches:     ruleMatch,
			},
		},
	}

	var route gatewayv1.HTTPRoute

	err := r.Get(ctx, types.NamespacedName{Name: ep.Name, Namespace: ep.Namespace}, &route)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// create route
		route.Name = ep.Name
		route.Namespace = ep.Namespace
		route.Spec = routeSpec
		if err := r.Create(ctx, &route); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create HTTPRoute: %w", err)
		}
	} else {
		patch := client.MergeFrom(route.DeepCopy())
		route.Spec = routeSpec
		// update route
		if err := r.Patch(ctx, &route, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to patch HTTPRoute: %w", err)
		}
	}

	// klog.InfoS("")
	klog.Infof("HTTPRoute for ArksEndpoint %s/%s updated", ep.Namespace, ep.Name)

	ep.Status.Routes = backendRefs
	if err := r.Client.Status().Update(ctx, ep); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ArksEndpoint status: %w", err)
	}

	return ctrl.Result{}, nil
}

func getArksEndpointNameFromApplication(obj client.Object) string {
	if obj == nil {
		return ""
	}
	app, ok := obj.(*arksv1.ArksApplication)
	if !ok {
		return ""
	}
	if app.Spec.ServedModelName == "" {
		return app.Spec.Model.Name
	}

	return app.Spec.ServedModelName
}

func isArksApplicationReady(obj client.Object) bool {
	if obj == nil {
		return false
	}
	switch appType := obj.(type) {
	case *arksv1.ArksApplication:
		app, ok := obj.(*arksv1.ArksApplication)
		if !ok {
			return false
		}
		return app.Spec.Replicas == int(app.Status.ReadyReplicas)
	case *arksv1.ArksDisaggregatedApplication:
		app, ok := obj.(*arksv1.ArksDisaggregatedApplication)
		if !ok {
			return false
		}
		return app.Status.Router.ReadyReplicas > 0
	default:
		klog.Warningf("unknown application type: %v", appType)
		return false
	}
}

func arksEndpointIncludesAppService(ep *arksv1.ArksEndpoint, appName string) bool {
	svcName := getApplicationServiceName(appName)
	for _, r := range ep.Status.Routes {
		if string(r.BackendRef.Name) == svcName {
			return true
		}
	}
	return false
}

func getApplicationServiceName(appName string) string {
	return fmt.Sprintf("arks-application-%s", appName)
}
