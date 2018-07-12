/*
Copyright 2016 The Kubernetes Authors.

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

package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/aerogear/managed-services/pkg/apis/aerogear/v1alpha1"
	"github.com/aerogear/managed-services/pkg/broker"
	brokerapi "github.com/aerogear/managed-services/pkg/broker"
	glog "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"github.com/operator-framework/operator-sdk/pkg/util/k8sutil"
)

// Controller defines the APIs that all controllers are expected to support. Implementations
// should be concurrency-safe
type Controller interface {
	Catalog() (*broker.Catalog, error)

	GetServiceInstanceLastOperation(instanceID, serviceID, planID, operation string) (*broker.LastOperationResponse, error)
	CreateServiceInstance(instanceID string, req *broker.CreateServiceInstanceRequest) (*broker.CreateServiceInstanceResponse, error)
	RemoveServiceInstance(instanceID, serviceID, planID string, acceptsIncomplete bool) (*broker.DeleteServiceInstanceResponse, error)

	Bind(instanceID, bindingID string, req *broker.BindingRequest) (*broker.CreateServiceBindingResponse, error)
	UnBind(instanceID, bindingID, serviceID, planID string) error
}

type errNoSuchInstance struct {
	instanceID string
}

func (e errNoSuchInstance) Error() string {
	return fmt.Sprintf("no such instance with ID %s", e.instanceID)
}

type userProvidedServiceInstance struct {
	Name       string
	Credential *brokerapi.Credential
}

type userProvidedController struct {
	rwMutex             sync.RWMutex
	instanceMap         map[string]*userProvidedServiceInstance
	sharedServiceClient dynamic.ResourceInterface
	sharedServiceSliceClient dynamic.ResourceInterface
	brokerNS  string
}

// CreateController creates an instance of a User Provided service broker controller.
func CreateController(brokerNS string, sharedServiceSlice, sharedServiceClient dynamic.ResourceInterface) Controller {
	var instanceMap = make(map[string]*userProvidedServiceInstance)
	return &userProvidedController{
		instanceMap:         instanceMap,
		sharedServiceClient: sharedServiceClient,
		brokerNS:brokerNS,
		sharedServiceSliceClient:sharedServiceSlice,
	}
}

func sharedServiceFromRunTime(object runtime.Object) (*v1alpha1.SharedService, error) {
	bytes, err := object.(*unstructured.Unstructured).MarshalJSON()
	if err != nil {
		return nil, err
	}
	s := &v1alpha1.SharedService{}
	if err := json.Unmarshal(bytes, s); err != nil {
		return nil, err
	}
	return s, nil
}

func brokerServiceFromSharedService(sharedService *v1alpha1.SharedService) *brokerapi.Service {
	// uuid generator
	// hard coded for keycloak
	return &brokerapi.Service{
		Name:        sharedService.Name + "-slice",
		ID:          sharedService.Name + "-slice",
		Description: sharedService.Name + " shared service",
		Metadata:map[string]string{"serviceName":sharedService.Name},
		Plans: []brokerapi.ServicePlan{{
			Name:        "shared",
			ID:          sharedService.Name + "-slice-plan",
			Description: "this plan gives you access to a shared managed keycloak instance",
			Free:        true,
			Schemas:&brokerapi.Schemas{
				ServiceBinding:&brokerapi.ServiceBindingSchema{
					Create:&brokerapi.RequestResponseSchema{
						InputParametersSchema:brokerapi.InputParametersSchema{
							Parameters: map[string]interface{}{
								"$schema": "http://json-schema.org/draft-04/schema#",
								"type":    "object",
								"properties": map[string]interface{}{
									"Username": map[string]interface{}{
										"description": "Username",
										"type":        "string",
									},
									"ClientType": map[string]interface{}{
										"description": "ClientType",
										"type":        "string",
									},
								},
							},
						},
					},
				},
			},
		},
		},
		Bindable:       true,
		PlanUpdateable: false,
	}
}

var services []*brokerapi.Service

// todo lock
func findServiceById(id string)*brokerapi.Service{
	for _, s := range services{
		if s.ID == id{
			return s
		}
	}
	return nil
}

func (c *userProvidedController) Catalog() (*brokerapi.Catalog, error) {
	glog.Info("Catalog()")

	sharedServiceResourceList, err := c.sharedServiceClient.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	services := []*brokerapi.Service{}
	sharedServiceResourceList.(*unstructured.UnstructuredList).EachListItem(func(object runtime.Object) error {
		sharedService, err := sharedServiceFromRunTime(object)
		if err != nil {
			return err
		}

		if sharedService.Status.Ready == true {
			services = append(services, brokerServiceFromSharedService(sharedService))
		}

		return nil
	})

	return &brokerapi.Catalog{
		Services: services,
	}, nil
}

func (c *userProvidedController) CreateServiceInstance(
	id string,
	req *brokerapi.CreateServiceInstanceRequest,
) (*brokerapi.CreateServiceInstanceResponse, error) {
	glog.Info("CreateServiceInstance()")
	s := findServiceById(id)
	serviceMeta := s.Metadata.(map[string]string)
	ss := &v1alpha1.SharedServiceSlice{
		ObjectMeta:metav1.ObjectMeta{
			Name:id,
			//move to config
			Namespace:c.brokerNS,
			Labels:map[string]string{
				"serviceInstanceID":id,
			},
		},
		Spec:v1alpha1.SharedServiceSliceSpec{
			ServiceType:serviceMeta["serviceName"],
			Params:req.Parameters,
		},
	}

	if _, err := c.sharedServiceSliceClient.Create(k8sutil.UnstructuredFromRuntimeObject(ss)); err != nil{
		glog.Error("error creating shared service slice ", err)

	}


	glog.Infof("Created User Provided Service Instance:\n%v\n", c.instanceMap[id])
	return &brokerapi.CreateServiceInstanceResponse{}, nil
}

func (c *userProvidedController) GetServiceInstanceLastOperation(
	instanceID,
	serviceID,
	planID,
	operation string,
) (*brokerapi.LastOperationResponse, error) {
	glog.Info("GetServiceInstanceLastOperation()")
	if operation == "provision"{
		unstructSSL, err := c.sharedServiceSliceClient.Get(instanceID, metav1.GetOptions{})
		if err != nil{
			return nil, err
		}
		serviceSlice := &v1alpha1.SharedServiceSlice{}
		if err := k8sutil.UnstructuredIntoRuntimeObject(unstructSSL, serviceSlice); err != nil{
			return nil, err
		}
		if serviceSlice.Status.Phase == "provisioning" {
			return &brokerapi.LastOperationResponse{Description: serviceSlice.Status.Message, State: brokerapi.StateInProgress}, nil
		}
		if serviceSlice.Status.Phase == "failed"{
			return &brokerapi.LastOperationResponse{Description: serviceSlice.Status.Message, State: brokerapi.StateFailed}, nil
		}
		if serviceSlice.Status.Phase == "complete"{
			return &brokerapi.LastOperationResponse{Description: serviceSlice.Status.Message, State: brokerapi.StateSucceeded}, nil
		}
	}
	return nil, errors.New("Unimplemented")
}

func (c *userProvidedController) RemoveServiceInstance(
	instanceID,
	serviceID,
	planID string,
	acceptsIncomplete bool,
) (*brokerapi.DeleteServiceInstanceResponse, error) {
	glog.Info("RemoveServiceInstance()")
	c.rwMutex.Lock()
	defer c.rwMutex.Unlock()
	_, ok := c.instanceMap[instanceID]
	if ok {
		delete(c.instanceMap, instanceID)
		return &brokerapi.DeleteServiceInstanceResponse{}, nil
	}

	return &brokerapi.DeleteServiceInstanceResponse{}, nil
}

func (c *userProvidedController) Bind(
	instanceID,
	bindingID string,
	req *brokerapi.BindingRequest,
) (*brokerapi.CreateServiceBindingResponse, error) {
	glog.Info("Bind()")
	c.rwMutex.RLock()
	defer c.rwMutex.RUnlock()
	instance, ok := c.instanceMap[instanceID]
	if !ok {
		return nil, errNoSuchInstance{instanceID: instanceID}
	}
	cred := instance.Credential
	return &brokerapi.CreateServiceBindingResponse{Credentials: *cred}, nil
}

func (c *userProvidedController) UnBind(instanceID, bindingID, serviceID, planID string) error {
	glog.Info("UnBind()")
	// Since we don't persist the binding, there's nothing to do here.
	return nil
}
