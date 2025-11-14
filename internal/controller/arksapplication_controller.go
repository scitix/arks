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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	lwsapi "sigs.k8s.io/lws/api/leaderworkerset/v1"
	lwscli "sigs.k8s.io/lws/client-go/clientset/versioned"
	rbgv1alpha1 "sigs.k8s.io/rbgs/api/workloads/v1alpha1"

	arksv1 "github.com/arks-ai/arks/api/v1"
)

const (
	arksApplicationControllerFinalizer  = "application.arks.ai/controller"
	arksApplicationModelVolumeName      = "models"
	arksApplicationModelVolumeMountPath = "/models"
)

// ArksApplicationReconciler reconciles a ArksApplication object
type ArksApplicationReconciler struct {
	client.Client
	KubeClient *kubernetes.Clientset
	LWSClient  *lwscli.Clientset
	Scheme     *runtime.Scheme
}

const arksApplicationModelField = "spec.model.name"

// +kubebuilder:rbac:groups=arks.ai,resources=arksapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arks.ai,resources=arksapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arks.ai,resources=arksapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=workloads.x-k8s.io,resources=rolebasedgroupsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=leaderworkerset.x-k8s.io,resources=leaderworkersets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ArksApplication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ArksApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here
	application := &arksv1.ArksApplication{}
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

	original := application.DeepCopy()

	// reconcile model
	result, err := r.reconcile(ctx, application)

	// update application status
	if statusErr := r.patchApplicationStatus(ctx, original, application); statusErr != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status for application %s/%s (%s): %w", application.Namespace, application.Name, application.UID, statusErr)
	}

	// handle reconcile error
	if err != nil {
		klog.Errorf("failed to reconcile application %s/%s (%s): %q", application.Namespace, application.Name, application.UID, err)
		return result, err
	}

	return result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArksApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()

	if err := mgr.GetFieldIndexer().IndexField(ctx, &arksv1.ArksApplication{}, arksApplicationModelField, func(obj client.Object) []string {
		app, ok := obj.(*arksv1.ArksApplication)
		if !ok {
			return nil
		}
		if app.Spec.Model.Name == "" {
			return nil
		}
		return []string{app.Spec.Model.Name}
	}); err != nil {
		return fmt.Errorf("failed to index arksapplication by model: %w", err)
	}

	// Index RBG by owner to optimize List operations in status sync
	if err := mgr.GetFieldIndexer().IndexField(ctx, &rbgv1alpha1.RoleBasedGroup{}, "metadata.ownerReferences.name", func(obj client.Object) []string {
		rbg, ok := obj.(*rbgv1alpha1.RoleBasedGroup)
		if !ok {
			return nil
		}
		ownerRef := metav1.GetControllerOf(rbg)
		if ownerRef == nil {
			return nil
		}
		return []string{ownerRef.Name}
	}); err != nil {
		return fmt.Errorf("failed to index RBG by owner: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 30,
		}).
		For(&arksv1.ArksApplication{}).
		Named("arksapplication").
		Owns(&lwsapi.LeaderWorkerSet{}).
		Owns(&rbgv1alpha1.RoleBasedGroupSet{}).
		Watches(&arksv1.ArksModel{}, handler.EnqueueRequestsFromMapFunc(r.requestsForModel)).
		Watches(&rbgv1alpha1.RoleBasedGroup{}, handler.EnqueueRequestsFromMapFunc(r.requestsForRBG)).
		Complete(r)
}

func (r *ArksApplicationReconciler) remove(ctx context.Context, application *arksv1.ArksApplication) (ctrl.Result, error) {
	// model is not be deleted
	if application.DeletionTimestamp == nil {
		return ctrl.Result{Requeue: true}, nil
	}

	serviceName := generateApplicationServiceName(application)
	klog.Infof("application %s/%s: start to remove application service (%s)", application.Namespace, application.Name, serviceName)
	if err := r.KubeClient.CoreV1().Services(application.Namespace).Delete(ctx, serviceName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete application service (%s): %q", application.Namespace, serviceName, serviceName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete application service (%s): %q", serviceName, err)
		}
	}
	klog.Infof("application %s/%s: remove application service (%s) successfully", application.Namespace, application.Name, serviceName)

	klog.Infof("application %s/%s: start to remove application underlying workload", application.Namespace, application.Name)

	// Try to delete RBGS
	rbgs := &rbgv1alpha1.RoleBasedGroupSet{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: application.Namespace, Name: application.Name}, rbgs); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to check RBGS: %q", application.Namespace, application.Name, err)
			return ctrl.Result{}, fmt.Errorf("failed to check RBGS: %q", err)
		}
	} else {
		if err := r.Client.Delete(ctx, rbgs); err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete underlying RBGS: %q", application.Namespace, application.Name, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete underlying RBGS: %q", err)
		}
		klog.Infof("application %s/%s: remove application underlying RBGS successfully", application.Namespace, application.Name)
	}

	// Try to delete LWS for backward compatibility
	if r.LWSClient != nil {
		if err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Delete(ctx, application.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("application %s/%s: failed to delete underlying LWS: %q", application.Namespace, serviceName, err)
				return ctrl.Result{}, fmt.Errorf("failed to delete underlying LWS: %q", err)
			}
		} else {
			klog.Infof("application %s/%s: remove application underlying LWS successfully", application.Namespace, application.Name)
		}
	}

	// remove finalizer
	if err := r.removeFinalizerWithRetry(ctx, application); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove application finalizer: %w", err)
	}

	klog.Infof("application %s/%s: delete the application successfully", application.Namespace, application.Name)
	return ctrl.Result{}, nil
}

