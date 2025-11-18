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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ArksDriver string
type ArksRuntime string
type ArksApplicationPhase string
type ArksApplicationConditionType string

type ArksBackend string

const (
	ArksApplicationPhasePending  ArksApplicationPhase = "Pending"
	ArksApplicationPhaseChecking ArksApplicationPhase = "Checking"
	ArksApplicationPhaseLoading  ArksApplicationPhase = "Loading"
	ArksApplicationPhaseCreating ArksApplicationPhase = "Creating"
	ArksApplicationPhaseRunning  ArksApplicationPhase = "Running"
	ArksApplicationPhaseFailed   ArksApplicationPhase = "Failed"

	// ArksApplicationPrecheck is the condition that indicates if the application is precheck or not.
	ArksApplicationPrecheck ArksApplicationConditionType = "Precheck"
	// ArksApplicationLoaded is the condition that indicates if the model is loaded or not.
	ArksApplicationLoaded ArksApplicationConditionType = "Loaded"
	// ArksApplicationReady is the condition that indicates if the application is ready or not.
	ArksApplicationReady ArksApplicationConditionType = "Ready"

	ArksRuntimeDefault ArksRuntime = "vllm" // The default driver is vLLM
	ArksRuntimeVLLM    ArksRuntime = "vllm"
	ArksRuntimeSGLang  ArksRuntime = "sglang"
	ArksRuntimeDynamo  ArksRuntime = "dynamo"

	// Backend types for workload orchestration
	ArksBackendLWS ArksBackend = "lws" // LeaderWorkerSet backend (no rolling update)
	ArksBackendRBG ArksBackend = "rbg" // RoleBasedGroup backend (supports rolling update)
)

const (
	ArksControllerKeyApplication        = "arks.ai/application"
	ArksControllerKeyModel              = "arks.ai/model"
	ArksControllerKeyToken              = "arks.ai/token"
	ArksControllerKeyQuota              = "arks.ai/quota"
	ArksControllerKeyWorkLoadRole       = "arks.ai/work-load-role"
	ArksControllerKeyDisaggregationRole = "arks.ai/disaggregation-role"
	ArksControllerKeySglangRouter       = "arks.ai/sglang-router"

	ArksWorkLoadRoleLeader = "leader"
	ArksWorkLoadRoleWorker = "worker"
)

