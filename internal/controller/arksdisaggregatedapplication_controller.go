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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	lwsapi "sigs.k8s.io/lws/api/leaderworkerset/v1"
	lwscli "sigs.k8s.io/lws/client-go/clientset/versioned"

	arksv1 "github.com/arks-ai/arks/api/v1"
)

// ArksDisaggregatedApplicationReconciler reconciles a ArksDisaggregatedapplication object
type ArksDisaggregatedApplicationReconciler struct {
	client.Client
	KubeClient *kubernetes.Clientset
	LWSClient  *lwscli.Clientset
	Scheme     *runtime.Scheme
}

// +kubebuilder:rbac:groups=arks.ai,resources=arksdisaggregatedapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arks.ai,resources=arksdisaggregatedapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arks.ai,resources=arksdisaggregatedapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ArksDisaggregatedapplication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ArksDisaggregatedApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here
	application := &arksv1.ArksDisaggregatedApplication{}
	if err := r.Client.Get(ctx, req.NamespacedName, application, &client.GetOptions{
		Raw: &metav1.GetOptions{
			ResourceVersion: "",
		},
	}); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// remove model
	if application.DeletionTimestamp != nil {
		klog.Infof("application %s/%s: remove application", application.Namespace, application.Name)
		return r.remove(ctx, application)
	}

	// reconcile model
	result, err := r.reconcile(ctx, application)

	// update application status
	if err := r.Client.Status().Update(ctx, application); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status for application %s/%s (%s): %q", application.Namespace, application.Name, application.UID, err)
	}

	// handle reconcile error
	if err != nil {
		klog.Errorf("failed to reconcile application %s/%s (%s): %q", application.Namespace, application.Name, application.UID, err)
		return result, err
	}

	return ctrl.Result{}, nil
}

func (r *ArksDisaggregatedApplicationReconciler) remove(ctx context.Context, application *arksv1.ArksDisaggregatedApplication) (ctrl.Result, error) {
	// model is not be deleted
	if application.DeletionTimestamp == nil {
		return ctrl.Result{Requeue: true}, nil
	}

	prefillName := fmt.Sprintf("%s-prefill", application.Name)
	decodeName := fmt.Sprintf("%s-decode", application.Name)
	routerName := fmt.Sprintf("%s-router", application.Name)
	routerSvcName := r.generateApplicationServiceName(application)

	klog.Infof("application %s/%s: start to remove application router service (%s)", application.Namespace, application.Name, routerSvcName)
	if err := r.KubeClient.CoreV1().Services(application.Namespace).Delete(ctx, routerSvcName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete application router service (%s): %q", application.Namespace, application.Name, routerSvcName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete application router service (%s): %q", routerSvcName, err)
		}
	}
	klog.Infof("application %s/%s: remove application router service (%s) successfully", application.Namespace, application.Name, routerSvcName)

	klog.Infof("application %s/%s: start to remove application underlying prefill LWS", application.Namespace, application.Name)
	if err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Delete(ctx, prefillName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete underlying prefill LWS: %q", application.Namespace, prefillName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete underlying prefill LWS: %q", err)
		}
	}
	klog.Infof("application %s/%s: remove application underlying prefill LWS successfully", application.Namespace, application.Name)

	klog.Infof("application %s/%s: start to remove application underlying decode LWS", application.Namespace, application.Name)
	if err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Delete(ctx, decodeName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete underlying decode LWS: %q", application.Namespace, decodeName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete underlying decode LWS: %q", err)
		}
	}
	klog.Infof("application %s/%s: remove application underlying decode LWS successfully", application.Namespace, application.Name)

	klog.Infof("application %s/%s: start to remove application underlying router deployment", application.Namespace, application.Name)
	if err := r.KubeClient.AppsV1().Deployments(application.Namespace).Delete(ctx, routerName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete underlying router deployment: %q", application.Namespace, routerName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete underlying router deployment: %q", err)
		}
	}
	klog.Infof("application %s/%s: remove application underlying router deployment successfully", application.Namespace, application.Name)

	// remove finalizer
	removeFinalizer(application, arksApplicationControllerFinalizer)
	if err := r.Client.Update(ctx, application); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove application finalizer: %q", err)
	}

	klog.Infof("application %s/%s: delete the application successfully", application.Namespace, application.Name)
	return ctrl.Result{}, nil
}