func (r *ArksApplicationReconciler) reconcile(ctx context.Context, application *arksv1.ArksApplication) (ctrl.Result, error) {
	if application.DeletionTimestamp != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	if application.Status.Phase == string(arksv1.ArksApplicationPhaseFailed) {
		return ctrl.Result{}, nil
	}

	if application.Status.Phase == "" {
		application.Status.Phase = string(arksv1.ArksApplicationPhasePending)
	}

	initializeApplicationCondition(application)

	appRuntime := getStandaloneApplicationRuntime(application)

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

	if !checkApplicationCondition(application, arksv1.ArksApplicationPrecheck) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseChecking)
		switch appRuntime {
		case string(arksv1.ArksRuntimeVLLM), string(arksv1.ArksRuntimeSGLang), string(arksv1.ArksRuntimeDynamo):
		default:
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "RuntimeNotSupport", fmt.Sprintf("LWS not support the specified runtime: %s", appRuntime))
			return ctrl.Result{}, nil
		}

		// precheck: volumes
		for _, volume := range application.Spec.InstanceSpec.Volumes {
			if volume.Name == arksApplicationModelVolumeName {
				application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
				updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "ReservedVolumeName", "Volume name 'models' is reserved for ArksModel")
				return ctrl.Result{}, nil
			}
		}
		for _, volumeMount := range application.Spec.InstanceSpec.VolumeMounts {
			if volumeMount.MountPath == arksApplicationModelVolumeMountPath {
				application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
				updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "ReservedVolumeMountPath", "Volume mount path '/models' is reserved for ArksModel")
				return ctrl.Result{}, nil
			}
		}

		updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionTrue, "PrecheckPass", "The application passed the pre-checking")
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
			updateApplicationCondition(application, arksv1.ArksApplicationLoaded, corev1.ConditionFalse, "ModelNotExist", "The referenced model doesn't exist")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// wait model to be ready
	if !checkApplicationCondition(application, arksv1.ArksApplicationLoaded) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseLoading)
		switch model.Status.Phase {
		case string(arksv1.ArksModelPhaseFailed):
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			updateApplicationCondition(application, arksv1.ArksApplicationLoaded, corev1.ConditionFalse, "ModelLoadFailed", "Failed to load the referenced model")
			klog.Errorf("application %s/%s: failed to load the referenced model (%s), please check the state of the model", application.Namespace, application.Name, model.Name)
			return ctrl.Result{}, nil
		case string(arksv1.ArksModelReady):
			updateApplicationCondition(application, arksv1.ArksApplicationLoaded, corev1.ConditionTrue, "ModelLoadSucceeded", "The referenced model is loaded")
			klog.Infof("application %s/%s: the referenced model (%s) is loaded successfully", application.Namespace, application.Name, model.Name)
		default:
			klog.V(4).Infof("application %s/%s: wait for the referenced model (%s) be loaded", application.Namespace, application.Name, model.Name)
			return ctrl.Result{}, nil
		}
	}

	// Detect backend: LWS if exists, otherwise RBG
	backend := r.determineBackend(ctx, application.Namespace, application.Name)
	klog.Infof("application %s/%s: using backend: %s", application.Namespace, application.Name, backend)

	// Always reconcile RBGS (regardless of Ready status) to support rolling updates
	if backend == arksv1.ArksBackendRBG {
		rbgs := &rbgv1alpha1.RoleBasedGroupSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      application.Name,
				Namespace: application.Namespace,
			},
		}

		result, err := controllerutil.CreateOrPatch(ctx, r.Client, rbgs, func() error {
			// Generate desired RBGS spec
			desiredRBGS, err := generateRBGS(application, model)
			if err != nil {
				return fmt.Errorf("failed to generate RBGS: %w", err)
			}

			// Update spec, labels to match desired state
			rbgs.Spec = desiredRBGS.Spec
			rbgs.Labels = desiredRBGS.Labels
			// Annotations not set by generateRBGS, so we don't touch them

			// Set owner reference
			return controllerutil.SetControllerReference(application, rbgs, r.Scheme)
		})

		if err != nil {
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlayReconcileFailed", fmt.Sprintf("Failed to reconcile underlay: %q", err))
			klog.Errorf("application %s/%s: failed to reconcile underlying RBGS: %q", application.Namespace, application.Name, err)
			return ctrl.Result{}, fmt.Errorf("failed to reconcile underlying RBGS: %w", err)
		}

		// Log based on operation result
		switch result {
		case controllerutil.OperationResultCreated:
			klog.Infof("application %s/%s: created underlying RBGS successfully", application.Namespace, application.Name)
		case controllerutil.OperationResultUpdated:
			klog.Infof("application %s/%s: updated underlying RBGS successfully (rolling update triggered)", application.Namespace, application.Name)
		}
	}

	// start model service (initial setup only)
	if !checkApplicationCondition(application, arksv1.ArksApplicationReady) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseCreating)

		// Use appropriate backend (LWS only, RBG already handled above)
		if backend != arksv1.ArksBackendRBG {
			// Use LWS backend (default)
			if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, application.Name, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					lws, err := generateLws(application, model)
					if err != nil {
						application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
						updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "UnderlayGenerateFailed", fmt.Sprintf("Failed to generate underlay: %q", err))
						return ctrl.Result{}, fmt.Errorf("failed to generate underlying LWS: %q", err)
					}
					ctrl.SetControllerReference(application, lws, r.Scheme)

					if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Create(ctx, lws, metav1.CreateOptions{}); err != nil {
						if !apierrors.IsAlreadyExists(err) {
							updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlayCreatedFailed", fmt.Sprintf("Failed to create underlay: %q", err))
							klog.Errorf("application %s/%s: failed to create underlying LWS: %q", application.Namespace, application.Name, err)
							return ctrl.Result{}, fmt.Errorf("failed to create underlying LWS: %q", err)
						}
					}
					klog.Infof("application %s/%s: create underlying LWS successfully", application.Namespace, application.Name)
				} else {
					klog.Errorf("application %s/%s: failed to check the underlying LWS: %q", application.Namespace, application.Name, err)
					return ctrl.Result{}, fmt.Errorf("failed to check the underlying LWS: %q", err)
				}
			}
		}

		// check service
		serviceName := generateApplicationServiceName(application)
		if _, err := r.KubeClient.CoreV1().Services(application.Namespace).Get(ctx, serviceName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: application.Namespace,
						Name:      serviceName,
						Labels: map[string]string{
							"prometheus-discovery": "true",
							"managed-by":           "arks",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							arksv1.ArksControllerKeyApplication:  application.Name,
							arksv1.ArksControllerKeyWorkLoadRole: arksv1.ArksWorkLoadRoleLeader,
						},
						Ports: []corev1.ServicePort{
							{
								Protocol: corev1.ProtocolTCP,
								Port:     8080,
								Name:     "http",
							},
						},
					},
				}
				ctrl.SetControllerReference(application, svc, r.Scheme)

				if _, err := r.KubeClient.CoreV1().Services(application.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						klog.Errorf("application %s/%s: failed to create application service: %q", application.Namespace, application.Name, err)
						return ctrl.Result{}, fmt.Errorf("failed to create application service: %q", err)
					}
				}
				klog.Infof("application %s/%s: create application service successfully", application.Namespace, application.Name)
			} else {
				klog.Errorf("application %s/%s: failed to check the service: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to check the service: %q", err)
			}
		}

		application.Status.Phase = string(arksv1.ArksApplicationPhaseRunning)
		updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionTrue, "Running", "The LLM service is running")
		klog.Infof("application %s/%s: create underlying LWS successfully", application.Namespace, application.Name)
	}

	// sync status
	// backend already declared above, reuse it
	if backend == arksv1.ArksBackendRBG {
		// Only read RBGS status (don't modify RBGS in status sync phase)
		rbgs := &rbgv1alpha1.RoleBasedGroupSet{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: application.Name, Namespace: application.Namespace}, rbgs); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("application %s/%s: failed to query the underlying RBGS status: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to query underlying RBGS status: %w", err)
			}
			// RBGS not found - might be being created
			klog.V(4).Infof("application %s/%s: underlying RBGS not found yet", application.Namespace, application.Name)
		} else {
			// List RBGs owned by this RBGS
			rbgList := &rbgv1alpha1.RoleBasedGroupList{}
			if err := r.Client.List(ctx, rbgList,
				client.InNamespace(application.Namespace),
				client.MatchingFields{"metadata.ownerReferences.name": rbgs.Name},
			); err != nil {
				// Fallback to manual filtering
				if err := r.Client.List(ctx, rbgList, client.InNamespace(application.Namespace)); err != nil {
					klog.Warningf("application %s/%s: failed to list RBGs: %v", application.Namespace, application.Name, err)
					// Reset status
					application.Status.Replicas = 0
					application.Status.ReadyReplicas = 0
					application.Status.UpdatedReplicas = 0
				} else {
					// Filter by owner
					var filteredRBGs []rbgv1alpha1.RoleBasedGroup
					for _, rbg := range rbgList.Items {
						if metav1.GetControllerOf(&rbg) != nil && metav1.GetControllerOf(&rbg).Name == rbgs.Name {
							filteredRBGs = append(filteredRBGs, rbg)
						}
					}
					rbgList.Items = filteredRBGs
				}
			}

			if len(rbgList.Items) == 0 {
				klog.V(4).Infof("application %s/%s: no RBGs found for RBGS %s", application.Namespace, application.Name, rbgs.Name)
				// Reset status
				application.Status.Replicas = 0
				application.Status.ReadyReplicas = 0
				application.Status.UpdatedReplicas = 0
			} else {
				// Use first RBG for status
				// TODO: When RBGS supports multiple replicas, aggregate status from all RBGs
				rbg := &rbgList.Items[0]

				// Sync status from RBG's RoleStatuses
				for _, roleStatus := range rbg.Status.RoleStatuses {
					if roleStatus.Name == "inference" {
						application.Status.Replicas = roleStatus.Replicas
						application.Status.ReadyReplicas = roleStatus.ReadyReplicas
						// UpdatedReplicas from LWS
						lwsName := fmt.Sprintf("%s-inference", rbg.Name)
						if lws, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, lwsName, metav1.GetOptions{}); err == nil {
							application.Status.UpdatedReplicas = lws.Status.UpdatedReplicas
						}
						break
					}
				}
			}
		}
	} else {
		if lws, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, application.Name, metav1.GetOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("application %s/%s: failed to query the underlying LWS status: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to query the underlying LWS status: %q", err)
			}
			application.Status.Phase = string(arksv1.ArksApplicationPhaseRunning)
			application.Status.Replicas = 0
			application.Status.ReadyReplicas = 0
			application.Status.UpdatedReplicas = 0
			updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlyingNotExit", "The underlying LWS doesn't exist")
			klog.Errorf("application %s/%s: the underlying LWS doesn't exist", application.Namespace, application.Name)
		} else {
			application.Status.Replicas = lws.Status.Replicas
			application.Status.ReadyReplicas = lws.Status.ReadyReplicas
			application.Status.UpdatedReplicas = lws.Status.UpdatedReplicas
		}
	}

	return ctrl.Result{}, nil
}

