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
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	arksv1 "github.com/arks-ai/arks/api/v1"
)

const (
	arksModelControllerFinalizer = "model.arks.ai/controller"
)

// ArksModelReconciler reconciles a ArksModel object
type ArksModelReconciler struct {
	client.Client
	KubeClient *kubernetes.Clientset
	Scheme     *runtime.Scheme
}

// +kubebuilder:rbac:groups=arks.ai,resources=arksmodels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arks.ai,resources=arksmodels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arks.ai,resources=arksmodels/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create;get;list;watch;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ArksModel object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ArksModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here
	model := &arksv1.ArksModel{}
	if err := r.Client.Get(ctx, req.NamespacedName, model, &client.GetOptions{
		Raw: &metav1.GetOptions{
			ResourceVersion: "",
		},
	}); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// remove model
	if model.DeletionTimestamp != nil {
		return r.remove(ctx, model)
	}

	// reconcile model
	result, err := r.reconcile(ctx, model)

	// update application status
	if err := r.Client.Status().Update(ctx, model); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status for %s/%s (%s): %q", model.Namespace, model.Name, model.UID, err)
	}

	// handle reconcile error
	if err != nil {
		klog.Errorf("model %s/%s: failed to reconcile: %q", model.Namespace, model.Name, err)
		return result, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArksModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 30,
		}).
		For(&arksv1.ArksModel{}).
		Named("arksmodel").
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func (r *ArksModelReconciler) remove(ctx context.Context, model *arksv1.ArksModel) (ctrl.Result, error) {
	// model is not be deleted
	if model.DeletionTimestamp == nil {
		return ctrl.Result{Requeue: true}, nil
	}

	if model.Spec.Source != nil {
		if model.Spec.Source.Huggingface != nil {
			// remove worker pod
			if err := r.KubeClient.CoreV1().Pods(model.Namespace).Delete(ctx, generateWorkerPodName(model), metav1.DeleteOptions{}); err != nil {
				if !apierrors.IsNotFound(err) {
					klog.Errorf("model %s/%s: failed to delete worker pod: %q", model.Namespace, model.Name, err)
					return ctrl.Result{}, nil
				}
			}
		}
	}

	// remove finalizer
	removeFinalizer(model, arksModelControllerFinalizer)
	if err := r.Client.Update(ctx, model); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove model finalizer: %q", err)
	}

	klog.Infof("model %s/%s: delete model successfully", model.Namespace, model.Name)
	return ctrl.Result{}, nil
}