func (r *ArksDisaggregatedApplicationReconciler) reconcile(ctx context.Context, application *arksv1.ArksDisaggregatedApplication) (ctrl.Result, error) {
	if application.DeletionTimestamp != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	if application.Status.Phase == string(arksv1.ArksApplicationPhaseFailed) {
		return ctrl.Result{}, nil
	}

	if application.Status.Phase == "" {
		application.Status.Phase = string(arksv1.ArksApplicationPhasePending)
	}

	r.initializeApplicationCondition(application)

	// precheck: driver &&runtime
	if application.Spec.Runtime == "" {
		application.Spec.Runtime = string(arksv1.ArksRuntimeSGLang)
	}

	if !hasFinalizer(application, arksApplicationControllerFinalizer) {
		addFinalizer(application, arksApplicationControllerFinalizer)

		if err := r.Client.Update(ctx, application); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add application finalizer: %q", err)
		}

		// requeue to refresh application resource version
		return ctrl.Result{
			Requeue: true,
		}, nil
	}

	if !r.checkApplicationCondition(application, arksv1.ArksApplicationPrecheck) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseChecking)
		switch application.Spec.Runtime {
		case string(arksv1.ArksRuntimeSGLang):
		default:
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "RuntimeNotSupport", fmt.Sprintf("LWS not support the specified runtime: %s", application.Spec.Runtime))
			return ctrl.Result{}, nil
		}

		// precheck: volumes
		if err := r.checkApplicationVolumes(&application.Spec.Prefill); err != nil {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "InvalidVolumeDefinitions", err.Error())
			return ctrl.Result{}, nil
		}
		if err := r.checkApplicationVolumes(&application.Spec.Decode); err != nil {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "InvalidVolumeDefinitions", err.Error())
			return ctrl.Result{}, nil
		}

		r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionTrue, "PrecheckPass", "The application passed the pre-checking")
		klog.Infof("application %s/%s: pre-check successfully", application.Namespace, application.Name)
	}

	model := &arksv1.ArksModel{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: application.Namespace, Name: application.Spec.Model.Name}, model, &client.GetOptions{
		Raw: &metav1.GetOptions{
			ResourceVersion: "",
		},
	}); err != nil {
		if apierrors.IsNotFound(err) {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationLoaded, corev1.ConditionFalse, "ModelNotExist", "The referenced model doesn't exist")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// wait model to be ready
	if !r.checkApplicationCondition(application, arksv1.ArksApplicationLoaded) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseLoading)
		switch model.Status.Phase {
		case string(arksv1.ArksModelPhaseFailed):
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationLoaded, corev1.ConditionFalse, "ModelLoadFailed", "Failed to load the referenced model")
			klog.Errorf("application %s/%s: failed to load the referenced model (%s), please check the state of the model", application.Namespace, application.Name, model.Name)
			return ctrl.Result{}, nil
		case string(arksv1.ArksModelReady):
			r.updateApplicationCondition(application, arksv1.ArksApplicationLoaded, corev1.ConditionTrue, "ModelLoadSucceeded", "The referenced model is loaded")
			klog.Infof("application %s/%s: the referenced model (%s) is loaded successfully", application.Namespace, application.Name, model.Name)
		default:
			klog.V(4).Infof("application %s/%s: wait for the referenced model (%s) be loaded", application.Namespace, application.Name, model.Name)
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
	}

	// start model service
	if !r.checkApplicationCondition(application, arksv1.ArksApplicationReady) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseCreating)
		if application.Spec.Prefill.Size < 1 {
			application.Spec.Prefill.Size = 1
		}

		prefillName := fmt.Sprintf("%s-prefill", application.Name)
		if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, prefillName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				lws, err := r.generateDisaggregatedLws(application, model, "prefill")
				if err != nil {
					application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
					r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "UnderlayGenerateFailed", fmt.Sprintf("Failed to generate prefill underlay: %q", err))
					return ctrl.Result{}, fmt.Errorf("failed to generate prefill underlying LWS: %q", err)
				}
				lws.Name = prefillName
				ctrl.SetControllerReference(application, lws, r.Scheme)

				if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Create(ctx, lws, metav1.CreateOptions{}); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlayCreatedFailed", fmt.Sprintf("Failed to create prefill underlay: %q", err))
						klog.Errorf("application %s/%s: failed to create prefill underlying LWS: %q", application.Namespace, application.Name, err)
						return ctrl.Result{}, fmt.Errorf("failed to create prefill underlying LWS: %q", err)
					}
				}
				klog.Infof("application %s/%s: create prefill underlying LWS successfully", application.Namespace, application.Name)
			} else {
				klog.Errorf("application %s/%s: failed to check the prefill underlying LWS: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to check the prefill underlying LWS: %q", err)
			}
		}

		decodeName := fmt.Sprintf("%s-decode", application.Name)
		if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, decodeName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				lws, err := r.generateDisaggregatedLws(application, model, "decode")
				if err != nil {
					application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
					r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "UnderlayGenerateFailed", fmt.Sprintf("Failed to generate decode underlay: %q", err))
					return ctrl.Result{}, fmt.Errorf("failed to generate decode underlying LWS: %q", err)
				}
				lws.Name = decodeName
				ctrl.SetControllerReference(application, lws, r.Scheme)

				if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Create(ctx, lws, metav1.CreateOptions{}); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlayCreatedFailed", fmt.Sprintf("Failed to create decode underlay: %q", err))
						klog.Errorf("application %s/%s: failed to create decode underlying LWS: %q", application.Namespace, application.Name, err)
						return ctrl.Result{}, fmt.Errorf("failed to create decode underlying LWS: %q", err)
					}
				}
				klog.Infof("application %s/%s: create decode underlying LWS successfully", application.Namespace, application.Name)
			} else {
				klog.Errorf("application %s/%s: failed to check the decode underlying LWS: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to check the decode underlying LWS: %q", err)
			}
		}

		routerName := fmt.Sprintf("%s-router", application.Name)
		if _, err := r.KubeClient.AppsV1().Deployments(application.Namespace).Get(ctx, routerName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				deploy, err := r.generateRouterDeployment(ctx, application)
				if err != nil {
					application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
					r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "UnderlayGenerateFailed", fmt.Sprintf("Failed to generate router underlay: %q", err))
					return ctrl.Result{}, fmt.Errorf("failed to generate router underlying deployment: %q", err)
				}
				deploy.Name = routerName
				ctrl.SetControllerReference(application, deploy, r.Scheme)

				if _, err := r.KubeClient.AppsV1().Deployments(application.Namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlayCreatedFailed", fmt.Sprintf("Failed to create router underlay: %q", err))
						klog.Errorf("application %s/%s: failed to create router underlying LWS: %q", application.Namespace, application.Name, err)
						return ctrl.Result{}, fmt.Errorf("failed to create router underlying LWS: %q", err)
					}
				}
				klog.Infof("application %s/%s: create router underlying LWS successfully", application.Namespace, application.Name)
			} else {
				klog.Errorf("application %s/%s: failed to check the router underlying LWS: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to check the router underlying LWS: %q", err)
			}
		}
		routerSvcName := r.generateApplicationServiceName(application)
		if _, err := r.KubeClient.CoreV1().Services(application.Namespace).Get(ctx, routerSvcName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				svc, err := r.generateRouterSvc(application)
				if err != nil {
					application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
					r.updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "UnderlayGenerateFailed", fmt.Sprintf("Failed to generate router underlay: %q", err))
					return ctrl.Result{}, fmt.Errorf("failed to generate router underlay")
				}
				svc.Name = routerSvcName
				ctrl.SetControllerReference(application, svc, r.Scheme)

				if _, err := r.KubeClient.CoreV1().Services(application.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						klog.Errorf("application %s/%s: failed to create application router service: %q", application.Namespace, application.Name, err)
						return ctrl.Result{}, fmt.Errorf("failed to create application router service: %q", err)
					}
				}
				klog.Infof("application %s/%s: create application router service successfully", application.Namespace, application.Name)
			}
		}

		application.Status.Phase = string(arksv1.ArksApplicationPhaseRunning)
		r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionTrue, "Running", "The LLM service is running")
		klog.Infof("application %s/%s: create underlying successfully", application.Namespace, application.Name)
	}

	// sync underly components
	prefillName := fmt.Sprintf("%s-prefill", application.Name)
	if lws, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, prefillName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to query the prefill underlying LWS status: %q", application.Namespace, application.Name, err)
			return ctrl.Result{}, fmt.Errorf("failed to query the prefill underlying LWS status: %q", err)
		} else {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlyingNotExist", "The underlying prefill LWS doesn't exist")
			klog.Errorf("application %s/%s: the underlying prefill LWS doesn't exist", application.Namespace, application.Name)
		}
	} else {
		application.Status.Prefill.Replicas = lws.Status.Replicas
		application.Status.Prefill.ReadyReplicas = lws.Status.ReadyReplicas
		application.Status.Prefill.UpdatedReplicas = lws.Status.UpdatedReplicas

		prefillReplicas := int32(1)
		if application.Spec.Prefill.Replicas != nil && *application.Spec.Prefill.Replicas >= 0 {
			prefillReplicas = *application.Spec.Prefill.Replicas
		}

		needUpdate := false
		if prefillReplicas != *lws.Spec.Replicas {
			klog.Infof("application %s/%s: prefill replicas changed %d", application.Namespace, application.Name, prefillReplicas)
			lws.Spec.Replicas = ptr.To(prefillReplicas)
			needUpdate = true
		}

		// TODO support update decode command (and runtime args)

		// TODO support update podSpec

		if needUpdate {
			if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Update(ctx, lws, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("application %s/%s: failed to update prefill lws: %q", application.Namespace, application.Name, err)
			}
			klog.Infof("application %s/%s: update prefill lws successfully", application.Namespace, application.Name)
		}
	}

	decodeName := fmt.Sprintf("%s-decode", application.Name)
	if lws, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, decodeName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to query the decode underlying LWS status: %q", application.Namespace, application.Name, err)
			return ctrl.Result{}, fmt.Errorf("failed to query the decode underlying LWS status: %q", err)
		} else {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlyingNotExist", "The underlying decode LWS doesn't exist")
			klog.Errorf("application %s/%s: the underlying decode LWS doesn't exist", application.Namespace, application.Name)
		}
	} else {
		application.Status.Decode.Replicas = lws.Status.Replicas
		application.Status.Decode.ReadyReplicas = lws.Status.ReadyReplicas
		application.Status.Decode.UpdatedReplicas = lws.Status.UpdatedReplicas

		decodeReplicas := int32(1)
		if application.Spec.Decode.Replicas != nil && *application.Spec.Decode.Replicas >= 0 {
			decodeReplicas = *application.Spec.Decode.Replicas
		}

		needUpdate := false
		if decodeReplicas != *lws.Spec.Replicas {
			klog.Infof("application %s/%s: decode replicas changed %d", application.Namespace, application.Name, decodeReplicas)
			lws.Spec.Replicas = ptr.To(decodeReplicas)
			needUpdate = true
		}

		// TODO support update decode command (and runtime args)

		// TODO support update podSpec

		if needUpdate {
			if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Update(ctx, lws, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("application %s/%s: failed to update prefill lws: %q", application.Namespace, application.Name, err)
			}
			klog.Infof("application %s/%s: update prefill lws successfully", application.Namespace, application.Name)
		}
	}

	routerName := fmt.Sprintf("%s-router", application.Name)
	if deployment, err := r.KubeClient.AppsV1().Deployments(application.Namespace).Get(ctx, routerName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to query the underlying router deployment status: %q", application.Namespace, application.Name, err)
			return ctrl.Result{}, fmt.Errorf("failed to query the underlying router deployment status: %q", err)
		} else {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			r.updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlyingNotExist", "The underlying router deployment doesn't exist")
			klog.Errorf("application %s/%s: the underlying router deployment doesn't exist", application.Namespace, application.Name)
		}
	} else {
		application.Status.Router.Replicas = deployment.Status.Replicas
		application.Status.Router.ReadyReplicas = deployment.Status.ReadyReplicas
		application.Status.Router.UpdatedReplicas = deployment.Status.UpdatedReplicas

		routerReplicas := int32(1)
		if application.Spec.Router.Replicas != nil && *application.Spec.Router.Replicas > 0 {
			routerReplicas = *application.Spec.Router.Replicas
		}

		needUpdate := false
		if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas != routerReplicas {
			deployment.Spec.Replicas = ptr.To(routerReplicas)
			needUpdate = true
		}

		// TODO support update podSpec

		if needUpdate {
			if _, err := r.KubeClient.AppsV1().Deployments(application.Namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("application %s/%s: failed to update router deployment: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to update router deployment: %q", err)
			}
			klog.Infof("update router deployment successfully")
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArksDisaggregatedApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arksv1.ArksDisaggregatedApplication{}).
		Named("arksdisaggregatedapplication").
		Owns(&lwsapi.LeaderWorkerSet{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func (r *ArksDisaggregatedApplicationReconciler) applyRouterRBAC(ctx context.Context, application *arksv1.ArksDisaggregatedApplication) (string, error) {
	if _, err := r.KubeClient.RbacV1().RoleBindings(application.Namespace).Get(ctx, "sglang-router", metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("failed to check sglang router role binding: %q", err)
		}
		role := rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: application.Namespace,
				Name:      "sglang-router",
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch"},
					Resources: []string{"pods"},
					APIGroups: []string{""},
				},
			},
		}
		if _, err := r.KubeClient.RbacV1().Roles(application.Namespace).Create(ctx, &role, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return "", fmt.Errorf("failed to create sglang router role: %q", err)
			}
		}
		klog.Infof("create sglang router role successfully")

		serviceAccount := corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: application.Namespace,
				Name:      "sglang-router",
			},
		}
		if _, err := r.KubeClient.CoreV1().ServiceAccounts(application.Namespace).Create(ctx, &serviceAccount, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return "", fmt.Errorf("failed to create sglang router service account: %q", err)
			}
		}
		klog.Infof("create sglang router service account successfully")

		roleBinding := rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: application.Namespace,
				Name:      "sglang-router",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "sglang-router",
					Namespace: application.Namespace,
				},
			},
			RoleRef: rbacv1.RoleRef{
				Kind:     "Role",
				Name:     "sglang-router",
				APIGroup: "rbac.authorization.k8s.io",
			},
		}
		if _, err := r.KubeClient.RbacV1().RoleBindings(application.Namespace).Create(ctx, &roleBinding, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return "", fmt.Errorf("failed to create sglang router role binding: %q", err)
			}
		}
		klog.Infof("create sglang router role binding successfully")

		return "sglang-router", nil
	}
	return "sglang-router", nil
}

