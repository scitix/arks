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
type ArksApplicationMode string
type ArksApplicationTrafficTarget string

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
	// ArksApplicationTrafficTargetReady is the condition that indicates service traffic is routed to a ready target.
	ArksApplicationTrafficTargetReady ArksApplicationConditionType = "TrafficTargetReady"

	ArksRuntimeDefault ArksRuntime = "vllm" // The default driver is vLLM
	ArksRuntimeVLLM    ArksRuntime = "vllm"
	ArksRuntimeSGLang  ArksRuntime = "sglang"
	ArksRuntimeDynamo  ArksRuntime = "dynamo"

	ArksApplicationModeUnified       ArksApplicationMode = "unified"
	ArksApplicationModeDisaggregated ArksApplicationMode = "disaggregated"

	// ArksApplicationTrafficTargetEngine indicates the Service routes traffic
	// directly to the inference engine pod (i.e., the unified role).
	ArksApplicationTrafficTargetEngine  ArksApplicationTrafficTarget = "engine"
	ArksApplicationTrafficTargetRouter  ArksApplicationTrafficTarget = "router"
	ArksApplicationTrafficTargetPending ArksApplicationTrafficTarget = "pending"
)

const (
	ArksControllerKeyApplication = "arks.ai/application"
	ArksControllerKeyModel       = "arks.ai/model"
	ArksControllerKeyToken       = "arks.ai/token"
	ArksControllerKeyQuota       = "arks.ai/quota"
	// ArksControllerKeyWorkLoadRole identifies leader/worker within a role's pods.
	ArksControllerKeyWorkLoadRole = "arks.ai/work-load-role"
	// ArksControllerKeyRole identifies the ArksApplication role
	// (unified / router / prefill / decode) for Service selector and discovery.
	// New ArksApplication CRD uses this key.
	ArksControllerKeyRole         = "arks.ai/role"
	ArksControllerKeySglangRouter = "arks.ai/sglang-router"
	// ArksControllerKeyWorkerGroup identifies which workload group
	// (heterogeneous engine configuration, e.g. a prefill group) a pod
	// belongs to within a role. Only set when the role is rendered from
	// spec.prefillGroups; absent for single-workload roles.
	ArksControllerKeyWorkerGroup = "arks.ai/worker-group"

	ArksWorkLoadRoleLeader = "leader"
	ArksWorkLoadRoleWorker = "worker"
)

// ============================================================================
// LEGACY: symbols below this block exist only to support the frozen
// ArksDisaggregatedApplication CRD (a transitional CRD kept for backward
// compatibility). They are NOT referenced by ArksApplication code paths.
//
// When ArksDisaggregatedApplication is removed in a future release, every
// symbol in this block can be deleted along with the legacy CRD's types,
// controller, and scheme registration.
//
// Do NOT add new references to these from new ArksApplication code paths.
// ============================================================================

// ArksBackend selects the workload backend type used by the legacy
// ArksDisaggregatedApplication controller.
//
// Deprecated: legacy ArksDisaggregatedApplication only.
type ArksBackend string

const (
	// ArksBackendLWS / ArksBackendRBG are used by the legacy
	// ArksDisaggregatedApplication controller to dispatch workload generation.
	//
	// Deprecated: legacy ArksDisaggregatedApplication only.
	ArksBackendLWS ArksBackend = "lws"
	ArksBackendRBG ArksBackend = "rbg"

	// ArksControllerKeyDisaggregationRole is the Pod label key used by the
	// legacy ArksDisaggregatedApplication controller (router / prefill / decode).
	// New ArksApplication uses ArksControllerKeyRole ("arks.ai/role") instead.
	//
	// Deprecated: legacy ArksDisaggregatedApplication only.
	ArksControllerKeyDisaggregationRole = "arks.ai/disaggregation-role"
)

// ArksRoleStatus reports the status of a single ArksApplication role
// (unified / router / prefill / decode).
type ArksRoleStatus struct {
	Replicas        int32 `json:"replicas"`
	ReadyReplicas   int32 `json:"readyReplicas"`
	UpdatedReplicas int32 `json:"updatedReplicas"`
}

// ArksGroupStatus reports the status of one workload group (for example one
// prefill group) within a role.
type ArksGroupStatus struct {
	// Name is the group name from spec (for example spec.prefillGroups[].name).
	Name           string `json:"name"`
	ArksRoleStatus `json:",inline"`
}

// ArksPodGroupPolicy is the ArksApplication-owned PodGroup configuration for
// gang-scheduling. It mirrors the structure of the legacy PodGroupPolicy used
// by ArksDisaggregatedApplication but is defined independently so the new CRD
// does not depend on the legacy CRD's types.
type ArksPodGroupPolicy struct {
	ArksPodGroupPolicySource `json:",inline"`
}

