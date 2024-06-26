/*
Copyright 2024.

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

// EipBindingSpec defines the desired state of EipBinding
type EipBindingSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// The kubevirt vmi name
	Vmi string `json:"vmi"`

	// The elastic ipv4 address
	EipAddr string `json:"eip"`

	// Last vmi pod placed on
	// +optional
	LastHyper string `json:"lastHyper,omitempty"`

	// Last vmi pod ipv4 address
	// +optional
	LastIP string `json:"LastIP,omitempty"`

	// The number of jobs to retain
	// +optional
	//+kubebuilder:default:=5
	//+kubebuilder:validation:Minimum=0
	JobHistory *int32 `json:"jobHistory,omitempty"`
}

// EipBindingStatus defines the observed state of EipBinding
type EipBindingStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// A list of pointers to currently running jobs.
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// Information when was the last time the job was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// EipBinding is the Schema for the eipbindings API
type EipBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EipBindingSpec   `json:"spec,omitempty"`
	Status EipBindingStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// EipBindingList contains a list of EipBinding
type EipBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EipBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EipBinding{}, &EipBindingList{})
}