// ArksApplicationCondition represents the state of a application.
type ArksApplicationCondition struct {
	Type               ArksApplicationConditionType `json:"type" description:"type of condition ie. Ready|Loaded."`
	Status             corev1.ConditionStatus       `json:"status" description:"status of the condition, one of True, False, Unknown"`
	LastTransitionTime metav1.Time                  `json:"lastTransitionTime,omitempty"`
	Reason             string                       `json:"reason,omitempty" description:"reason for the condition's last transition"`
	Message            string                       `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
type ArksInstanceSpec struct {
	// +optional
	// +kubebuilder:validation:Immutable
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty" protobuf:"varint,4,opt,name=terminationGracePeriodSeconds"`
	// +optional
	// +kubebuilder:validation:Immutable
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty" protobuf:"varint,5,opt,name=activeDeadlineSeconds"`
	// +optional
	// +kubebuilder:validation:Immutable
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty" protobuf:"bytes,6,opt,name=dnsPolicy,casttype=DNSPolicy"`
	// +optional
	// +kubebuilder:validation:Immutable
	DNSConfig *corev1.PodDNSConfig `json:"dnsConfig,omitempty" protobuf:"bytes,26,opt,name=dnsConfig"`
	// +optional
	// +kubebuilder:validation:Immutable
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty" protobuf:"varint,21,opt,name=automountServiceAccountToken"`
	// +optional
	// +kubebuilder:validation:Immutable
	NodeName string `json:"nodeName,omitempty" protobuf:"bytes,10,opt,name=nodeName"`
	// +optional
	// +kubebuilder:validation:Immutable
	HostNetwork bool `json:"hostNetwork,omitempty" protobuf:"varint,11,opt,name=hostNetwork"`
	// +optional
	// +kubebuilder:validation:Immutable
	HostPID bool `json:"hostPID,omitempty" protobuf:"varint,12,opt,name=hostPID"`
	// +optional
	// +kubebuilder:validation:Immutable
	HostIPC bool `json:"hostIPC,omitempty" protobuf:"varint,13,opt,name=hostIPC"`
	// +optional
	// +kubebuilder:validation:Immutable
	ShareProcessNamespace *bool `json:"shareProcessNamespace,omitempty" protobuf:"varint,27,opt,name=shareProcessNamespace"`
	// +optional
	// +kubebuilder:validation:Immutable
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty" protobuf:"bytes,14,opt,name=podSecurityContext"`
	// +optional
	// +kubebuilder:validation:Immutable
	Subdomain string `json:"subdomain,omitempty" protobuf:"bytes,17,opt,name=subdomain"`
	// +optional
	// +kubebuilder:validation:Immutable
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty" patchStrategy:"merge" patchMergeKey:"ip" protobuf:"bytes,23,rep,name=hostAliases"`
	// +optional
	// +kubebuilder:validation:Immutable
	PriorityClassName string `json:"priorityClassName,omitempty" protobuf:"bytes,24,opt,name=priorityClassName"`
	// +optional
	// +kubebuilder:validation:Immutable
	Priority *int32 `json:"priority,omitempty" protobuf:"bytes,25,opt,name=priority"`
	// +optional
	// +kubebuilder:validation:Immutable
	RuntimeClassName *string `json:"runtimeClassName,omitempty" protobuf:"bytes,29,opt,name=runtimeClassName"`
	// +optional
	// +kubebuilder:validation:Immutable
	EnableServiceLinks *bool `json:"enableServiceLinks,omitempty" protobuf:"varint,30,opt,name=enableServiceLinks"`
	// +optional
	// +kubebuilder:validation:Immutable
	PreemptionPolicy *corev1.PreemptionPolicy `json:"preemptionPolicy,omitempty" protobuf:"bytes,31,opt,name=preemptionPolicy"`
	// +optional
	// +kubebuilder:validation:Immutable
	Overhead corev1.ResourceList `json:"overhead,omitempty" protobuf:"bytes,32,opt,name=overhead"`
	// +optional
	// +kubebuilder:validation:Immutable
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty" patchStrategy:"merge" patchMergeKey:"topologyKey" protobuf:"bytes,33,opt,name=topologySpreadConstraints"`
	// +optional
	// +kubebuilder:validation:Immutable
	SetHostnameAsFQDN *bool `json:"setHostnameAsFQDN,omitempty" protobuf:"varint,35,opt,name=setHostnameAsFQDN"`
	// +optional
	// +kubebuilder:validation:Immutable
	OS *corev1.PodOS `json:"os,omitempty" protobuf:"bytes,36,opt,name=os"`
	// +optional
	// +kubebuilder:validation:Immutable
	HostUsers *bool `json:"hostUsers,omitempty" protobuf:"bytes,37,opt,name=hostUsers"`
	// +optional
	// +kubebuilder:validation:Immutable
	SchedulingGates []corev1.PodSchedulingGate `json:"schedulingGates,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,38,opt,name=schedulingGates"`
	// +optional
	// +kubebuilder:validation:Immutable
	ResourceClaims []corev1.PodResourceClaim `json:"resourceClaims,omitempty" patchStrategy:"merge,retainKeys" patchMergeKey:"name" protobuf:"bytes,39,rep,name=resourceClaims"`
	// +optional
	// +kubebuilder:validation:Immutable
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty" protobuf:"bytes,15,opt,name=securityContext"`

	// Resources define the leader/worker container resources.
	// +optional
	// +kubebuilder:validation:Immutable
	Resources corev1.ResourceRequirements `json:"resources"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// +optional
	// +kubebuilder:validation:Immutable
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	// +optional
	// +kubebuilder:validation:Immutable
	Annotations map[string]string `json:"annotations,omitempty"`

	// +optional
	// +kubebuilder:validation:Immutable
	Env []corev1.EnvVar `json:"env,omitempty"`

	// VolumeMounts define the mount point for leader/worker pod.
	// NOTE: the mount point can not be '/models', it is reserved for ArksModel.
	// +optional
	// +kubebuilder:validation:Immutable
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// Volumes define the extra volumes for leader/worker pod, volume name
	// can not be 'models', it is reserved for ArksModel.
	// +optional
	// +kubebuilder:validation:Immutable
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the leader/worker pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	// +mapType=atomic
	// +kubebuilder:validation:Immutable
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// If specified, the pod's scheduling constraints
	// +optional
	// +kubebuilder:validation:Immutable
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// If specified, the pod will be dispatched by specified scheduler.
	// If not specified, the pod will be dispatched by default scheduler.
	// +optional
	// +kubebuilder:validation:Immutable
	SchedulerName string `json:"schedulerName,omitempty"`

	// If specified, the pod's tolerations.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:Immutable
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Periodic probe of container liveness.
	// +optional
	// +kubebuilder:validation:Immutable
	LivenessProbe *corev1.Probe `json:"livenessProbe"`

	// Periodic probe of container readiness.
	// +optional
	// +kubebuilder:validation:Immutable
	ReadinessProbe *corev1.Probe `json:"readinessProbe"`

	// +optional
	// +kubebuilder:validation:Immutable
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty" protobuf:"bytes,22,opt,name=startupProbe"`

	// +optional
	// +kubebuilder:validation:Immutable
	Lifecycle *corev1.Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,12,opt,name=lifecycle"`

	// ServiceAccountName is the name of the ServiceAccount to use to run leader/worker pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	// +optional
	// +kubebuilder:validation:Immutable
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// InitContainers
	// +optional
	// +kubebuilder:validation:Immutable
	InitContainers []corev1.Container `json:"initContainers"`
}