func (r *ArksDisaggregatedApplicationReconciler) generateRouterDeployment(ctx context.Context, application *arksv1.ArksDisaggregatedApplication) (*appsv1.Deployment, error) {
	serviceAccountName := application.Spec.Router.InstanceSpec.ServiceAccountName
	if serviceAccountName == "" {
		serviceAccount, err := r.applyRouterRBAC(ctx, application)
		if err != nil {
			return nil, fmt.Errorf("failed to apply sglang router rbac: %q", err)
		}
		serviceAccountName = serviceAccount
	}
	port := application.Spec.Router.Port
	if port == 0 {
		port = 8080
	}

	metricPort := application.Spec.Router.MetricPort
	if metricPort == 0 {
		metricPort = 9090
	}

	image, err := r.getApplicationRouterImage(application)
	if err != nil {
		return nil, err
	}

	command, err := r.generateDisaggregationRouterCommand(application, port, metricPort)
	if err != nil {
		return nil, err
	}

	commands := []string{"/bin/bash", "-c", command}
	if application.Spec.Router.CommandOverride != nil {
		commands = application.Spec.Router.CommandOverride
		envs := application.Spec.Router.InstanceSpec.Env
		if len(application.Spec.Router.CommandOverride) > 0 {
			envs = append(envs, corev1.EnvVar{
				Name:  "ARKS_ROUTER_COMMAND",
				Value: command,
			})
			commands = application.Spec.Router.CommandOverride
		}
	}

	replicas := int32(1)
	if application.Spec.Router.Replicas != nil && *application.Spec.Router.Replicas >= 0 {
		replicas = *application.Spec.Router.Replicas
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: application.Namespace,
			Name:      application.Name,
			Labels: map[string]string{
				arksv1.ArksControllerKeyApplication: application.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					arksv1.ArksControllerKeyApplication: application.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: application.Spec.Router.InstanceSpec.Annotations,
					Labels:      r.generateRouterLabels(application),
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: application.Spec.Router.InstanceSpec.TerminationGracePeriodSeconds,
					ActiveDeadlineSeconds:         application.Spec.Router.InstanceSpec.ActiveDeadlineSeconds,
					DNSPolicy:                     application.Spec.Router.InstanceSpec.DNSPolicy,
					DNSConfig:                     application.Spec.Router.InstanceSpec.DNSConfig,
					AutomountServiceAccountToken:  application.Spec.Router.InstanceSpec.AutomountServiceAccountToken,
					NodeName:                      application.Spec.Router.InstanceSpec.NodeName,
					HostNetwork:                   application.Spec.Router.InstanceSpec.HostNetwork,
					HostPID:                       application.Spec.Router.InstanceSpec.HostPID,
					HostIPC:                       application.Spec.Router.InstanceSpec.HostIPC,
					ShareProcessNamespace:         application.Spec.Router.InstanceSpec.ShareProcessNamespace,
					SecurityContext:               application.Spec.Router.InstanceSpec.PodSecurityContext,
					Subdomain:                     application.Spec.Router.InstanceSpec.Subdomain,
					HostAliases:                   application.Spec.Router.InstanceSpec.HostAliases,
					PriorityClassName:             application.Spec.Router.InstanceSpec.PriorityClassName,
					Priority:                      application.Spec.Router.InstanceSpec.Priority,
					RuntimeClassName:              application.Spec.Router.InstanceSpec.RuntimeClassName,
					EnableServiceLinks:            application.Spec.Router.InstanceSpec.EnableServiceLinks,
					PreemptionPolicy:              application.Spec.Router.InstanceSpec.PreemptionPolicy,
					Overhead:                      application.Spec.Router.InstanceSpec.Overhead,
					TopologySpreadConstraints:     application.Spec.Router.InstanceSpec.TopologySpreadConstraints,
					SetHostnameAsFQDN:             application.Spec.Router.InstanceSpec.SetHostnameAsFQDN,
					OS:                            application.Spec.Router.InstanceSpec.OS,
					HostUsers:                     application.Spec.Router.InstanceSpec.HostUsers,
					SchedulingGates:               application.Spec.Router.InstanceSpec.SchedulingGates,
					ResourceClaims:                application.Spec.Router.InstanceSpec.ResourceClaims,

					ServiceAccountName: serviceAccountName,
					SchedulerName:      application.Spec.Router.InstanceSpec.SchedulerName,
					Affinity:           application.Spec.Router.InstanceSpec.Affinity,
					NodeSelector:       application.Spec.Router.InstanceSpec.NodeSelector,
					Tolerations:        application.Spec.Router.InstanceSpec.Tolerations,
					ImagePullSecrets:   application.Spec.RuntimeImagePullSecrets,
					InitContainers:     application.Spec.Router.InstanceSpec.InitContainers,
					Containers: []corev1.Container{
						{
							Name:            "main",
							Image:           image,
							Command:         commands,
							Resources:       application.Spec.Router.InstanceSpec.Resources,
							SecurityContext: application.Spec.Router.InstanceSpec.SecurityContext,
							ReadinessProbe:  application.Spec.Router.InstanceSpec.ReadinessProbe,
							LivenessProbe:   application.Spec.Router.InstanceSpec.LivenessProbe,
							StartupProbe:    application.Spec.Router.InstanceSpec.StartupProbe,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: port,
								},
							},
							VolumeMounts: application.Spec.Router.InstanceSpec.VolumeMounts,
						},
					},
					Volumes: application.Spec.Router.InstanceSpec.Volumes,
				},
			},
		},
	}
	return deploy, nil
}