// GenerateLws generates LeaderWorkerSet for ArksApplication
func generateLws(application *arksv1.ArksApplication, model *arksv1.ArksModel) (*lwsapi.LeaderWorkerSet, error) {
	appRuntime := getStandaloneApplicationRuntime(application)

	image, err := getApplicationRuntimeImage(application)
	if err != nil {
		return nil, err
	}

	leaderCommand, err := generateLeaderCommand(application, model)
	if err != nil {
		return nil, err
	}

	workerCommand, err := generateWorkerCommand(application, model)
	if err != nil {
		return nil, err
	}

	lwsReplicas := application.Spec.Replicas
	if lwsReplicas < 0 {
		lwsReplicas = 0
	}
	lwsSize := application.Spec.Size
	if lwsSize < 1 {
		lwsSize = 1
	}
	klog.Infof("application %s/%s: replicas %d, size: %d", application.Namespace, application.Name, lwsReplicas, lwsSize)

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
	volumes = append(volumes, application.Spec.InstanceSpec.Volumes...)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      arksApplicationModelVolumeName,
			MountPath: arksApplicationModelVolumeMountPath,
			ReadOnly:  true,
		},
	}
	volumeMounts = append(volumeMounts, application.Spec.InstanceSpec.VolumeMounts...)

	envs := []corev1.EnvVar{}
	envs = append(envs, application.Spec.InstanceSpec.Env...)
	if appRuntime == string(arksv1.ArksRuntimeSGLang) {
		envs = append(envs, corev1.EnvVar{
			Name: "LWS_WORKER_INDEX",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.labels['leaderworkerset.sigs.k8s.io/worker-index']",
				},
			},
		})
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(8080),
			},
		},
		InitialDelaySeconds: 15,
		PeriodSeconds:       10,
	}
	if application.Spec.InstanceSpec.ReadinessProbe != nil {
		readinessProbe = application.Spec.InstanceSpec.ReadinessProbe
	}

	livenessProbe := application.Spec.InstanceSpec.LivenessProbe

	lws := &lwsapi.LeaderWorkerSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: application.Namespace,
			Name:      application.Name,
			Labels: map[string]string{
				arksv1.ArksControllerKeyApplication: application.Name,
			},
		},
		Spec: lwsapi.LeaderWorkerSetSpec{
			Replicas:      ptr.To(int32(lwsReplicas)),
			StartupPolicy: lwsapi.LeaderCreatedStartupPolicy,
			LeaderWorkerTemplate: lwsapi.LeaderWorkerTemplate{
				RestartPolicy: lwsapi.RecreateGroupOnPodRestart,
				Size:          ptr.To(int32(lwsSize)),
				LeaderTemplate: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: application.Spec.InstanceSpec.Annotations,
						Labels:      generateLwsLabels(application, arksv1.ArksWorkLoadRoleLeader),
					},
					Spec: corev1.PodSpec{
						ServiceAccountName:            application.Spec.InstanceSpec.ServiceAccountName,
						SchedulerName:                 application.Spec.InstanceSpec.SchedulerName,
						Affinity:                      application.Spec.InstanceSpec.Affinity,
						NodeSelector:                  application.Spec.InstanceSpec.NodeSelector,
						Tolerations:                   application.Spec.InstanceSpec.Tolerations,
						TerminationGracePeriodSeconds: application.Spec.InstanceSpec.TerminationGracePeriodSeconds,
						ActiveDeadlineSeconds:         application.Spec.InstanceSpec.ActiveDeadlineSeconds,
						DNSPolicy:                     application.Spec.InstanceSpec.DNSPolicy,
						DNSConfig:                     application.Spec.InstanceSpec.DNSConfig,
						AutomountServiceAccountToken:  application.Spec.InstanceSpec.AutomountServiceAccountToken,
						NodeName:                      application.Spec.InstanceSpec.NodeName,
						HostNetwork:                   application.Spec.InstanceSpec.HostNetwork,
						HostPID:                       application.Spec.InstanceSpec.HostPID,
						HostIPC:                       application.Spec.InstanceSpec.HostIPC,
						ShareProcessNamespace:         application.Spec.InstanceSpec.ShareProcessNamespace,
						SecurityContext:               application.Spec.InstanceSpec.PodSecurityContext,
						Subdomain:                     application.Spec.InstanceSpec.Subdomain,
						HostAliases:                   application.Spec.InstanceSpec.HostAliases,
						PriorityClassName:             application.Spec.InstanceSpec.PriorityClassName,
						Priority:                      application.Spec.InstanceSpec.Priority,
						RuntimeClassName:              application.Spec.InstanceSpec.RuntimeClassName,
						EnableServiceLinks:            application.Spec.InstanceSpec.EnableServiceLinks,
						PreemptionPolicy:              application.Spec.InstanceSpec.PreemptionPolicy,
						Overhead:                      application.Spec.InstanceSpec.Overhead,
						TopologySpreadConstraints:     application.Spec.InstanceSpec.TopologySpreadConstraints,
						SetHostnameAsFQDN:             application.Spec.InstanceSpec.SetHostnameAsFQDN,
						OS:                            application.Spec.InstanceSpec.OS,
						HostUsers:                     application.Spec.InstanceSpec.HostUsers,
						SchedulingGates:               application.Spec.InstanceSpec.SchedulingGates,
						ResourceClaims:                application.Spec.InstanceSpec.ResourceClaims,
						ImagePullSecrets:              application.Spec.RuntimeImagePullSecrets,
						InitContainers:                application.Spec.InstanceSpec.InitContainers,
						Containers: []corev1.Container{
							{
								Name:         "leader",
								Image:        image,
								Command:      leaderCommand,
								Resources:    application.Spec.InstanceSpec.Resources,
								VolumeMounts: volumeMounts,
								Env:          envs,
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 8080,
									},
								},
								ReadinessProbe: readinessProbe,
								LivenessProbe:  livenessProbe,
							},
						},
						Volumes: volumes,
					},
				},
				WorkerTemplate: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: application.Spec.InstanceSpec.Annotations,
						Labels:      generateLwsLabels(application, arksv1.ArksWorkLoadRoleWorker),
					},
					Spec: corev1.PodSpec{
						ServiceAccountName:            application.Spec.InstanceSpec.ServiceAccountName,
						SchedulerName:                 application.Spec.InstanceSpec.SchedulerName,
						Affinity:                      application.Spec.InstanceSpec.Affinity,
						NodeSelector:                  application.Spec.InstanceSpec.NodeSelector,
						Tolerations:                   application.Spec.InstanceSpec.Tolerations,
						TerminationGracePeriodSeconds: application.Spec.InstanceSpec.TerminationGracePeriodSeconds,
						ActiveDeadlineSeconds:         application.Spec.InstanceSpec.ActiveDeadlineSeconds,
						DNSPolicy:                     application.Spec.InstanceSpec.DNSPolicy,
						DNSConfig:                     application.Spec.InstanceSpec.DNSConfig,
						AutomountServiceAccountToken:  application.Spec.InstanceSpec.AutomountServiceAccountToken,
						NodeName:                      application.Spec.InstanceSpec.NodeName,
						HostNetwork:                   application.Spec.InstanceSpec.HostNetwork,
						HostPID:                       application.Spec.InstanceSpec.HostPID,
						HostIPC:                       application.Spec.InstanceSpec.HostIPC,
						ShareProcessNamespace:         application.Spec.InstanceSpec.ShareProcessNamespace,
						SecurityContext:               application.Spec.InstanceSpec.PodSecurityContext,
						Subdomain:                     application.Spec.InstanceSpec.Subdomain,
						HostAliases:                   application.Spec.InstanceSpec.HostAliases,
						PriorityClassName:             application.Spec.InstanceSpec.PriorityClassName,
						Priority:                      application.Spec.InstanceSpec.Priority,
						RuntimeClassName:              application.Spec.InstanceSpec.RuntimeClassName,
						EnableServiceLinks:            application.Spec.InstanceSpec.EnableServiceLinks,
						PreemptionPolicy:              application.Spec.InstanceSpec.PreemptionPolicy,
						Overhead:                      application.Spec.InstanceSpec.Overhead,
						TopologySpreadConstraints:     application.Spec.InstanceSpec.TopologySpreadConstraints,
						SetHostnameAsFQDN:             application.Spec.InstanceSpec.SetHostnameAsFQDN,
						OS:                            application.Spec.InstanceSpec.OS,
						HostUsers:                     application.Spec.InstanceSpec.HostUsers,
						SchedulingGates:               application.Spec.InstanceSpec.SchedulingGates,
						ResourceClaims:                application.Spec.InstanceSpec.ResourceClaims,
						ImagePullSecrets:              application.Spec.RuntimeImagePullSecrets,
						InitContainers:                application.Spec.InstanceSpec.InitContainers,
						Containers: []corev1.Container{
							{
								Name:         "worker",
								Image:        image,
								Command:      workerCommand,
								Resources:    application.Spec.InstanceSpec.Resources,
								VolumeMounts: volumeMounts,
								Env:          envs,
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

func generateRBGS(application *arksv1.ArksApplication, model *arksv1.ArksModel) (*rbgv1alpha1.RoleBasedGroupSet, error) {
	appRuntime := getStandaloneApplicationRuntime(application)

	image, err := getApplicationRuntimeImage(application)
	if err != nil {
		return nil, err
	}

	leaderCommand, err := generateLeaderCommand(application, model)
	if err != nil {
		return nil, err
	}

	workerCommand, err := generateWorkerCommand(application, model)
	if err != nil {
		return nil, err
	}

	rbgsReplicas := int32(application.Spec.Replicas)
	if rbgsReplicas < 0 {
		rbgsReplicas = 0
	}
	lwsSize := int32(application.Spec.Size)
	if lwsSize < 1 {
		lwsSize = 1
	}
	klog.Infof("application %s/%s (RBG): replicas %d, size: %d", application.Namespace, application.Name, rbgsReplicas, lwsSize)

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
	volumes = append(volumes, application.Spec.InstanceSpec.Volumes...)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      arksApplicationModelVolumeName,
			MountPath: arksApplicationModelVolumeMountPath,
			ReadOnly:  true,
		},
	}
	volumeMounts = append(volumeMounts, application.Spec.InstanceSpec.VolumeMounts...)

	envs := []corev1.EnvVar{}
	envs = append(envs, application.Spec.InstanceSpec.Env...)
	if appRuntime == string(arksv1.ArksRuntimeSGLang) {
		envs = append(envs, corev1.EnvVar{
			Name: "LWS_WORKER_INDEX",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.labels['leaderworkerset.sigs.k8s.io/worker-index']",
				},
			},
		})
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt32(8080),
			},
		},
		InitialDelaySeconds: 15,
		PeriodSeconds:       10,
	}
	if application.Spec.InstanceSpec.ReadinessProbe != nil {
		readinessProbe = application.Spec.InstanceSpec.ReadinessProbe
	}

	livenessProbe := application.Spec.InstanceSpec.LivenessProbe

	// Create the base pod spec
	podSpec := corev1.PodSpec{
		TerminationGracePeriodSeconds: application.Spec.InstanceSpec.TerminationGracePeriodSeconds,
		ActiveDeadlineSeconds:         application.Spec.InstanceSpec.ActiveDeadlineSeconds,
		DNSPolicy:                     application.Spec.InstanceSpec.DNSPolicy,
		DNSConfig:                     application.Spec.InstanceSpec.DNSConfig,
		AutomountServiceAccountToken:  application.Spec.InstanceSpec.AutomountServiceAccountToken,
		NodeName:                      application.Spec.InstanceSpec.NodeName,
		HostNetwork:                   application.Spec.InstanceSpec.HostNetwork,
		HostPID:                       application.Spec.InstanceSpec.HostPID,
		HostIPC:                       application.Spec.InstanceSpec.HostIPC,
		ShareProcessNamespace:         application.Spec.InstanceSpec.ShareProcessNamespace,
		SecurityContext:               application.Spec.InstanceSpec.PodSecurityContext,
		Subdomain:                     application.Spec.InstanceSpec.Subdomain,
		HostAliases:                   application.Spec.InstanceSpec.HostAliases,
		PriorityClassName:             application.Spec.InstanceSpec.PriorityClassName,
		Priority:                      application.Spec.InstanceSpec.Priority,
		RuntimeClassName:              application.Spec.InstanceSpec.RuntimeClassName,
		EnableServiceLinks:            application.Spec.InstanceSpec.EnableServiceLinks,
		PreemptionPolicy:              application.Spec.InstanceSpec.PreemptionPolicy,
		Overhead:                      application.Spec.InstanceSpec.Overhead,
		TopologySpreadConstraints:     application.Spec.InstanceSpec.TopologySpreadConstraints,
		SetHostnameAsFQDN:             application.Spec.InstanceSpec.SetHostnameAsFQDN,
		OS:                            application.Spec.InstanceSpec.OS,
		HostUsers:                     application.Spec.InstanceSpec.HostUsers,
		SchedulingGates:               application.Spec.InstanceSpec.SchedulingGates,
		ResourceClaims:                application.Spec.InstanceSpec.ResourceClaims,
		ServiceAccountName:            application.Spec.InstanceSpec.ServiceAccountName,
		SchedulerName:                 application.Spec.InstanceSpec.SchedulerName,
		Affinity:                      application.Spec.InstanceSpec.Affinity,
		NodeSelector:                  application.Spec.InstanceSpec.NodeSelector,
		Tolerations:                   application.Spec.InstanceSpec.Tolerations,
		ImagePullSecrets:              application.Spec.RuntimeImagePullSecrets,
		InitContainers:                application.Spec.InstanceSpec.InitContainers,
		Volumes:                       volumes,
		Containers: []corev1.Container{
			{
				Name:            "instance",
				Image:           image,
				Command:         leaderCommand, // Will be patched for workers
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             envs,
				Resources:       application.Spec.InstanceSpec.Resources,
				VolumeMounts:    volumeMounts,
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 8080,
					},
				},
				SecurityContext: application.Spec.InstanceSpec.SecurityContext,
				ReadinessProbe:  readinessProbe,
				LivenessProbe:   livenessProbe,
				StartupProbe:    application.Spec.InstanceSpec.StartupProbe,
			},
		},
	}

	// Create worker patch
	workerPatch := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{}, // Include metadata to match API server default
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:      "instance",
					Command:   workerCommand,
					Resources: corev1.ResourceRequirements{}, // Include empty resources to match API server default
				},
			},
		},
	}

	workerPatchJSON, err := json.Marshal(workerPatch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker patch: %v", err)
	}

	// Create RoleBasedGroupSet
	rbgs := &rbgv1alpha1.RoleBasedGroupSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: application.Namespace,
			Name:      application.Name,
			Labels: map[string]string{
				arksv1.ArksControllerKeyApplication: application.Name,
			},
		},
		Spec: rbgv1alpha1.RoleBasedGroupSetSpec{
			Replicas: &rbgsReplicas,
			Template: rbgv1alpha1.RoleBasedGroupSpec{
				Roles: []rbgv1alpha1.RoleSpec{
					{
						Name:          "inference",
						Replicas:      ptr.To(int32(1)), // One role per group
						RestartPolicy: rbgv1alpha1.RecreateRoleInstanceOnPodRestart,
						Workload: rbgv1alpha1.WorkloadSpec{
							APIVersion: "leaderworkerset.x-k8s.io/v1",
							Kind:       "LeaderWorkerSet",
						},
						LeaderWorkerSet: rbgv1alpha1.LeaderWorkerTemplate{
							Size: &lwsSize,
							PatchWorkerTemplate: runtime.RawExtension{
								Raw: workerPatchJSON,
							},
						},
						RolloutStrategy: &rbgv1alpha1.RolloutStrategy{
							Type: rbgv1alpha1.RollingUpdateStrategyType,
							RollingUpdate: &rbgv1alpha1.RollingUpdate{
								MaxUnavailable: intstr.FromInt(1),
								MaxSurge:       intstr.FromInt(0),
								Partition:      ptr.To(int32(0)), // Include partition to match API server default
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Annotations: application.Spec.InstanceSpec.Annotations,
								Labels:      generateLwsLabels(application, arksv1.ArksWorkLoadRoleLeader),
							},
							Spec: podSpec,
						},
					},
				},
			},
		},
	}

	return rbgs, nil
}

