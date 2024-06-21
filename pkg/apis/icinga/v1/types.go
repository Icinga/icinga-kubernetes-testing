package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TestType is the Schema for the tests API
type TestType struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TestSpec `json:"spec"`
}

// TestSpec defines the desired state of Test
type TestSpec struct {
	CronSpec string `json:"cronSpec"`
	Image    string `json:"image"`
	Replicas int    `json:"replicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TestTypeList contains a list of Test
type TestTypeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TestType `json:"items"`
}