// ArksPodGroupPolicySource enumerates supported gang-scheduling plugins.
// Only one of its members may be specified.
type ArksPodGroupPolicySource struct {
	// KubeScheduling plugin from the Kubernetes scheduler-plugins for gang-scheduling.
	KubeScheduling *ArksKubeSchedulingPodGroupPolicySource `json:"kubeScheduling,omitempty"`

	// VolcanoScheduling plugin for Volcano gang-scheduling.
	VolcanoScheduling *ArksVolcanoSchedulingPodGroupPolicySource `json:"volcanoScheduling,omitempty"`
}

// ArksKubeSchedulingPodGroupPolicySource configures the kube-scheduler-plugins backend.
// The number of min members in the PodGroupSpec is always equal to the number of rbg pods.
type ArksKubeSchedulingPodGroupPolicySource struct {
	// Time threshold to schedule PodGroup for gang-scheduling.
	// Defaults to 60 seconds.
	// +kubebuilder:default=60
	ScheduleTimeoutSeconds *int32 `json:"scheduleTimeoutSeconds,omitempty"`
}

// ArksVolcanoSchedulingPodGroupPolicySource configures the Volcano backend.
type ArksVolcanoSchedulingPodGroupPolicySource struct {
	// If specified, indicates the PodGroup's priority. "system-node-critical" and
	// "system-cluster-critical" are two special keywords which indicate the
	// highest priorities with the former being the highest priority.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// Queue defines the queue to allocate resource for PodGroup; if queue does not exist,
	// the PodGroup will not be scheduled. Defaults to `default` Queue with the lowest weight.
	// +optional
	Queue string `json:"queue,omitempty"`
}

