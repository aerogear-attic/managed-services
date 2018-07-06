package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SharedServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []SharedService `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SharedService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              SharedServiceSpec   `json:"spec"`
	Status            SharedServiceStatus `json:"status,omitempty"`
}

type SharedServiceSpec struct {
	//Image the docker image to run to provision the service
	Image string `json:"image"`
}
type SharedServiceStatus struct {
	// Fill me
	Ready bool `json:"ready"`
	Status string `json:"status"`  // provisioning, failed, provisioned
}


// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SharedServiceSliceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []SharedServiceSlice `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SharedServiceSlice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              SharedServiceSliceSpec   `json:"spec"`
	Status            SharedServiceSliceStatus `json:"status,omitempty"`
}

type SharedServiceSliceSpec struct {
	ServiceType string `json:"serviceType"`
	// Fill me
}
type SharedServiceSliceStatus struct {
	// Fill me
	Phase Phase `json:"phase"`
	Action string `json:"action"`
	CredentialREf string `json:"secretRef"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SharedServiceClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []SharedServiceClient `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SharedServiceClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              SharedServiceClientSpec   `json:"spec"`
	Status            SharedServiceClientStatus `json:"status,omitempty"`
	ServiceInstanceId string `json:"serviceInstanceId,omitempty"`
	Params            map[string]interface{} `json:"params,omitempty"`
}

type SharedServiceClientSpec struct {
	// Fill me
}
type SharedServiceClientStatus struct {
	// Fill me
}


type Phase string

var(
	AcceptedPhase Phase = "accepted"
	CompletePhase Phase = "complete"
)