func (r *ArksDisaggregatedApplicationReconciler) generateRouterSvc(application *arksv1.ArksDisaggregatedApplication) (*corev1.Service, error) {
	port := application.Spec.Router.Port
	if port == 0 {
		port = 8080
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: application.Namespace,
			Name:      fmt.Sprintf("%s-router-svc", application.Name),
			Labels: map[string]string{
				arksv1.ArksControllerKeyApplication: application.Name,
				arksv1.ArksControllerKeyModel:       application.Spec.Model.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				arksv1.ArksControllerKeyApplication:        application.Name,
				arksv1.ArksControllerKeyModel:              application.Spec.Model.Name,
				arksv1.ArksControllerKeyDisaggregationRole: "router",
			},
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: port,
				},
			},
		},
	}

	return svc, nil
}

func (r *ArksDisaggregatedApplicationReconciler) generateDisaggregatedLws(application *arksv1.ArksDisaggregatedApplication, model *arksv1.ArksModel, disaggregatedRole string) (*lwsapi.LeaderWorkerSet, error) {
	image, err := r.getApplicationRuntimeImage(application)
	if err != nil {
		return nil, err
	}

	leaderCommand, err := r.generateDisaggregationLeaderCommand(application, model, disaggregatedRole)
	if err != nil {
		return nil, err
	}

	workerCommand, err := r.generateDisaggregationWorkerCommand(application, model, disaggregatedRole)
	if err != nil {
		return nil, err
	}

	workload := application.Spec.Prefill

	generateLwsLabels := r.generatePrefillWorkloadLwsLabels
	if disaggregatedRole == "decode" {
		workload = application.Spec.Decode
		generateLwsLabels = r.generateDecodeWorkloadLwsLabels
	}

	lwsReplicas := workload.Replicas
	if lwsReplicas == nil || *lwsReplicas < 0 {
		*lwsReplicas = 1
	}
	lwsSize := workload.Size
	if lwsSize < 1 {
		lwsSize = 1
	}
	klog.Infof("application %s/%s(role %s): replicas %d, size: %d", application.Namespace, application.Name, disaggregatedRole, lwsReplicas, lwsSize)

	volumes := []corev1.Volume{
		{
			Name: arksApplicationModelVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: model.Spec.Storage.PVC.Name,
				},
			},
		},
	}
	volumes = append(volumes, workload.InstanceSpec.Volumes...)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      arksApplicationModelVolumeName,
			MountPath: arksApplicationModelVolumeMountPath,
			SubPath:   arksApplicationModelVolumeSubPath,
			ReadOnly:  true,
		},
	}
	volumeMounts = append(volumeMounts, workload.InstanceSpec.VolumeMounts...)

	envs := []corev1.EnvVar{}
	envs = append(envs, workload.InstanceSpec.Env...)
	if application.Spec.Runtime == string(arksv1.ArksRuntimeSGLang) {
		envs = append(envs, corev1.EnvVar{
			Name: "LWS_WORKER_INDEX",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.labels['leaderworkerset.sigs.k8s.io/worker-index']",
				},
			},
		})
	}

	leaderCommands := []string{"/bin/bash", "-c", leaderCommand}
	if len(workload.LeaderCommandOverride) > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "ARKS_LEADER_COMMAND",
			Value: leaderCommand,
		})
		leaderCommands = workload.LeaderCommandOverride
	}
	workerCommands := []string{"/bin/bash", "-c", workerCommand}
	if len(workload.WorkerCommandOverride) > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "ARKS_WORKER_COMMAND",
			Value: workerCommand,
		})
		workerCommands = workload.WorkerCommandOverride
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/health",
				Port: intstr.FromInt(8080),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		TimeoutSeconds:      3,
		FailureThreshold:    120,
	}
	if workload.InstanceSpec.ReadinessProbe != nil {
		readinessProbe = workload.InstanceSpec.ReadinessProbe
	}

	lws := &lwsapi.LeaderWorkerSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: application.Namespace,
			Name:      fmt.Sprintf("%s-%s", application.Name, disaggregatedRole),
			Labels: map[string]string{
				arksv1.ArksControllerKeyApplication: application.Name,
			},
		},
		Spec: lwsapi.LeaderWorkerSetSpec{
			Replicas:      ptr.To(*lwsReplicas),
			StartupPolicy: lwsapi.LeaderCreatedStartupPolicy,
			LeaderWorkerTemplate: lwsapi.LeaderWorkerTemplate{
				RestartPolicy: lwsapi.RecreateGroupOnPodRestart,
				Size:          ptr.To(int32(lwsSize)),
				LeaderTemplate: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: workload.InstanceSpec.Annotations,
						Labels:      generateLwsLabels(application, arksv1.ArksWorkLoadRoleLeader),
					},
					Spec: corev1.PodSpec{
						TerminationGracePeriodSeconds: workload.InstanceSpec.TerminationGracePeriodSeconds,
						ActiveDeadlineSeconds:         workload.InstanceSpec.ActiveDeadlineSeconds,
						DNSPolicy:                     workload.InstanceSpec.DNSPolicy,
						DNSConfig:                     workload.InstanceSpec.DNSConfig,
						AutomountServiceAccountToken:  workload.InstanceSpec.AutomountServiceAccountToken,
						NodeName:                      workload.InstanceSpec.NodeName,
						HostNetwork:                   workload.InstanceSpec.HostNetwork,
						HostPID:                       workload.InstanceSpec.HostPID,
						HostIPC:                       workload.InstanceSpec.HostIPC,
						ShareProcessNamespace:         workload.InstanceSpec.ShareProcessNamespace,
						SecurityContext:               workload.InstanceSpec.PodSecurityContext,
						Subdomain:                     workload.InstanceSpec.Subdomain,
						HostAliases:                   workload.InstanceSpec.HostAliases,
						PriorityClassName:             workload.InstanceSpec.PriorityClassName,
						Priority:                      workload.InstanceSpec.Priority,
						RuntimeClassName:              workload.InstanceSpec.RuntimeClassName,
						EnableServiceLinks:            workload.InstanceSpec.EnableServiceLinks,
						PreemptionPolicy:              workload.InstanceSpec.PreemptionPolicy,
						Overhead:                      workload.InstanceSpec.Overhead,
						TopologySpreadConstraints:     workload.InstanceSpec.TopologySpreadConstraints,
						SetHostnameAsFQDN:             workload.InstanceSpec.SetHostnameAsFQDN,
						OS:                            workload.InstanceSpec.OS,
						HostUsers:                     workload.InstanceSpec.HostUsers,
						SchedulingGates:               workload.InstanceSpec.SchedulingGates,
						ResourceClaims:                workload.InstanceSpec.ResourceClaims,
						ServiceAccountName:            workload.InstanceSpec.ServiceAccountName,
						SchedulerName:                 workload.InstanceSpec.SchedulerName,
						Affinity:                      workload.InstanceSpec.Affinity,
						NodeSelector:                  workload.InstanceSpec.NodeSelector,
						Tolerations:                   workload.InstanceSpec.Tolerations,
						ImagePullSecrets:              application.Spec.RuntimeImagePullSecrets,
						InitContainers:                workload.InstanceSpec.InitContainers,
						Containers: []corev1.Container{
							{
								Name:         "main",
								Image:        image,
								Command:      leaderCommands,
								Resources:    workload.InstanceSpec.Resources,
								VolumeMounts: volumeMounts,
								Env:          envs,
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 8080,
									},
								},
								SecurityContext: workload.InstanceSpec.SecurityContext,
								ReadinessProbe:  readinessProbe,
								LivenessProbe:   workload.InstanceSpec.LivenessProbe,
								StartupProbe:    workload.InstanceSpec.StartupProbe,
							},
						},
						Volumes: volumes,
					},
				},
				WorkerTemplate: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: workload.InstanceSpec.Annotations,
						Labels:      generateLwsLabels(application, arksv1.ArksWorkLoadRoleWorker),
					},
					Spec: corev1.PodSpec{
						TerminationGracePeriodSeconds: workload.InstanceSpec.TerminationGracePeriodSeconds,
						ActiveDeadlineSeconds:         workload.InstanceSpec.ActiveDeadlineSeconds,
						DNSPolicy:                     workload.InstanceSpec.DNSPolicy,
						DNSConfig:                     workload.InstanceSpec.DNSConfig,
						AutomountServiceAccountToken:  workload.InstanceSpec.AutomountServiceAccountToken,
						NodeName:                      workload.InstanceSpec.NodeName,
						HostNetwork:                   workload.InstanceSpec.HostNetwork,
						HostPID:                       workload.InstanceSpec.HostPID,
						HostIPC:                       workload.InstanceSpec.HostIPC,
						ShareProcessNamespace:         workload.InstanceSpec.ShareProcessNamespace,
						SecurityContext:               workload.InstanceSpec.PodSecurityContext,
						Subdomain:                     workload.InstanceSpec.Subdomain,
						HostAliases:                   workload.InstanceSpec.HostAliases,
						PriorityClassName:             workload.InstanceSpec.PriorityClassName,
						Priority:                      workload.InstanceSpec.Priority,
						RuntimeClassName:              workload.InstanceSpec.RuntimeClassName,
						EnableServiceLinks:            workload.InstanceSpec.EnableServiceLinks,
						PreemptionPolicy:              workload.InstanceSpec.PreemptionPolicy,
						Overhead:                      workload.InstanceSpec.Overhead,
						TopologySpreadConstraints:     workload.InstanceSpec.TopologySpreadConstraints,
						SetHostnameAsFQDN:             workload.InstanceSpec.SetHostnameAsFQDN,
						OS:                            workload.InstanceSpec.OS,
						HostUsers:                     workload.InstanceSpec.HostUsers,
						SchedulingGates:               workload.InstanceSpec.SchedulingGates,
						ResourceClaims:                workload.InstanceSpec.ResourceClaims,
						ServiceAccountName:            workload.InstanceSpec.ServiceAccountName,
						SchedulerName:                 workload.InstanceSpec.SchedulerName,
						Affinity:                      workload.InstanceSpec.Affinity,
						NodeSelector:                  workload.InstanceSpec.NodeSelector,
						Tolerations:                   workload.InstanceSpec.Tolerations,
						ImagePullSecrets:              application.Spec.RuntimeImagePullSecrets,
						InitContainers:                workload.InstanceSpec.InitContainers,
						Containers: []corev1.Container{
							{
								Name:            "main",
								Image:           image,
								Command:         workerCommands,
								Resources:       workload.InstanceSpec.Resources,
								VolumeMounts:    volumeMounts,
								Env:             envs,
								SecurityContext: workload.InstanceSpec.SecurityContext,
							},
						},
						Volumes: volumes,
					},
				},
			},
		},
	}

	return lws, nil
}