func (r *ArksModelReconciler) reconcile(ctx context.Context, model *arksv1.ArksModel) (ctrl.Result, error) {
	// model is deleted
	if model.DeletionTimestamp != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	// check status (skip if Succeeded or Failed)
	if model.Status.Phase == string(arksv1.ArksModelPhaseReady) || model.Status.Phase == string(arksv1.ArksModelPhaseFailed) {
		return ctrl.Result{}, nil
	}

	// add finalization
	if !hasFinalizer(model, arksModelControllerFinalizer) {
		addFinalizer(model, arksModelControllerFinalizer)

		if err := r.Client.Update(ctx, model); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add model finalizer: %q", err)
		}

		// requeue to refresh model resource version
		return ctrl.Result{
			Requeue: true,
		}, nil
	}

	// initialize the model condition firstly.
	initializeModelCondition(model)

	// create PVC for the model storage if not exist
	var storagePvcName string
	if model.Spec.Storage.PVC != nil {
		if !checkModelCondition(model, arksv1.ArksModelStorageCreated) {
			model.Status.Phase = string(arksv1.ArksModelPhaseStorageCreating)

			// check model volume
			pvc := model.Spec.Storage.PVC
			storagePvcName = pvc.Name
			if storagePvcName == "" {
				storagePvcName = model.Name
			}

			if _, err := r.KubeClient.CoreV1().PersistentVolumeClaims(model.Namespace).Get(ctx, storagePvcName, metav1.GetOptions{ResourceVersion: ""}); err != nil {
				if apierrors.IsNotFound(err) {
					// create pvc
					newPvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: model.Namespace,
							Name:      storagePvcName,
							Labels: map[string]string{
								arksv1.ArksControllerKeyModel: model.Name,
							},
						},
						Spec: pvc.Spec,
					}

					ctrl.SetControllerReference(model, newPvc, r.Scheme)
					if _, err := r.KubeClient.CoreV1().PersistentVolumeClaims(model.Namespace).Create(ctx, newPvc, metav1.CreateOptions{}); err != nil {
						if !apierrors.IsAlreadyExists(err) {
							updateModelCondition(model, arksv1.ArksModelStorageCreated, corev1.ConditionFalse, "BoundFailed", fmt.Sprintf("Failed to create storage volume (%s/%s): %q", model.Namespace, pvc.Name, err))
							return ctrl.Result{}, fmt.Errorf("failed to create storage volume (%s/%s): %q", model.Namespace, pvc.Name, err)
						}
					}
				} else {
					return ctrl.Result{}, fmt.Errorf("failed to check storage volume (%s/%s) : %q", model.Namespace, pvc.Name, err)
				}
			}
			updateModelCondition(model, arksv1.ArksModelStorageCreated, corev1.ConditionTrue, "CreateSucceeded", fmt.Sprintf("Create model storage successfully(%s)", pvc.Name))
			klog.Infof("model %s/%s: create model storage (%s) successfully", model.Namespace, model.Name, storagePvcName)
		}
	} else {
		model.Status.Phase = string(arksv1.ArksModelPhaseFailed)
		updateModelCondition(model, arksv1.ArksModelStorageCreated, corev1.ConditionFalse, "CreateFailed", "Failed to create model storage: no storage PVC specified")
		return ctrl.Result{}, nil
	}

	if !checkModelCondition(model, arksv1.ArksModelModelLoaded) {
		if model.Spec.Source != nil {
			if model.Spec.Source.Huggingface != nil {
				// create a worker pod to load the model:
				workerName := generateWorkerPodName(model)
				if currentWorker, err := r.KubeClient.CoreV1().Pods(model.Namespace).Get(ctx, workerName, metav1.GetOptions{}); err != nil {
					envs := []corev1.EnvVar{
						{
							Name:  "MODEL_NAME",
							Value: model.Spec.Model,
						},
						{
							Name:  "MODEL_PATH",
							Value: generateModelPath(model),
						},
					}

					if model.Spec.Source.Huggingface.TokenSecretRef != nil {
						envs = append(envs, corev1.EnvVar{
							Name: "HF_TOKEN",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: *model.Spec.Source.Huggingface.TokenSecretRef,
									Key:                  "HF_TOKEN",
								},
							},
						})
					}
					if apierrors.IsNotFound(err) {
						workerPod := &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      workerName,
								Namespace: model.Namespace,
								Labels: map[string]string{
									arksv1.ArksControllerKeyModel: model.Name,
								},
							},
							Spec: corev1.PodSpec{
								NodeSelector:                  model.Spec.InstanceSpec.NodeSelector,
								SchedulerName:                 model.Spec.InstanceSpec.SchedulerName,
								Affinity:                      model.Spec.InstanceSpec.Affinity,
								Tolerations:                   model.Spec.InstanceSpec.Tolerations,
								TerminationGracePeriodSeconds: model.Spec.InstanceSpec.TerminationGracePeriodSeconds,
								ActiveDeadlineSeconds:         model.Spec.InstanceSpec.ActiveDeadlineSeconds,
								DNSPolicy:                     model.Spec.InstanceSpec.DNSPolicy,
								DNSConfig:                     model.Spec.InstanceSpec.DNSConfig,
								AutomountServiceAccountToken:  model.Spec.InstanceSpec.AutomountServiceAccountToken,
								NodeName:                      model.Spec.InstanceSpec.NodeName,
								HostNetwork:                   model.Spec.InstanceSpec.HostNetwork,
								HostPID:                       model.Spec.InstanceSpec.HostPID,
								HostIPC:                       model.Spec.InstanceSpec.HostIPC,
								ShareProcessNamespace:         model.Spec.InstanceSpec.ShareProcessNamespace,
								SecurityContext:               model.Spec.InstanceSpec.PodSecurityContext,
								Subdomain:                     model.Spec.InstanceSpec.Subdomain,
								HostAliases:                   model.Spec.InstanceSpec.HostAliases,
								PriorityClassName:             model.Spec.InstanceSpec.PriorityClassName,
								Priority:                      model.Spec.InstanceSpec.Priority,
								RuntimeClassName:              model.Spec.InstanceSpec.RuntimeClassName,
								EnableServiceLinks:            model.Spec.InstanceSpec.EnableServiceLinks,
								PreemptionPolicy:              model.Spec.InstanceSpec.PreemptionPolicy,
								Overhead:                      model.Spec.InstanceSpec.Overhead,
								TopologySpreadConstraints:     model.Spec.InstanceSpec.TopologySpreadConstraints,
								SetHostnameAsFQDN:             model.Spec.InstanceSpec.SetHostnameAsFQDN,
								OS:                            model.Spec.InstanceSpec.OS,
								HostUsers:                     model.Spec.InstanceSpec.HostUsers,
								SchedulingGates:               model.Spec.InstanceSpec.SchedulingGates,
								ResourceClaims:                model.Spec.InstanceSpec.ResourceClaims,
								ServiceAccountName:            model.Spec.InstanceSpec.ServiceAccountName,
								ImagePullSecrets:              model.Spec.ImagePullSecrets,

								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:    "worker",
										Image:   getScriptsImage(),
										Command: []string{"/bin/bash", "-c", "python3 /scripts/download.py"},
										Env:     envs,

										TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,

										ReadinessProbe: model.Spec.InstanceSpec.ReadinessProbe,
										LivenessProbe:  model.Spec.InstanceSpec.LivenessProbe,

										VolumeMounts: []corev1.VolumeMount{
											{
												Name:      arksApplicationModelVolumeName,
												MountPath: arksApplicationModelVolumeMountPath,
											},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: arksApplicationModelVolumeName,
										VolumeSource: corev1.VolumeSource{
											PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: storagePvcName,
											},
										},
									},
								},
							},
						}
						ctrl.SetControllerReference(model, workerPod, r.Scheme)

						if _, err := r.KubeClient.CoreV1().Pods(model.Namespace).Create(ctx, workerPod, metav1.CreateOptions{}); err != nil {
							if !apierrors.IsAlreadyExists(err) {
								model.Status.Phase = string(arksv1.ArksModelPhaseModelLoading)
								updateModelCondition(model, arksv1.ArksModelModelLoaded, corev1.ConditionFalse, "CreateWorkerPodFailed", fmt.Sprintf("Failed to create worker pod to load model: %q", err))
								return ctrl.Result{}, fmt.Errorf("failed to create worker pod to load model: %q", err)
							}
						}

						klog.Infof("model %s/%s: create worker pod successfully", model.Namespace, model.Name)
						model.Status.Phase = string(arksv1.ArksModelPhaseModelLoading)
						updateModelCondition(model, arksv1.ArksModelModelLoaded, corev1.ConditionFalse, "ModelLoading", "The model is loading now")

						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to query worker pod for loading model: %q", err)
				} else {
					switch currentWorker.Status.Phase {
					case corev1.PodFailed:
						workerPodTerminiationMessage := getJobFailureMessage(currentWorker)
						model.Status.Phase = string(arksv1.ArksModelPhaseFailed)
						updateModelCondition(model, arksv1.ArksModelModelLoaded, corev1.ConditionFalse, "ModelLoadFailed", fmt.Sprintf("Failed to load the model: %s", workerPodTerminiationMessage))
						klog.Infof("model %s/%s: failed to load the model: %q", model.Namespace, model.Name, workerPodTerminiationMessage)
						return ctrl.Result{}, nil
					case corev1.PodSucceeded:
						klog.Infof("model %s/%s: load the model successfully", model.Namespace, model.Name)
						updateModelCondition(model, arksv1.ArksModelModelLoaded, corev1.ConditionTrue, "ModelLoadSucceeded", "Load the model successfully")
					default:
						klog.V(4).Infof("model %s/%s: worker pod is in phase: %s", model.Namespace, workerName, currentWorker.Status.Phase)
						return ctrl.Result{}, nil
					}
				}
			}
		} else {
			klog.Infof("model %s/%s: model is in existing storage , skip model loading", model.Namespace, model.Name)
			updateModelCondition(model, arksv1.ArksModelModelLoaded, corev1.ConditionTrue, "ModelLoadSucceeded", "The model is in existing storage")
		}
	}

	if !checkModelCondition(model, arksv1.ArksModelReady) {
		model.Status.Phase = string(arksv1.ArksModelPhaseReady)
		updateModelCondition(model, arksv1.ArksModelReady, corev1.ConditionTrue, "Ready", "The model is ready now")
	}

	return ctrl.Result{}, nil
}