// ArksApplicationCondition represents the state of a application.
type ArksApplicationCondition struct {
	Type               ArksApplicationConditionType `json:"type" description:"type of condition ie. Ready|Loaded."`
	Status             corev1.ConditionStatus       `json:"status" description:"status of the condition, one of True, False, Unknown"`
	LastTransitionTime metav1.Time                  `json:"lastTransitionTime,omitempty"`
	Reason             string                       `json:"reason,omitempty" description:"reason for the condition's last transition"`
	Message            string                       `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// CoordinationPolicy controls the coordination strategy for prefill and decode roles.
// When configured, prefill and decode deployment/update will proceed in a coordinated manner.
// This is independent of PodGroupPolicy and can be used with LWS-level gang scheduling.
type CoordinationPolicy struct {
	// Scaling defines the coordination strategy for initial deployment and scale-up.
	// Takes effect when prefill/decode scales from 0 replicas, or when replicas increase.
	// +optional
	Scaling *ScalingCoordination `json:"scaling,omitempty"`

	// RollingUpdate defines the coordination strategy for rolling updates.
	// Takes effect when Pod template changes (e.g., image, config) trigger a rolling update.
	// +optional
	RollingUpdate *RollingUpdateCoordination `json:"rollingUpdate,omitempty"`
}

// ScalingCoordination defines the coordination strategy for scaling operations.
// Ensures prefill and decode are created proportionally to avoid resource waste.
type ScalingCoordination struct {
	// MaxSkew defines the maximum allowed difference in deployment progress between prefill and decode.
	// For example, with "10%", the deployment progress difference cannot exceed 10%.
	// Only percentage values are supported.
	// +optional
	// +kubebuilder:default="10%"
	// +kubebuilder:validation:Pattern=`^([0-9]|[1-9][0-9]|100)%$`
	MaxSkew string `json:"maxSkew,omitempty"`

	// Progression defines when to proceed to the next batch of deployment.
	// - OrderScheduled: Wait for all pods in current batch to be scheduled (have nodeName).
	// - OrderReady: Wait for all pods in current batch to be ready.
	// +optional
	// +kubebuilder:default="OrderScheduled"
	// +kubebuilder:validation:Enum=OrderScheduled;OrderReady
	Progression string `json:"progression,omitempty"`
}

// RollingUpdateCoordination defines the coordination strategy for rolling updates.
// Ensures prefill and decode are updated synchronously to avoid version inconsistency.
type RollingUpdateCoordination struct {
	// MaxSkew defines the maximum allowed difference in update progress between prefill and decode.
	// For example, with "5%", the update progress difference cannot exceed 5%.
	// Only percentage values are supported.
	// +optional
	// +kubebuilder:default="5%"
	// +kubebuilder:validation:Pattern=`^([0-9]|[1-9][0-9]|100)%$`
	MaxSkew string `json:"maxSkew,omitempty"`

	// MaxUnavailable defines the maximum number of unavailable replicas during the update (percentage).
	// If configured, overrides the MaxUnavailable in each role's RolloutStrategy.
	// Only percentage values are supported.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]|[1-9][0-9]|100)%$`
	MaxUnavailable string `json:"maxUnavailable,omitempty"`

	// Partition defines the partition point for rolling update (percentage).
	// If configured, overrides the Partition in each role's RolloutStrategy.
	// Only percentage values are supported.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]|[1-9][0-9]|100)%$`
	Partition string `json:"partition,omitempty"`
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

// ArksApplicationRouter defines the router role configuration.
// Field layout aligns with ArksDisaggregatedRouter for migration parity:
// the router image lives at the top-level Spec.RouterImage (not here).
// Presence of the parent *Router pointer expresses "enabled" (no Enabled flag here).
type ArksApplicationRouter struct {
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	CommandOverride []string `json:"commandOverride,omitempty"`
	// +optional
	Port int32 `json:"port,omitempty"`
	// +optional
	MetricPort int32 `json:"metricPort,omitempty"`
	// +optional
	RouterArgs []string `json:"routerArgs,omitempty"`
	// +optional
	InstanceSpec ArksInstanceSpec `json:"instanceSpec,omitempty"`
}

type ArksApplicationWorkload struct {
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	Size int `json:"size,omitempty"`
	// +optional
	LeaderCommandOverride []string `json:"leaderCommandOverride,omitempty"`
	// +optional
	WorkerCommandOverride []string `json:"workerCommandOverride,omitempty"`
	// +optional
	RuntimeCommonArgs []string `json:"runtimeCommonArgs,omitempty"`
	// +optional
	InstanceSpec ArksInstanceSpec `json:"instanceSpec,omitempty"`
}

// ArksApplicationWorkloadGroup is a named variant of a role workload. Groups
// within one role deploy heterogeneous engine configurations (for example
// different parallelism layouts) behind a single router. Each group is
// rendered as its own underlying role/workload.
type ArksApplicationWorkloadGroup struct {
	// Name identifies the group. It is appended to the role name (for
	// example "prefill-<name>") and exposed on pods via the
	// arks.ai/worker-group label, so it must be a DNS-1123 label. It is
	// kept short because it is embedded in workload resource names.
	// +kubebuilder:validation:MaxLength=24
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Model optionally overrides spec.model for this group, for example a
	// pre-sharded weight copy bound to this group's parallel layout. The
	// referenced ArksModel must be in the same namespace. When unset the
	// group uses spec.model.
	// +optional
	Model *corev1.LocalObjectReference `json:"model,omitempty"`

	ArksApplicationWorkload `json:",inline"`
}

// ArksApplicationSpec defines the desired state of ArksApplication.
//
// CRD-level validation rules enforce the allowed role combinations per mode:
//
//	mode=unified       → requires spec.unified
//	                     forbids  spec.prefill, spec.decode
//	                     spec.router is optional
//
//	mode=disaggregated → requires spec.decode, spec.router, and exactly one
//	                     of spec.prefill (homogeneous) or spec.prefillGroups
//	                     (heterogeneous engine configurations)
//	                     forbids  spec.unified
//
//	spec.coordinationPolicy is only valid when mode=disaggregated.
//
// Each rule reports an independent message so users get precise feedback
// from `kubectl apply` admission failures.
//
// +kubebuilder:validation:XValidation:rule="self.mode != 'unified' || has(self.unified)",message="spec.unified is required when spec.mode is 'unified'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'unified' || !has(self.prefill)",message="spec.prefill must not be set when spec.mode is 'unified'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'unified' || !has(self.prefillGroups)",message="spec.prefillGroups must not be set when spec.mode is 'unified'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'unified' || !has(self.decode)",message="spec.decode must not be set when spec.mode is 'unified'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'disaggregated' || (has(self.prefill) != has(self.prefillGroups))",message="exactly one of spec.prefill or spec.prefillGroups is required when spec.mode is 'disaggregated'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'disaggregated' || has(self.decode)",message="spec.decode is required when spec.mode is 'disaggregated'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'disaggregated' || has(self.router)",message="spec.router is required when spec.mode is 'disaggregated'"
// +kubebuilder:validation:XValidation:rule="self.mode != 'disaggregated' || !has(self.unified)",message="spec.unified must not be set when spec.mode is 'disaggregated'"
// +kubebuilder:validation:XValidation:rule="!has(self.coordinationPolicy) || self.mode == 'disaggregated'",message="spec.coordinationPolicy is only valid when spec.mode is 'disaggregated'"
// +kubebuilder:validation:XValidation:rule="!has(self.coordinationPolicy) || !has(self.prefillGroups)",message="spec.coordinationPolicy is not supported together with spec.prefillGroups"
type ArksApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Mode selects the deployment topology.
	// - unified (default): a single role (`unified`) runs the full inference pipeline,
	//   optionally fronted by a router.
	// - disaggregated: prefill + decode roles fronted by a router.
	// +optional
	// +kubebuilder:default=unified
	// +kubebuilder:validation:Enum=unified;disaggregated
	Mode ArksApplicationMode `json:"mode,omitempty"`

	// Runtime defines the inference runtime.
	// Now support: vllm, sglang. Default vLLM.
	// We will support Dynamo in future.
	// +optional
	Runtime string `json:"runtime"` // vLLM, SGLang, Default vLLM.

	// RuntimeImage defines the runtime container image URL.
	// Shared by inference/prefill/decode roles.
	// Specify this only when a specific version of the runtime image is required.
	// Customized runtime container images must be compatible with the Runtime.
	// Arks provides a default version of the runtime container image.
	// +optional
	RuntimeImage string `json:"runtimeImage,omitempty"`

	// RuntimeImagePullSecrets defines the runtime image pull secret.
	// You can specify the image pull secrets for the private image registry.
	// +optional
	RuntimeImagePullSecrets []corev1.LocalObjectReference `json:"runtimeImagePullSecrets,omitempty"`

	Model corev1.LocalObjectReference `json:"model"`

	// ServedModelName defines a custom model name.
	// +optional
	ServedModelName string `json:"servedModelName,omitempty"`

	// +optional
	// +kubebuilder:validation:Immutable
	PodGroupPolicy *ArksPodGroupPolicy `json:"podGroupPolicy,omitempty"`

	// CoordinationPolicy controls the coordinated scaling and rolling-update
	// strategy across roles (disaggregated mode only).
	// +optional
	CoordinationPolicy *CoordinationPolicy `json:"coordinationPolicy,omitempty"`

	// Unified defines the unified inference role (single role running the full
	// inference pipeline). Required in unified mode.
	// +optional
	Unified *ArksApplicationWorkload `json:"unified,omitempty"`

	// Prefill defines the prefill role. In disaggregated mode exactly one of
	// Prefill or PrefillGroups is required.
	// +optional
	Prefill *ArksApplicationWorkload `json:"prefill,omitempty"`

	// PrefillGroups defines heterogeneous prefill groups. Each group is
	// rendered as its own role (named "prefill-<group>") with an independent
	// engine configuration and, optionally, its own model. All groups serve
	// behind the same router. In disaggregated mode exactly one of Prefill
	// or PrefillGroups is required. Mutually exclusive with Prefill.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	PrefillGroups []ArksApplicationWorkloadGroup `json:"prefillGroups,omitempty"`

	// Decode defines the decode role. Required in disaggregated mode.
	// +optional
	Decode *ArksApplicationWorkload `json:"decode,omitempty"`

	// RouterImage defines the router container image. Used when Router is set.
	// +optional
	RouterImage string `json:"routerImage,omitempty"`

	// Router defines the router role.
	// In unified mode, presence enables router (nil = no router).
	// In disaggregated mode, this field is required.
	// +optional
	Router *ArksApplicationRouter `json:"router,omitempty"`
}