func (r *ArksDisaggregatedApplicationReconciler) getApplicationRouterImage(application *arksv1.ArksDisaggregatedApplication) (string, error) {
	if application.Spec.RouterImage != "" {
		return application.Spec.RouterImage, nil
	}

	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeSGLang):
		sglangImage := os.Getenv("ARKS_DEFAULT_SGLANG_ROUTER_IMAGE")
		if sglangImage != "" {
			return sglangImage, nil
		}
		sglangImage = os.Getenv("ARKS_DEFAULT_SGLANG_IMAGE")
		if sglangImage != "" {
			return sglangImage, nil
		}
		return "lmsysorg/sglang:v0.5.1.post1-cu126", nil
	default:
		return "", errors.New("unsupported runtime")
	}
}

func (r *ArksDisaggregatedApplicationReconciler) getApplicationRuntimeImage(application *arksv1.ArksDisaggregatedApplication) (string, error) {
	if application.Spec.RuntimeImage != "" {
		return application.Spec.RuntimeImage, nil
	}

	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeSGLang):
		sglangImage := os.Getenv("ARKS_DEFAULT_SGLANG_IMAGE")
		if sglangImage != "" {
			return sglangImage, nil
		}
		return "lmsysorg/sglang:v0.5.1.post1-cu126", nil
	default:
		return "", errors.New("unsupported runtime")
	}
}

