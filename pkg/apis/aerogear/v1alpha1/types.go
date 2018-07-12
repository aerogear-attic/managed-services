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
	Image                           string                 `json:"image"`
	ClusterServiceClassName         string                 `json:"cluster_service_class_name"`
	ClusterServiceClassExternalName string                 `json:"cluster_service_class_external_name"`
	Params                          map[string]interface{} `json:"params"`
}
type SharedServiceStatus struct {
	// Fill me
	Ready           bool   `json:"ready"`
	Status          string `json:"status"` // provisioning, failed, provisioned
	Phase           Phase  `json:"phase"`
	ServiceInstance string `json:"service_instance"`
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
	ServiceType string                 `json:"serviceType"`
	Params      map[string]interface{} `json:"params"`
	// Fill me
}
type SharedServiceSliceStatus struct {
	// Fill me
	Phase  Phase  `json:"phase"`
	Action string `json:"action"`
	// the ServiceInstanceID that represents the slice
	SliceServiceInstance string `json:"slice_service_instance"`
	// the ServiceInstanceID that represents the parent shared service
	SharedServiceInstance string `json:"shared_service_instance"`
	// Human readable message about what is happening
	Message string `json:"message"`
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
}

type SharedServiceClientSpec struct {
	// Fill me
}
type SharedServiceClientStatus struct {
	// Fill me
}

type Phase string

var (
	NoPhase           Phase = ""
	AcceptedPhase     Phase = "accepted"
	ProvisioningPhase Phase = "provisioning"
	CompletePhase     Phase = "complete"
	FailedPhase       Phase = "failed"
)