func generateApplicationServiceName(application *arksv1.ArksApplication) string {
	return fmt.Sprintf("arks-application-%s", application.Name)
}

func generateLwsLabels(application *arksv1.ArksApplication, role string) map[string]string {
	podLabels := map[string]string{}
	for key, value := range application.Spec.InstanceSpec.Labels {
		podLabels[key] = value
	}
	podLabels[arksv1.ArksControllerKeyApplication] = application.Name
	podLabels[arksv1.ArksControllerKeyModel] = application.Spec.Model.Name
	podLabels[arksv1.ArksControllerKeyWorkLoadRole] = role

	return podLabels
}

func getApplicationRuntimeImage(application *arksv1.ArksApplication) (string, error) {
	// Make sure the Runtime match with the RuntimeImage.
	if application.Spec.RuntimeImage != "" {
		return application.Spec.RuntimeImage, nil
	}

	appRuntime := getStandaloneApplicationRuntime(application)

	switch appRuntime {
	case string(arksv1.ArksRuntimeVLLM):
		vllmImage := os.Getenv("ARKS_RUNTIME_DEFAULT_VLLM_IMAGE")
		if vllmImage != "" {
			return vllmImage, nil
		}
		return "vllm/vllm-openai:v0.8.2", nil
	case string(arksv1.ArksRuntimeSGLang):
		sglangImage := os.Getenv("ARKS_RUNTIME_DEFAULT_SGLANG_IMAGE")
		if sglangImage != "" {
			return sglangImage, nil
		}
		return "lmsysorg/sglang:v0.4.5-cu124", nil
	case string(arksv1.ArksRuntimeDynamo):
		dynamoImage := os.Getenv("ARKS_RUNTIME_DEFAULT_DYNAMO_IMAGE")
		if dynamoImage != "" {
			return dynamoImage, nil
		}
		return "scitixai/k8s/dynamo:vllm", nil
		// return "docker.io/scitixai/dynamo:vllm", nil
	default:
		// never reach here
		return "", fmt.Errorf("runtime not support")
	}
}