func (r *ArksDisaggregatedApplicationReconciler) generateWorkloadLabels(workload arksv1.ArksDisaggregatedWorkload, disaggregatedRole string) map[string]string {
	podLabels := map[string]string{}
	for key, value := range workload.InstanceSpec.Labels {
		podLabels[key] = value
	}
	podLabels[arksv1.ArksControllerKeyDisaggregationRole] = disaggregatedRole
	return podLabels
}

func (r *ArksDisaggregatedApplicationReconciler) generateRouterLabels(application *arksv1.ArksDisaggregatedApplication) map[string]string {
	podLabels := r.generateWorkloadLabels(application.Spec.Decode, "router")
	podLabels[arksv1.ArksControllerKeyApplication] = application.Name
	podLabels[arksv1.ArksControllerKeyModel] = application.Spec.Model.Name

	return podLabels
}

func (r *ArksDisaggregatedApplicationReconciler) generatePrefillWorkloadLwsLabels(application *arksv1.ArksDisaggregatedApplication, role string) map[string]string {
	podLabels := r.generateWorkloadLabels(application.Spec.Prefill, "prefill")
	podLabels[arksv1.ArksControllerKeyApplication] = application.Name
	podLabels[arksv1.ArksControllerKeyModel] = application.Spec.Model.Name
	podLabels[arksv1.ArksControllerKeyWorkLoadRole] = role

	return podLabels
}

