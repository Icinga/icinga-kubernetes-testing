package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestTest defines a test to be run
type TestTest struct {
	TestKind     string `json:"testKind"`
	GoodReplicas *int32 `json:"goodReplicas"`
	BadReplicas  *int32 `json:"badReplicas"`
	TestConfig   string `json:"testConfig"`
}

// TestSpec defines the desired state of Test
type TestSpec struct {
	DeploymentName string     `json:"deploymentName"`
	Tests          []TestTest `json:"tests"`
}

type TestStatus struct {
	AvailableReplicas int32 `json:"availableReplicas"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status

// Test is the Schema for the tests API
type Test struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TestSpec   `json:"spec"`
	Status            TestStatus `json:"status"`
}

// TestList contains a list of Test
type TestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Test `json:"items"`
}

func (in *TestList) DeepCopyObject() runtime.Object {
	//TODO implement me
	panic("implement me")
}
