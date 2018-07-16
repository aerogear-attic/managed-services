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
	"crypto/md5"
	"encoding/hex"
	"net/http"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
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
func CreateController(brokerNS string, sharedServiceClient, sharedServiceSlice dynamic.ResourceInterface) Controller {
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
		ID:          createID(sharedService.Name),
		Description: sharedService.Name + " shared service",
		Metadata:map[string]string{"serviceName":sharedService.Name},
		Plans: []brokerapi.ServicePlan{{
			Name:        "shared",
			ID:          createID(sharedService.Name + "-slice-plan"),
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
				ServiceInstance:&brokerapi.ServiceInstanceSchema{
					Create:&brokerapi.InputParametersSchema{
						Parameters: map[string]interface{}{
							"$schema": "http://json-schema.org/draft-04/schema#",
							"type":    "object",
							"properties": map[string]interface{}{
								"CUSTOM_REALM_NAME": map[string]interface{}{
									"description": "the realm you want to create",
									"type":        "string",
									"required":true,
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

func createID(seed string) string {
	hasher := md5.New()
	hasher.Write([]byte(seed))
	return hex.EncodeToString(hasher.Sum(nil))
}

var services []*brokerapi.Service

var serviceMap map[string]*brokerapi.Service = map[string]*brokerapi.Service{}

// todo lock
func (c *userProvidedController)findServiceById(id string)*brokerapi.Service{
	c.rwMutex.Lock()
	defer c.rwMutex.Unlock()
	return serviceMap[id]
}

func (c *userProvidedController) Catalog() (*brokerapi.Catalog, error) {
	glog.Info("Catalog()")
	services = []*brokerapi.Service{}
	sharedServiceResourceList, err := c.sharedServiceClient.List(metav1.ListOptions{})
	glog.Info(sharedServiceResourceList)
	if err != nil {
		return nil, err
		}
	sharedServiceResourceList.(*unstructured.UnstructuredList).EachListItem(func(object runtime.Object) error {
		sharedService, err := sharedServiceFromRunTime(object)
		if err != nil {
			return err
		}

		if sharedService.Status.Ready == true {
			glog.Info("shared service is ready")
			bs := brokerServiceFromSharedService(sharedService)
			serviceMap[bs.ID] = bs
		}

		return nil
	})
	for _,v := range serviceMap{
		services = append(services, v)
	}
	return &brokerapi.Catalog{
		Services: services,
	}, nil
}

func (c *userProvidedController) CreateServiceInstance(
	id string,
	req *brokerapi.CreateServiceInstanceRequest,
) (*brokerapi.CreateServiceInstanceResponse, error) {
	glog.Info("CreateServiceInstance()", id , req.Parameters, req.ServiceID, req.AcceptsIncomplete, req.ContextProfile, req.AcceptsIncomplete)
	s := c.findServiceById(req.ServiceID)
	if s == nil{
		glog.Error("failed to find service instance")
		return  nil, errNoSuchInstance{}
	}
	glog.Info("got service ",s)
	serviceMeta := s.Metadata.(map[string]string)
	ss := &v1alpha1.SharedServiceSlice{
		ObjectMeta:metav1.ObjectMeta{
			Name:id,
			//move to config
			Namespace:c.brokerNS,
			Labels:map[string]string{
				"serviceInstanceID":id,
				"serviceID":req.ServiceID,
			},
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion:v1alpha1.SchemeGroupVersion.String(),
			Kind:"SharedServiceSlice",
		},
		Spec:v1alpha1.SharedServiceSliceSpec{
			ServiceType:serviceMeta["serviceName"],
			Params:req.Parameters,
		},
	}


	if _, err := c.sharedServiceSliceClient.Create(k8sutil.UnstructuredFromRuntimeObject(ss)); err != nil{
		glog.Error("error creating shared service slice ", err)
		if errors2.IsAlreadyExists(err){
			return &brokerapi.CreateServiceInstanceResponse{Code:http.StatusOK}, nil
		}
		return nil, err
	}


	glog.Infof("Created User Provided Service Instance:\n%v\n", c.instanceMap[id])
	return &brokerapi.CreateServiceInstanceResponse{Code:http.StatusAccepted, Operation:"provision"}, nil
}

func (c *userProvidedController) GetServiceInstanceLastOperation(
	instanceID,
	serviceID,
	planID,
	operation string,
) (*brokerapi.LastOperationResponse, error) {
	glog.Info("GetServiceInstanceLastOperation()", "operation " + operation, serviceID)
	if operation == "provision"{
		unstructSSL, err := c.sharedServiceSliceClient.Get(instanceID, metav1.GetOptions{})
		if err != nil{
			glog.Error("failed to get shared service slice ", instanceID, err)
			return nil, err
		}
		serviceSlice := &v1alpha1.SharedServiceSlice{}
		if err := k8sutil.UnstructuredIntoRuntimeObject(unstructSSL, serviceSlice); err != nil{
			glog.Error("failed runtime object ", err)
			return nil, err
		}
		glog.Info("status of slice ", serviceSlice.Status.Phase, serviceSlice.Status.Message)
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
	glog.Info("RemoveServiceInstance()", instanceID)
	if err := c.sharedServiceSliceClient.Delete(instanceID,metav1.NewDeleteOptions(10)); err != nil{
		glog.Error("failed to delete the slice ", err)
		if errors2.IsNotFound(err){
			return &brokerapi.DeleteServiceInstanceResponse{}, nil
		}
		return nil, err
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
