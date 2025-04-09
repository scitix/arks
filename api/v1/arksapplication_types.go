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

	ArksDriverDefault ArksDriver = "LWS" // The default driver is LWS
	ArksDriverLWS     ArksDriver = "LWS"
	// ArksDriverDynamo  ArksDriver = "Dynamo" // Support in future

	ArksRuntimeDefault ArksRuntime = "vllm" // The default driver is vLLM
	ArksRuntimeVLLM    ArksRuntime = "vllm"
	ArksRuntimeSGLang  ArksRuntime = "sglang"
)

const (
	ArksControllerKeyApplication  = "arks.scitix.ai/application"
	ArksControllerKeyModel        = "arks.scitix.ai/model"
	ArksControllerKeyToken        = "arks.scitix.ai/token"
	ArksControllerKeyQuota        = "arks.scitix.ai/quota"
	ArksControllerKeyWorkLoadRole = "arks.scitix.ai/work-load-role"

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
	// Replicas defines the instance number in a deploy set.
	// +kubebuilder:validation:Optional
	// +immutable
	Replicas int `json:"replicas"`

	// Resources define the leader/worker container resources.
	Resources corev1.ResourceRequirements `json:"resources"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// VolumeMounts define the mount point for leader/worker pod.
	// NOTE: the mount point can not be '/models', it is reserved for ArksModel.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// Volumes define the extra volumes for leader/worker pod, volume name
	// can not be 'models', it is reserved for ArksModel.
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the leader/worker pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	// +mapType=atomic
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// If specified, the pod's scheduling constraints
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// If specified, the pod will be dispatched by specified scheduler.
	// If not specified, the pod will be dispatched by default scheduler.
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`

	// If specified, the pod's tolerations.
	// +optional
	// +listType=atomic
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run leader/worker pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// ArksApplicationSpec defines the desired state of ArksApplication.
type ArksApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Replicas int `json:"replicas"`

	// +optional
	Driver string `json:"driver"` // LWS, kuberay, Dynamo, Default LWS.
	// +optional
	Runtime string `json:"runtime"` // vLLM, SGLang, Default vLLM.

	Model corev1.LocalObjectReference `json:"model"`

	ServedModelName string `json:"servedModelName"`

	// +optional
	TensorParallelSize int `json:"tensorParallelSize"`
	// +optional
	ExtraOptions []string `json:"extraOptions"`

	InstanceSpec ArksInstanceSpec `json:"instanceSpec"`
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ArksApplication is the Schema for the arksapplications API.
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