func getScriptsImage() string {
	scriptsImage := os.Getenv("ARKS_SCRIPTS_IMAGE")
	if scriptsImage != "" {
		return scriptsImage
	}
	return "scitixai/arks-scripts:latest"
}

func generateModelPath(model *arksv1.ArksModel) string {
	if model.Spec.Storage.SubPath != "" {
		return fmt.Sprintf("/models/%s", model.Spec.Storage.SubPath)
	}
	return fmt.Sprintf("/models/models/%s/%s", model.Namespace, model.Name)
}

func generateWorkerPodName(model *arksv1.ArksModel) string {
	return fmt.Sprintf("arks-worker-%s", model.Name)
}

func getJobFailureMessage(wokerPod *corev1.Pod) string {
	for _, status := range wokerPod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			return status.State.Waiting.Message
		}
		if status.State.Terminated != nil {
			return status.State.Terminated.Message
		}
	}

	return "Unknown failure reason"
}

func checkModelCondition(model *arksv1.ArksModel, conditionType arksv1.ArksModelConditionType) bool {
	for i := range model.Status.Conditions {
		if model.Status.Conditions[i].Type == conditionType {
			return model.Status.Conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

func updateModelCondition(model *arksv1.ArksModel, conditionType arksv1.ArksModelConditionType, conditionStatus corev1.ConditionStatus, reason, message string) {
	for i := range model.Status.Conditions {
		if model.Status.Conditions[i].Type == conditionType {
			model.Status.Conditions[i].Status = conditionStatus
			model.Status.Conditions[i].Reason = reason
			model.Status.Conditions[i].Message = message
			model.Status.Conditions[i].LastTransitionTime = metav1.Now()
			return
		}
	}

	model.Status.Conditions = append(model.Status.Conditions, arksv1.ArksModelCondition{
		Type:               conditionType,
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func initializeModelCondition(model *arksv1.ArksModel) {
	if model.Status.Conditions != nil {
		return
	}
	model.Status.Conditions = append(model.Status.Conditions, arksv1.ArksModelCondition{
		Type:               arksv1.ArksModelStorageCreated,
		Status:             corev1.ConditionFalse,
		Reason:             "NewIncomming",
		Message:            "Wait the controller to prepare the storage",
		LastTransitionTime: metav1.Now(),
	})
	model.Status.Conditions = append(model.Status.Conditions, arksv1.ArksModelCondition{
		Type:               arksv1.ArksModelModelLoaded,
		Status:             corev1.ConditionFalse,
		Reason:             "NewIncomming",
		Message:            "Wait the controller to load the model",
		LastTransitionTime: metav1.Now(),
	})
	model.Status.Conditions = append(model.Status.Conditions, arksv1.ArksModelCondition{
		Type:               arksv1.ArksModelReady,
		Status:             corev1.ConditionFalse,
		Reason:             "NewIncomming",
		Message:            "Wait the controller to check the model status",
		LastTransitionTime: metav1.Now(),
	})
}

func hasFinalizer(object metav1.Object, finalizer string) bool {
	for _, f := range object.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

func removeFinalizer(object metav1.Object, finalizer string) {
	filtered := []string{}
	for _, f := range object.GetFinalizers() {
		if f != finalizer {
			filtered = append(filtered, f)
		}
	}
	object.SetFinalizers(filtered)
}

func addFinalizer(object metav1.Object, finalizer string) {
	if hasFinalizer(object, finalizer) {
		return
	}
	object.SetFinalizers(append(object.GetFinalizers(), finalizer))
}
