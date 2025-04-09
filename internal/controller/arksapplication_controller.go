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
	"time"

	corev1 "k8s.io/api/core/v1"
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

	arksv1 "github.com/scitix/arks/api/v1"
)

const (
	arksApplicationControllerFinalizer  = "application.arks.scitix.ai/controller"
	arksApplicationModelVolumeName      = "models"
	arksApplicationModelVolumeMountPath = "/models"
	arksApplicationModelVolumeSubPath   = "models"
)

// ArksApplicationReconciler reconciles a ArksApplication object
type ArksApplicationReconciler struct {
	client.Client
	KubeClient *kubernetes.Clientset
	LWSClient  *lwscli.Clientset
	Scheme     *runtime.Scheme
}

// +kubebuilder:rbac:groups=arks.scitix.ai,resources=arksapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arks.scitix.ai,resources=arksapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arks.scitix.ai,resources=arksapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=leaderworkerset.x-k8s.io,resources=leaderworkersets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

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

// SetupWithManager sets up the controller with the Manager.
func (r *ArksApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arksv1.ArksApplication{}).
		Named("arksapplication").
		Owns(&lwsapi.LeaderWorkerSet{}).
		Complete(r)
}

func (r *ArksApplicationReconciler) remove(ctx context.Context, application *arksv1.ArksApplication) (ctrl.Result, error) {
	// model is not be deleted
	if application.DeletionTimestamp == nil {
		return ctrl.Result{Requeue: true}, nil
	}

	serviceName := generateApplicationServiceName(application)
	if err := r.KubeClient.CoreV1().Services(application.Namespace).Delete(ctx, serviceName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete application endpoint service: %q", application.Namespace, serviceName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete application endpoint service: %q", err)
		}
	}

	if err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Delete(ctx, application.Name, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("application %s/%s: failed to delete underlying LWS: %q", application.Namespace, serviceName, err)
			return ctrl.Result{}, fmt.Errorf("failed to delete underlying LWS: %q", err)
		}
	}

	// remove finalizer
	removeFinalizer(application, arksModelControllerFinalizer)
	if err := r.Client.Update(ctx, application); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove application finalizer: %q", err)
	}

	klog.Infof("application (%s/%s): delete the application successfully", application.Namespace, application.Name)
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

	// precheck: driver &&runtime
	if application.Spec.Driver == "" {
		application.Spec.Driver = string(arksv1.ArksDriverDefault)
	}
	if application.Spec.Runtime == "" {
		application.Spec.Runtime = string(arksv1.ArksRuntimeDefault)
	}

	if !checkApplicationCondition(application, arksv1.ArksApplicationPrecheck) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseChecking)
		switch application.Spec.Driver {
		case string(arksv1.ArksDriverLWS):
			switch application.Spec.Runtime {
			case string(arksv1.ArksRuntimeVLLM), string(arksv1.ArksRuntimeSGLang):
			default:
				application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
				updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "RuntimeNotSupport", fmt.Sprintf("LWS not support the specified runtime: %s", application.Spec.Runtime))
				return ctrl.Result{}, nil
			}
		default:
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "DriverNotSupport", fmt.Sprintf("Not support the specified runtime: %s", application.Spec.Driver))
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
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
	}

	// start model service
	if !checkApplicationCondition(application, arksv1.ArksApplicationReady) {
		application.Status.Phase = string(arksv1.ArksApplicationPhaseCreating)
		switch application.Spec.Driver {
		case string(arksv1.ArksDriverLWS): // LWS
			if _, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, application.Name, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					lws, err := generateLws(application, model.Spec.Storage.PVC.Name)
					if err != nil {
						application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
						updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "RuntimeNotSupport", "Not support the specified runtime")
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
		default:
			application.Status.Phase = string(arksv1.ArksApplicationPhaseFailed)
			updateApplicationCondition(application, arksv1.ArksApplicationPrecheck, corev1.ConditionFalse, "RuntimeNotSupport", fmt.Sprintf("runtime not support: %s", application.Spec.Runtime))
			return ctrl.Result{}, nil
		}

		// check service
		serviceName := generateApplicationServiceName(application)
		if _, err := r.KubeClient.CoreV1().Services(application.Namespace).Get(ctx, serviceName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: application.Namespace,
						Name:      serviceName,
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
							},
						},
					},
				}

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
	switch application.Spec.Driver {
	case string(arksv1.ArksDriverLWS):
		if lws, err := r.LWSClient.LeaderworkersetV1().LeaderWorkerSets(application.Namespace).Get(ctx, application.Name, metav1.GetOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("application %s/%s: failed to query the underlying LWS status: %q", application.Namespace, application.Name, err)
				return ctrl.Result{}, fmt.Errorf("failed to query the underlying LWS status: %q", err)
			} else {
				application.Status.Phase = string(arksv1.ArksApplicationPhaseRunning)
				updateApplicationCondition(application, arksv1.ArksApplicationReady, corev1.ConditionFalse, "UnderlyingNotExit", "The underlying LWS doesn't exist")
				klog.Errorf("application %s/%s: the underlying LWS doesn't exist", application.Namespace, application.Name)
			}
		} else {
			application.Status.Replicas = lws.Status.Replicas
			application.Status.ReadyReplicas = lws.Status.ReadyReplicas
			application.Status.UpdatedReplicas = lws.Status.UpdatedReplicas
		}
	}

	return ctrl.Result{}, nil
}