func generateLeaderCommand(application *arksv1.ArksApplication, model *arksv1.ArksModel) ([]string, error) {
	appRuntime := getStandaloneApplicationRuntime(application)

	switch appRuntime {
	case string(arksv1.ArksRuntimeVLLM):
		args := "/bin/bash /vllm-workspace/examples/online_serving/multi-node-serving.sh leader --ray_cluster_size=$(LWS_GROUP_SIZE); python3 -m vllm.entrypoints.openai.api_server --port 8080"
		args = fmt.Sprintf("%s --model %s", args, generateModelPath(model))
		args = fmt.Sprintf("%s --served-model-name %s", args, getServedModelName(application))
		if application.Spec.TensorParallelSize > 0 {
			args = fmt.Sprintf("%s --tensor-parallel-size %d", args, application.Spec.TensorParallelSize)
		}
		for i := range application.Spec.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, application.Spec.RuntimeCommonArgs[i])
		}
		return []string{"/bin/bash", "-c", args}, nil
	case string(arksv1.ArksRuntimeSGLang):
		args := "python3 -m sglang.launch_server --dist-init-addr $(LWS_LEADER_ADDRESS):20000 --nnodes $(LWS_GROUP_SIZE) --node-rank $(LWS_WORKER_INDEX) --trust-remote-code --host 0.0.0.0 --port 8080"
		args = fmt.Sprintf("%s --model-path %s", args, generateModelPath(model))
		args = fmt.Sprintf("%s --served-model-name %s", args, getServedModelName(application))
		if application.Spec.TensorParallelSize > 0 {
			args = fmt.Sprintf("%s --tp %d", args, application.Spec.TensorParallelSize)
		}
		for i := range application.Spec.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, application.Spec.RuntimeCommonArgs[i])
		}
		if !strings.Contains(args, "enable-metrics") {
			args = fmt.Sprintf("%s --enable-metrics", args)
		}
		return []string{"/bin/bash", "-c", args}, nil
	case string(arksv1.ArksRuntimeDynamo):
		args := "dynamo run in=http out=dyn://$(LWS_LEADER_ADDRESS)"
		for i := range application.Spec.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, application.Spec.RuntimeCommonArgs[i])
		}
		return []string{"/bin/bash", "-c", args}, nil
	default:
		// never reach here
		return nil, fmt.Errorf("runtime not support")
	}
}

