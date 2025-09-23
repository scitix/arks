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

type ArksDisaggregatedRouter struct {
	// +optional
	Replicas int `json:"replicas"`
	// +optional
	CommandOverride []string `json:"commandOverride"`
	// +optional
	RouterArgs []string `json:"routerArgs"`
	// +optional
	InstanceSpec ArksInstanceSpec `json:"instanceSpec"`
}

type ArksDisaggregatedWorkload struct {
	// +optional
	Replicas int `json:"replicas"`
	// +optional
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

	// Router
	Router ArksDisaggregatedRouter `json:"router"`

	// Prefill
	Prefill ArksDisaggregatedWorkload `json:"prefill"`

	// Decode
	Decode ArksDisaggregatedWorkload `json:"decode"`
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
