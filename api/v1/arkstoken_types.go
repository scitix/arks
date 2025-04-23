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
type RateLimitType string

const (
	RateLimitTypeRPM RateLimitType = "rpm"
	RateLimitTypeRPD RateLimitType = "rpd"
	RateLimitTypeTPM RateLimitType = "tpm"
	RateLimitTypeTPD RateLimitType = "tpd"
	// TODO: support more types
)

type RateLimit struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=rpm;rpd;tpm;tpd
	Type RateLimitType `json:"type"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	Value int64 `json:"value"`
}

type ArksQos struct {
	ArksEndpoint corev1.LocalObjectReference `json:"arksEndpoint"`
	// +optional
	RateLimits []RateLimit `json:"rateLimits"`
	// +optional
	Quota corev1.LocalObjectReference `json:"quota"`
}

// ArksTokenSpec defines the desired state of ArksToken.
type ArksTokenSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Token string `json:"token"`

	Qos []ArksQos `json:"qos"`
}

// ArksTokenStatus defines the observed state of ArksToken.
type ArksTokenStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ArksToken is the Schema for the arkstokens API.
type ArksToken struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ArksTokenSpec   `json:"spec,omitempty"`
	Status ArksTokenStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ArksTokenList contains a list of ArksToken.
type ArksTokenList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ArksToken `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ArksToken{}, &ArksTokenList{})
}
