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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	brokerapi "github.com/aerogear/managed-services/pkg/broker"
	"github.com/aerogear/managed-services/pkg/apis/aerogear/v1alpha1"
	"github.com/aerogear/managed-services/pkg/broker"

	glog "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
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
	rwMutex	sync.RWMutex
	instanceMap map[string]*userProvidedServiceInstance
	sharedServiceClient dynamic.ResourceInterface
}

// CreateController creates an instance of a User Provided service broker controller.
func CreateController(sharedServiceClient dynamic.ResourceInterface) Controller {
	var instanceMap = make(map[string]*userProvidedServiceInstance)
	return &userProvidedController{
		instanceMap:         instanceMap,
		sharedServiceClient: sharedServiceClient,
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

func createID(seed string) string {
	hasher := md5.New()
	hasher.Write([]byte(seed))
	return hex.EncodeToString(hasher.Sum(nil))
}

func brokerServiceFromSharedService(sharedService *v1alpha1.SharedService) *brokerapi.Service {
	serviceName := sharedService.Spec.ClusterServiceClassExternalName + "-slice"
	planName := sharedService.Spec.ClusterServiceClassExternalName + "-shared"
	return &brokerapi.Service{
		Name:        serviceName,
		ID:          createID(serviceName),
		Description: "A user provided service",
		Plans: []brokerapi.ServicePlan{{
			Name:        planName,
			ID:          createID(planName),
			Description: "Sample plan description",
			Free:        true,
		},
		},
		Bindable:       true,
		PlanUpdateable: false,
	}
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
	credString, ok := req.Parameters["credentials"]
	c.rwMutex.Lock()
	defer c.rwMutex.Unlock()
	if ok {
		jsonCred, err := json.Marshal(credString)
		if err != nil {
			glog.Errorf("Failed to marshal credentials: %v", err)
			return nil, err
		}
		var cred brokerapi.Credential
		err = json.Unmarshal(jsonCred, &cred)
		if err != nil {
			glog.Errorf("Failed to unmarshal credentials: %v", err)
			return nil, err
		}

		c.instanceMap[id] = &userProvidedServiceInstance{
			Name:       id,
			Credential: &cred,
		}
	} else {
		c.instanceMap[id] = &userProvidedServiceInstance{
			Name: id,
			Credential: &brokerapi.Credential{
				"special-key-1": "special-value-1",
				"special-key-2": "special-value-2",
			},
		}
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
