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

type ArksModelPhase string
type ArksModelConditionType string

const (
	ArksModelPhasePending         ArksModelPhase = "Pending"
	ArksModelPhaseStorageCreating ArksModelPhase = "StorageCreating"
	ArksModelPhaseModelLoading    ArksModelPhase = "ModelLoading"
	ArksModelPhaseReady           ArksModelPhase = "Ready"
	ArksModelPhaseFailed          ArksModelPhase = "Failed"

	// ArksModelReady is the condition that indicates if the model volume is ready to be consumed.
	ArksModelReady ArksModelConditionType = "Ready"
	// ArksModelStorageCreated is the condition that indicates if the underlying PVC is created or not.
	ArksModelStorageCreated ArksModelConditionType = "StorageCreated"
	// ArksModelStorageBound is the condition that indicates if the underlying PVC is bound or not.
	ArksModelStorageBound ArksModelConditionType = "StorageCreated"
	// ArksModelModelLoaded is the condition that indicates if the clone container is running.
	ArksModelModelLoaded ArksModelConditionType = "ModelLoaded"
)

// ArksModelSourceHuggingFace provides the parameters to create a Model Volume from an hugging-face registry.
type ArksModelSourceHuggingFace struct {
	// +optional
	TokenSecretRef *corev1.LocalObjectReference `json:"tokenSecretRef,omitempty"`
}

// DataVolumeSource represents the source for our Model Volume.
type ArksModelSource struct {
	// Hugingface defines the parameters to pull model from hugging face registry.
	// +optional
	Huggingface *ArksModelSourceHuggingFace `json:"huggingface,omitempty"`
}

type ArksModelStoragePVC struct {
	// +optional
	Name string                           `json:"name,omitempty"`
	Spec corev1.PersistentVolumeClaimSpec `json:"spec"`
}

type ArksModelStorage struct {
	// PVC defines the storage type to store the specified model.
	// Storage directory path: Qwen/QwQ-32B
	PVC *ArksModelStoragePVC `json:"pvc,omitempty"`
	// +optional
	SubPath string `json:"subPath,omitempty"`
}

// ArksModelCondition represents the state of a model volume condition.
type ArksModelCondition struct {
	Type               ArksModelConditionType `json:"type" description:"type of condition ie. Ready|Bound|Running."`
	Status             corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty" description:"reason for the condition's last transition"`
	Message            string                 `json:"message,omitempty" description:"human-readable message indicating details about last transition"`
}

// ArksModelSpec defines the desired state of ArksModel.
type ArksModelSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Model string `json:"model"`

	// Source is the src of the data for the requested ArksModel
	// +optional
	Source *ArksModelSource `json:"source,omitempty"`

	// Storage defines the Storage type specification
	Storage *ArksModelStorage `json:"storage,omitempty"`

	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets"`

	// +optional
	InstanceSpec ArksInstanceSpec `json:"instanceSpec,omitempty"`
}

// ArksModelStatus defines the observed state of ArksModel.
type ArksModelStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase string `json:"phase"`

	Conditions []ArksModelCondition `json:"conditions,omitempty"`
}

// ArksModel is the Schema for the arksmodels API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model",description="The model being used"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="The current phase of the model"
// +kubebuilder:resource:shortName=am

type ArksModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ArksModelSpec   `json:"spec,omitempty"`
	Status ArksModelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ArksModelList contains a list of ArksModel.
type ArksModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ArksModel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ArksModel{}, &ArksModelList{})
}