// ArksApplicationSpec defines the desired state of ArksApplication.
type ArksApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +optional
	Replicas int `json:"replicas"`

	// Size defines the inference service group size.
	// Default is 1 for single node inerfence service
	// +optional
	Size int `json:"size"`

	// Runtime defines the inference runtime.
	// Now support: vllm, sglang. Default vLLM.
	// We will support Dynamo in future.
	// +optional
	Runtime string `json:"runtime"` // vLLM, SGLang, Default vLLM.

	// RuntimeImage defines the runtime container image URL.
	// Specify this only when a specific version of the runtime image is required.
	// Customized runtime container images must be compatible with the Runtime.
	// Arks provides a default version of the runtime container image.
	// +optional
	RuntimeImage string `json:"runtimeImage"` // The image of vLLM, SGLang or Dynamo.

	// RuntimeImagePullSecrets defines the runtime image pull secret.
	// You can specify the image pull secrets for the private image registry.
	// +optional
	RuntimeImagePullSecrets []corev1.LocalObjectReference `json:"runtimeImagePullSecrets"`

	Model corev1.LocalObjectReference `json:"model"`

	// ServedModelName defines a custom model name.
	// +optional
	ServedModelName string `json:"servedModelName"`

	// +optional
	TensorParallelSize int `json:"tensorParallelSize"`

	// +optional
	RuntimeCommonArgs []string `json:"runtimeCommonArgs"`

	InstanceSpec ArksInstanceSpec `json:"instanceSpec"`

	// +optional
	// +kubebuilder:validation:Immutable
	PodGroupPolicy *PodGroupPolicy `json:"podGroupPolicy"`
}

// ArksApplicationStatus defines the observed state of ArksApplication.
type ArksApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase string `json:"phase"`

	Replicas        int32 `json:"replicas"`
	ReadyReplicas   int32 `json:"readyReplicas"`
	UpdatedReplicas int32 `json:"updatedReplicas"`

	Conditions []ArksApplicationCondition `json:"conditions,omitempty"`
}

// ArksApplication is the Schema for the arksapplications API.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The current phase of the application"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Replicas",type="string",JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model.name",description="The model being used",priority=1
// +kubebuilder:printcolumn:name="Runtime",type="string",JSONPath=".spec.runtime",description="The runtime environment",priority=1
// +kubebuilder:printcolumn:name="Driver",type="string",JSONPath=".spec.driver",description="The driver name",priority=1
// +kubebuilder:resource:shortName=arkapp

type ArksApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ArksApplicationSpec   `json:"spec,omitempty"`
	Status ArksApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ArksApplicationList contains a list of ArksApplication.
type ArksApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ArksApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ArksApplication{}, &ArksApplicationList{})
}