func generateLws(application *arksv1.ArksApplication, pvcName string) (*lwsapi.LeaderWorkerSet, error) {
	image, err := getApplicationImage(application)
	if err != nil {
		return nil, err
	}

	leaderCommand, err := generateLeaderCommand(application)
	if err != nil {
		return nil, err
	}

	workerCommand, err := generateWorkerCommand(application)
	if err != nil {
		return nil, err
	}

	lwsReplicas := application.Spec.Replicas
	if lwsReplicas < 0 {
		lwsReplicas = 0
	}
	lwsSize := application.Spec.InstanceSpec.Replicas
	if lwsSize < 1 {
		lwsSize = 1
	}
	klog.Infof("application %s/%s: replicas %d, size: %d", application.Namespace, application.Name, lwsReplicas, lwsSize)

	volumes := []corev1.Volume{
		{
			Name: arksApplicationModelVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}
	volumes = append(volumes, application.Spec.InstanceSpec.Volumes...)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      arksApplicationModelVolumeName,
			MountPath: arksApplicationModelVolumeMountPath,
			SubPath:   arksApplicationModelVolumeSubPath,
			ReadOnly:  true,
		},
	}
	volumeMounts = append(volumeMounts, application.Spec.InstanceSpec.VolumeMounts...)

	envs := []corev1.EnvVar{}
	envs = append(envs, application.Spec.InstanceSpec.Env...)
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
						ServiceAccountName: application.Spec.InstanceSpec.ServiceAccountName,
						SchedulerName:      application.Spec.InstanceSpec.SchedulerName,
						Affinity:           application.Spec.InstanceSpec.Affinity,
						NodeSelector:       application.Spec.InstanceSpec.NodeSelector,
						Tolerations:        application.Spec.InstanceSpec.Tolerations,
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
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										TCPSocket: &corev1.TCPSocketAction{
											Port: intstr.FromInt32(8080),
										},
									},
									InitialDelaySeconds: 15,
									PeriodSeconds:       10,
								},
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
						ServiceAccountName: application.Spec.InstanceSpec.ServiceAccountName,
						SchedulerName:      application.Spec.InstanceSpec.SchedulerName,
						Affinity:           application.Spec.InstanceSpec.Affinity,
						NodeSelector:       application.Spec.InstanceSpec.NodeSelector,
						Tolerations:        application.Spec.InstanceSpec.Tolerations,
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

func getApplicationImage(application *arksv1.ArksApplication) (string, error) {
	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeVLLM):
		return "vllm/vllm-openai:v0.8.2", nil
	case string(arksv1.ArksRuntimeSGLang):
		return "lmsysorg/sglang:v0.4.5-cu124", nil
	default:
		// never reach here
		return "", fmt.Errorf("runtime not support")
	}
}

func generateLeaderCommand(application *arksv1.ArksApplication) ([]string, error) {
	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeVLLM):
		args := "/bin/bash /vllm-workspace/examples/online_serving/multi-node-serving.sh leader --ray_cluster_size=$(LWS_GROUP_SIZE); python3 -m vllm.entrypoints.openai.api_server --port 8080"
		args = fmt.Sprintf("%s --model %s", args, generateModelPath(application.Namespace, application.Spec.Model.Name))
		args = fmt.Sprintf("%s --served-model-name %s", args, application.Spec.ServedModelName)
		if application.Spec.TensorParallelSize > 0 {
			args = fmt.Sprintf("%s --tensor-parallel-size %d", args, application.Spec.TensorParallelSize)
		}
		for i := range application.Spec.ExtraOptions {
			args = fmt.Sprintf("%s %s", args, application.Spec.ExtraOptions[i])
		}
		return []string{"/bin/bash", "-c", args}, nil
	case string(arksv1.ArksRuntimeSGLang):
		args := "python3 -m sglang.launch_server --dist-init-addr $(LWS_LEADER_ADDRESS):20000 --nnodes $(LWS_GROUP_SIZE) --node-rank $(LWS_WORKER_INDEX) --trust-remote-code --host 0.0.0.0 --port 8080"
		args = fmt.Sprintf("%s --model-path /models/%s/%s", args, application.Namespace, application.Spec.Model.Name)
		args = fmt.Sprintf("%s --served-model-name %s", args, application.Spec.ServedModelName)
		if application.Spec.TensorParallelSize > 0 {
			args = fmt.Sprintf("%s --tp %d", args, application.Spec.TensorParallelSize)
		}
		for i := range application.Spec.ExtraOptions {
			args = fmt.Sprintf("%s %s", args, application.Spec.ExtraOptions[i])
		}
		return []string{"/bin/bash", "-c", args}, nil
	default:
		// never reach here
		return nil, fmt.Errorf("runtime not support")
	}
}

func generateWorkerCommand(application *arksv1.ArksApplication) ([]string, error) {
	switch application.Spec.Runtime {
	case string(arksv1.ArksRuntimeVLLM):
		command := []string{"/bin/bash", "-c", "/bin/bash /vllm-workspace/examples/online_serving/multi-node-serving.sh worker --ray_address=$(LWS_LEADER_ADDRESS)"}
		return command, nil
	case string(arksv1.ArksRuntimeSGLang):
		args := "python3 -m sglang.launch_server --dist-init-addr $(LWS_LEADER_ADDRESS):20000 --nnodes $(LWS_GROUP_SIZE) --node-rank $(LWS_WORKER_INDEX) --trust-remote-code"
		args = fmt.Sprintf("%s --model-path /models/%s/%s", args, application.Namespace, application.Spec.Model.Name)
		args = fmt.Sprintf("%s --served-model-name %s", args, application.Spec.ServedModelName)
		if application.Spec.TensorParallelSize > 0 {
			args = fmt.Sprintf("%s --tp %d", args, application.Spec.TensorParallelSize)
		}
		for i := range application.Spec.ExtraOptions {
			args = fmt.Sprintf("%s %s", args, application.Spec.ExtraOptions[i])
		}
		return []string{"/bin/bash", "-c", args}, nil
	default:
		// never reach here
		return nil, fmt.Errorf("runtime not support")
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