func (r *ArksDisaggregatedApplicationReconciler) generateDecodeWorkloadLwsLabels(application *arksv1.ArksDisaggregatedApplication, role string) map[string]string {
	podLabels := r.generateWorkloadLabels(application.Spec.Decode, "decode")
	podLabels[arksv1.ArksControllerKeyApplication] = application.Name
	podLabels[arksv1.ArksControllerKeyModel] = application.Spec.Model.Name
	podLabels[arksv1.ArksControllerKeyWorkLoadRole] = role

	return podLabels
}

func (r *ArksDisaggregatedApplicationReconciler) generateDisaggregationRouterCommand(application *arksv1.ArksDisaggregatedApplication, port, metricPort int32) (string, error) {
	var args string
	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeSGLang):
		args = fmt.Sprintf("python3 -m sglang_router.launch_router --pd-disaggregation --service-discovery --service-discovery-port 8080 --host 0.0.0.0 --port %d", port)
		args = fmt.Sprintf("%s --service-discovery-namespace %s", args, application.Namespace)
		args = fmt.Sprintf("%s --prefill-selector", args)
		prefillLabels := r.generatePrefillWorkloadLwsLabels(application, arksv1.ArksWorkLoadRoleLeader)
		for key, value := range prefillLabels {
			args = fmt.Sprintf("%s %s=%s", args, key, value)
		}
		args = fmt.Sprintf("%s --decode-selector", args)
		decodeLabels := r.generateDecodeWorkloadLwsLabels(application, arksv1.ArksWorkLoadRoleLeader)
		for key, value := range decodeLabels {
			args = fmt.Sprintf("%s %s=%s", args, key, value)
		}
		for _, arg := range application.Spec.Router.RouterArgs {
			args = fmt.Sprintf("%s %s", args, arg)
		}
		if !strings.Contains(args, "-policy") {
			args = fmt.Sprintf("%s --policy cache_aware", args)
		}
		if !strings.Contains(args, "prometheus-port") {
			args = fmt.Sprintf("%s --prometheus-host 0.0.0.0", args)
			args = fmt.Sprintf("%s --prometheus-port %d", args, metricPort)
		}
	default:
		return "", errors.New("unsupported runtime")
	}
	return args, nil
}