func generateWorkerCommand(application *arksv1.ArksApplication, model *arksv1.ArksModel) ([]string, error) {
	appRuntime := getStandaloneApplicationRuntime(application)

	switch appRuntime {
	case string(arksv1.ArksRuntimeVLLM):
		command := []string{"/bin/bash", "-c", "/bin/bash /vllm-workspace/examples/online_serving/multi-node-serving.sh worker --ray_address=$(LWS_LEADER_ADDRESS)"}
		return command, nil
	case string(arksv1.ArksRuntimeSGLang):
		args := "python3 -m sglang.launch_server --dist-init-addr $(LWS_LEADER_ADDRESS):20000 --nnodes $(LWS_GROUP_SIZE) --node-rank $(LWS_WORKER_INDEX) --trust-remote-code"
		args = fmt.Sprintf("%s --model-path %s", args, generateModelPath(model))
		args = fmt.Sprintf("%s --served-model-name %s", args, getServedModelName(application))
		if application.Spec.TensorParallelSize > 0 {
			args = fmt.Sprintf("%s --tp %d", args, application.Spec.TensorParallelSize)
		}
		for i := range application.Spec.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, application.Spec.RuntimeCommonArgs[i])
		}
		if !strings.Contains(args, "enable-metrics") {
			args = fmt.Sprintf("%s --enable-metrics", args)
		}
		return []string{"/bin/bash", "-c", args}, nil
	case string(arksv1.ArksRuntimeDynamo):
		args := fmt.Sprintf("dynamo run in=dyn://$(LWS_LEADER_ADDRESS) out=vllm %s", generateModelPath(model))
		args = fmt.Sprintf("%s --model-name %s", args, getServedModelName(application))
		for i := range application.Spec.RuntimeCommonArgs {
			args = fmt.Sprintf("%s %s", args, application.Spec.RuntimeCommonArgs[i])
		}
		return []string{"/bin/bash", "-c", args}, nil
	default:
		// never reach here
		return nil, fmt.Errorf("runtime not support")
	}
}