// ArksApplicationStatus defines the observed state of ArksApplication.
type ArksApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase string `json:"phase"`
	// +optional
	Mode ArksApplicationMode `json:"mode,omitempty"`
	// +optional
	TrafficTarget ArksApplicationTrafficTarget `json:"trafficTarget,omitempty"`

	Replicas        int32 `json:"replicas"`
	ReadyReplicas   int32 `json:"readyReplicas"`
	UpdatedReplicas int32 `json:"updatedReplicas"`

	// +optional
	Unified ArksRoleStatus `json:"unified,omitempty"`
	// +optional
	Router ArksRoleStatus `json:"router,omitempty"`
	// Prefill aggregates replica counts across all prefill workloads. With
	// spec.prefillGroups it is the sum over every group; per-group numbers
	// are reported in PrefillGroups.
	// +optional
	Prefill ArksRoleStatus `json:"prefill,omitempty"`
	// PrefillGroups reports per-group status when spec.prefillGroups is used.
	// +optional
	// +listType=map
	// +listMapKey=name
	PrefillGroups []ArksGroupStatus `json:"prefillGroups,omitempty"`
	// +optional
	Decode ArksRoleStatus `json:"decode,omitempty"`

	Conditions []ArksApplicationCondition `json:"conditions,omitempty"`
}

// ArksApplication is the Schema for the arksapplications API.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The current phase of the application"
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode",description="The inference topology mode"
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
