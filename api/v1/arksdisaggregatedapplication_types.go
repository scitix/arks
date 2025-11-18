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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PodGroupPolicy represents a PodGroup configuration for gang-scheduling.
type PodGroupPolicy struct {
	// Configuration for gang-scheduling using various plugins.
	PodGroupPolicySource `json:",inline"`
}

// PodGroupPolicySource represents supported plugins for gang-scheduling.
// Only one of its members may be specified.
type PodGroupPolicySource struct {
	// KubeScheduling plugin from the Kubernetes scheduler-plugins for gang-scheduling.
	KubeScheduling *KubeSchedulingPodGroupPolicySource `json:"kubeScheduling,omitempty"`

	VolcanoScheduling *VolcanoSchedulingPodGroupPolicySource `json:"volcanoScheduling,omitempty"`
}

// KubeSchedulingPodGroupPolicySource represents configuration for  Kubernetes scheduling plugin.
// The number of min members in the PodGroupSpec is always equal to the number of rbg pods.
type KubeSchedulingPodGroupPolicySource struct {
	// Time threshold to schedule PodGroup for gang-scheduling.
	// If the scheduling timeout is equal to 0, the default value is used.
	// Defaults to 60 seconds.
	// +kubebuilder:default=60
	ScheduleTimeoutSeconds *int32 `json:"scheduleTimeoutSeconds,omitempty"`
}

// VolcanoSchedulingPodGroupPolicySource represents configuration for volcano podgroup scheduling plugin
type VolcanoSchedulingPodGroupPolicySource struct {
	// If specified, indicates the PodGroup's priority. "system-node-critical" and
	// "system-cluster-critical" are two special keywords which indicate the
	// highest priorities with the former being the highest priority. Any other
	// name must be defined by creating a PriorityClass object with that name.
	// If not specified, the PodGroup priority will be default or zero if there is no
	// default.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// Queue defines the queue to allocate resource for PodGroup; if queue does not exist,
	// the PodGroup will not be scheduled. Defaults to `default` Queue with the lowest weight.
	// +optional
	Queue string `json:"queue,omitempty"`
}

type ArksDisaggregatedRouter struct {
	// +optional
	Replicas *int32 `json:"replicas"`
	// +optional
	CommandOverride []string `json:"commandOverride"`
	// Port
	// +optional
	Port int32 `json:"int32"`
	// MetricPort
	// +optional
	MetricPort int32 `json:"metricPort"`
	// +optional
	RouterArgs []string `json:"routerArgs"`
	// +optional
	InstanceSpec ArksInstanceSpec `json:"instanceSpec"`
}

type ArksDisaggregatedWorkload struct {
	// +optional
	Replicas *int32 `json:"replicas"`
	// +optional
	// +kubebuilder:validation:Immutable
	Size int `json:"size"`
	// +optional
	LeaderCommandOverride []string `json:"leaderCommandOverride"`
	// +optional
	WorkerCommandOverride []string `json:"workerCommandOverride"`
	// +optional
	RuntimeCommonArgs []string `json:"runtimeCommonArgs"`
	// InstanceSpec
	// +optional
	InstanceSpec ArksInstanceSpec `json:"instanceSpec"`
}

// ArksDisaggregatedApplicationSpec defines the desired state of ArksDisaggregatedApplication.
type ArksDisaggregatedApplicationSpec struct {
	// Runtime defines the inference runtime.
	// Now support: vllm, sglang. Default vLLM.
	// We will support Dynamo in future.
	// +optional
	// +kubebuilder:validation:Immutable
	Runtime string `json:"runtime"` // vLLM, SGLang, Default vLLM.

	// RouterImage defines the router container image URL.
	// +optional
	// +kubebuilder:validation:Immutable
	RouterImage string `json:"routerImage"`

	// RuntimeImage defines the runtime container image URL.
	// Specify this only when a specific version of the runtime image is required.
	// Customized runtime container images must be compatible with the Runtime.
	// Arks provides a default version of the runtime container image.
	// +optional
	// +kubebuilder:validation:Immutable
	RuntimeImage string `json:"runtimeImage"` // The image of vLLM, SGLang or Dynamo.

	// RuntimeImagePullSecrets defines the runtime image pull secret.
	// You can specify the image pull secrets for the private image registry.
	// +optional
	RuntimeImagePullSecrets []corev1.LocalObjectReference `json:"runtimeImagePullSecrets"`

	Model corev1.LocalObjectReference `json:"model"`

	// ServedModelName defines a custom model name.
	// +optional
	ServedModelName string `json:"servedModelName"`

	// Router
	Router ArksDisaggregatedRouter `json:"router"`

	// Prefill
	Prefill ArksDisaggregatedWorkload `json:"prefill"`

	// Decode
	Decode ArksDisaggregatedWorkload `json:"decode"`

	// +optional
	// +kubebuilder:validation:Immutable
	PodGroupPolicy *PodGroupPolicy `json:"podGroupPolicy"`
}

type ArksComponentStatus struct {
	Replicas        int32 `json:"replicas"`
	ReadyReplicas   int32 `json:"readyReplicas"`
	UpdatedReplicas int32 `json:"updatedReplicas"`
}

// ArksDisaggregatedApplicationStatus defines the observed state of ArksDisaggregatedApplication.
type ArksDisaggregatedApplicationStatus struct {
	// +optional
	Phase string `json:"phase"`
	// +optional
	Router ArksComponentStatus `json:"router"`
	// +optional
	Prefill ArksComponentStatus `json:"prefill"`
	// +optional
	Decode ArksComponentStatus `json:"decode"`
	// +optional
	Conditions []ArksApplicationCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The current phase of the application"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Router Ready",type="string",JSONPath=".status.router.readyReplicas",description="Ready router replicas"
// +kubebuilder:printcolumn:name="Prefill Ready",type="string",JSONPath=".status.prefill.readyReplicas",description="Ready prefill replicas"
// +kubebuilder:printcolumn:name="Decode Ready",type="string",JSONPath=".status.decode.readyReplicas",description="Ready decode replicas"
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model.name",description="The model being used",priority=1
// +kubebuilder:resource:shortName=arkdapp

// ArksDisaggregatedApplication is the Schema for the arksdisaggregatedapplications API.
type ArksDisaggregatedApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ArksDisaggregatedApplicationSpec   `json:"spec,omitempty"`
	Status ArksDisaggregatedApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ArksDisaggregatedApplicationList contains a list of ArksDisaggregatedApplication.
type ArksDisaggregatedApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ArksDisaggregatedApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ArksDisaggregatedApplication{}, &ArksDisaggregatedApplicationList{})
}