func getServedModelName(application *arksv1.ArksApplication) string {
	servedModelName := application.Spec.Model.Name
	if application.Spec.ServedModelName != "" {
		servedModelName = application.Spec.ServedModelName
	}
	return servedModelName
}

func (r *ArksApplicationReconciler) patchApplicationStatus(ctx context.Context, original, updated *arksv1.ArksApplication) error {
	if apiequality.Semantic.DeepEqual(original.Status, updated.Status) {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &arksv1.ArksApplication{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current); err != nil {
			return client.IgnoreNotFound(err)
		}

		current.Status = updated.Status
		return r.Client.Status().Update(ctx, current)
	})
}

func (r *ArksApplicationReconciler) removeFinalizerWithRetry(ctx context.Context, application *arksv1.ArksApplication) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &arksv1.ArksApplication{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(application), current); err != nil {
			return client.IgnoreNotFound(err)
		}

		if !hasFinalizer(current, arksApplicationControllerFinalizer) {
			return nil
		}

		removeFinalizer(current, arksApplicationControllerFinalizer)
		return r.Client.Update(ctx, current)
	})
}

func getStandaloneApplicationRuntime(application *arksv1.ArksApplication) string {
	if application.Spec.Runtime == "" {
		return string(arksv1.ArksRuntimeDefault)
	}
	return application.Spec.Runtime
}