func (r *ArksDisaggregatedApplicationReconciler) generateDisaggregationLeaderCommand(application *arksv1.ArksDisaggregatedApplication, model *arksv1.ArksModel, disaggregationRole string) (string, error) {
	workload := application.Spec.Prefill
	if disaggregationRole == "decode" {
		workload = application.Spec.Decode
	}

	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeSGLang):
		args := "python3 -m sglang.launch_server --dist-init-addr $(LWS_LEADER_ADDRESS):20000 --nnodes $(LWS_GROUP_SIZE) --node-rank $(LWS_WORKER_INDEX) --trust-remote-code --host 0.0.0.0 --port 8080 --disaggregation-mode prefill"
		args = fmt.Sprintf("%s --model-path %s", args, generateModelPath(model))
		args = fmt.Sprintf("%s --served-model-name %s", args, r.getServedModelName(application))
		args = fmt.Sprintf("%s --disaggregation-mode %s", args, disaggregationRole)
		for i := range workload.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, workload.RuntimeCommonArgs[i])
		}
		if !strings.Contains(args, "enable-metrics") {
			args = fmt.Sprintf("%s --enable-metrics", args)
		}
		return args, nil
	default:
		return "", errors.New("unsupported runtime")
	}
}

func (r *ArksDisaggregatedApplicationReconciler) generateDisaggregationWorkerCommand(application *arksv1.ArksDisaggregatedApplication, model *arksv1.ArksModel, disaggregationRole string) (string, error) {
	workload := application.Spec.Prefill
	if disaggregationRole == "decode" {
		workload = application.Spec.Decode
	}

	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeSGLang):
		args := "python3 -m sglang.launch_server --dist-init-addr $(LWS_LEADER_ADDRESS):20000 --nnodes $(LWS_GROUP_SIZE) --node-rank $(LWS_WORKER_INDEX) --trust-remote-code"
		args = fmt.Sprintf("%s --model-path %s", args, generateModelPath(model))
		args = fmt.Sprintf("%s --served-model-name %s", args, r.getServedModelName(application))
		args = fmt.Sprintf("%s --disaggregation-mode %s", args, disaggregationRole)
		for i := range workload.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, workload.RuntimeCommonArgs[i])
		}
		if !strings.Contains(args, "enable-metrics") {
			args = fmt.Sprintf("%s --enable-metrics", args)
		}
		return args, nil
	default:
		return "", errors.New("unsupported runtime")
	}
}

func (r *ArksDisaggregatedApplicationReconciler) getServedModelName(application *arksv1.ArksDisaggregatedApplication) string {
	servedModelName := application.Spec.Model.Name
	if application.Spec.ServedModelName != "" {
		servedModelName = application.Spec.ServedModelName
	}
	return servedModelName
}

func (r *ArksDisaggregatedApplicationReconciler) initializeApplicationCondition(application *arksv1.ArksDisaggregatedApplication) {
	if application.Status.Conditions != nil {
		return
	}
	application.Status.Conditions = append(application.Status.Conditions, arksv1.ArksApplicationCondition{
		Type:               arksv1.ArksApplicationPrecheck,
		Status:             corev1.ConditionFalse,
		Reason:             "NewIncomming",
		Message:            "Wait the controller to check the application",
		LastTransitionTime: metav1.Now(),
	})
	application.Status.Conditions = append(application.Status.Conditions, arksv1.ArksApplicationCondition{
		Type:               arksv1.ArksApplicationLoaded,
		Status:             corev1.ConditionFalse,
		Reason:             "NewIncomming",
		Message:            "Wait the controller to load the model",
		LastTransitionTime: metav1.Now(),
	})
	application.Status.Conditions = append(application.Status.Conditions, arksv1.ArksApplicationCondition{
		Type:               arksv1.ArksApplicationReady,
		Status:             corev1.ConditionFalse,
		Reason:             "NewIncomming",
		Message:            "Wait the controller to check the application status",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *ArksDisaggregatedApplicationReconciler) checkApplicationCondition(application *arksv1.ArksDisaggregatedApplication, conditionType arksv1.ArksApplicationConditionType) bool {
	for i := range application.Status.Conditions {
		if application.Status.Conditions[i].Type == conditionType {
			return application.Status.Conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

func (r *ArksDisaggregatedApplicationReconciler) updateApplicationCondition(application *arksv1.ArksDisaggregatedApplication, conditionType arksv1.ArksApplicationConditionType, conditionStatus corev1.ConditionStatus, reason, message string) {
	for i := range application.Status.Conditions {
		if application.Status.Conditions[i].Type == conditionType {
			application.Status.Conditions[i].Status = conditionStatus
			application.Status.Conditions[i].Reason = reason
			application.Status.Conditions[i].Message = message
			application.Status.Conditions[i].LastTransitionTime = metav1.Now()
			return
		}
	}

	application.Status.Conditions = append(application.Status.Conditions, arksv1.ArksApplicationCondition{
		Type:               conditionType,
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func (r *ArksDisaggregatedApplicationReconciler) checkApplicationVolumes(workload *arksv1.ArksDisaggregatedWorkload) error {
	for _, volume := range workload.InstanceSpec.Volumes {
		if volume.Name == arksApplicationModelVolumeName {
			return fmt.Errorf("Volume name 'models' is reserved for ArksModel")
		}
	}
	for _, volumeMount := range workload.InstanceSpec.VolumeMounts {
		if volumeMount.MountPath == arksApplicationModelVolumeMountPath {
			return fmt.Errorf("Volume mount path '/models' is reserved for ArksModel")
		}
	}
	return nil
}

func (r *ArksDisaggregatedApplicationReconciler) generateApplicationServiceName(application *arksv1.ArksDisaggregatedApplication) string {
	return fmt.Sprintf("arks-application-%s", application.Name)
}