func (r *ArksApplicationReconciler) requestsForModel(ctx context.Context, obj client.Object) []ctrl.Request {
	model, ok := obj.(*arksv1.ArksModel)
	if !ok {
		return nil
	}

	var apps arksv1.ArksApplicationList
	if err := r.Client.List(ctx, &apps,
		client.InNamespace(model.Namespace),
		client.MatchingFields{arksApplicationModelField: model.Name},
	); err != nil {
		klog.Errorf("failed to list applications referencing model %s/%s: %v", model.Namespace, model.Name, err)
		return nil
	}

	requests := make([]ctrl.Request, 0, len(apps.Items))
	for i := range apps.Items {
		requests = append(requests, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      apps.Items[i].Name,
				Namespace: apps.Items[i].Namespace,
			},
		})
	}
	return requests
}

// requestsForRBG maps RBG changes to Application reconcile requests
func (r *ArksApplicationReconciler) requestsForRBG(ctx context.Context, obj client.Object) []ctrl.Request {
	rbg, ok := obj.(*rbgv1alpha1.RoleBasedGroup)
	if !ok {
		return nil
	}

	// Get RBGS from RBG's owner reference
	rbgsRef := metav1.GetControllerOf(rbg)
	if rbgsRef == nil || rbgsRef.Kind != "RoleBasedGroupSet" {
		// RBG not owned by RBGS, skip
		return nil
	}

	// Fetch the RBGS object
	rbgs := &rbgv1alpha1.RoleBasedGroupSet{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      rbgsRef.Name,
		Namespace: rbg.Namespace,
	}, rbgs); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(4).Infof("failed to get RBGS %s/%s for RBG %s: %v", rbg.Namespace, rbgsRef.Name, rbg.Name, err)
		}
		// RBGS not found means it's being deleted, RBG will be garbage collected
		return nil
	}

	// Get Application from RBGS's owner reference
	appRef := metav1.GetControllerOf(rbgs)
	if appRef == nil || appRef.Kind != "ArksApplication" {
		// RBGS not owned by ArksApplication, skip
		return nil
	}

	klog.V(5).Infof("RBG %s/%s changed, triggering reconciliation for application %s", rbg.Namespace, rbg.Name, appRef.Name)

	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      appRef.Name,
				Namespace: rbg.Namespace,
			},
		},
	}
}

func checkApplicationCondition(application *arksv1.ArksApplication, conditionType arksv1.ArksApplicationConditionType) bool {
	for i := range application.Status.Conditions {
		if application.Status.Conditions[i].Type == conditionType {
			return application.Status.Conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

func updateApplicationCondition(application *arksv1.ArksApplication, conditionType arksv1.ArksApplicationConditionType, conditionStatus corev1.ConditionStatus, reason, message string) {
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

func initializeApplicationCondition(application *arksv1.ArksApplication) {
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

// determineBackend detects backend based on existing resources
func (r *ArksApplicationReconciler) determineBackend(
	ctx context.Context,
	namespace string,
	name string,
) arksv1.ArksBackend {
	// Check if LWS exists
	if r.LWSClient != nil {
		if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
			return arksv1.ArksBackendLWS
		}
	}

	// Check if RBGS exists
	rbgs := &rbgv1alpha1.RoleBasedGroupSet{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rbgs); err == nil {
		return arksv1.ArksBackendRBG
	}

	// Default to RBG
	return arksv1.ArksBackendRBG
}
