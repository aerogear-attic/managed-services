package managed

import (
	"context"

	"github.com/aerogear/managed-services/pkg/apis/aerogear/v1alpha1"

	"fmt"

	sc "github.com/kubernetes-incubator/service-catalog/pkg/client/clientset_generated/clientset"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"bytes"
	"encoding/json"
	"os"

	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1beta1"
	"github.com/lestrrat/go-jsschema"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	"github.com/golang/glog"
)

func NewHandler(k8sClient kubernetes.Interface, sharedServiceClient dynamic.ResourceInterface, operatorNS string, svcCatalog sc.Interface) sdk.Handler {
	return &Handler{
		k8client:             k8sClient,
		operatorNS:           operatorNS,
		sharedServiceClient:  sharedServiceClient,
		serviceCatalogClient: svcCatalog,
	}
}

type Handler struct {
	// Fill me
	k8client             kubernetes.Interface
	operatorNS           string
	sharedServiceClient  dynamic.ResourceInterface
	serviceCatalogClient sc.Interface
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.SharedService:
		if event.Deleted {
			return h.handleSharedServiceDelete(o)
		}
		return h.handleSharedServiceCreateUpdate(o)
	case *v1alpha1.SharedServiceSlice:
		if event.Deleted {
			return h.handleSharedServiceSliceDelete(o)
		}
		return h.handleSharedServiceSliceCreateUpdate(o)

	case *v1alpha1.SharedServiceClient:
		if event.Deleted {
			return h.handleSharedServiceClientDelete(o)
		}
		return h.handleSharedServiceClientCreateUpdate(o)
	}
	return nil
}

func buildServiceInstance(namespace string, serviceName string, parameters []byte, clusterServiceClass v1beta1.ClusterServiceClass) v1beta1.ServiceInstance {
	return v1beta1.ServiceInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "servicecatalog.k8s.io/v1beta1",
			Kind:       "ServiceInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: serviceName + "-",
		},
		Spec: v1beta1.ServiceInstanceSpec{
			PlanReference: v1beta1.PlanReference{
				ClusterServiceClassExternalName: clusterServiceClass.Spec.ExternalName,
			},
			ClusterServiceClassRef: &v1beta1.ClusterObjectReference{
				Name: clusterServiceClass.Name,
			},
			ClusterServicePlanRef: &v1beta1.ClusterObjectReference{
				Name: "default",
			},
			Parameters: &runtime.RawExtension{Raw: parameters},
		},
	}
}

func (h *Handler) getServiceClass(service *v1alpha1.SharedService) (*v1beta1.ClusterServiceClass, error) {
	svcCopy := service.DeepCopy()
	if svcCopy.Spec.ClusterServiceClassName != "" {
		return h.serviceCatalogClient.Servicecatalog().ClusterServiceClasses().Get(svcCopy.Spec.ClusterServiceClassName, metav1.GetOptions{})
	}

	scs, err := h.serviceCatalogClient.Servicecatalog().ClusterServiceClasses().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, sc := range scs.Items {
		if sc.Spec.CommonServiceClassSpec.ExternalName == svcCopy.Spec.ClusterServiceClassExternalName {
			svcCopy.Spec.ClusterServiceClassName = sc.Name
			return &sc, nil
		}
	}

	return nil, errors.New("Could not find a matching Cluster Service Class for:" + svcCopy.Spec.ClusterServiceClassExternalName)
}

func (h *Handler) handleSharedServiceCreateUpdate(service *v1alpha1.SharedService) error {
	svcCopy := service.DeepCopy()
	//If status is empty, then this is a new CRD, start provision request
	switch svcCopy.Status.Phase {
	case v1alpha1.NoPhase:
		svcCopy.Status.Phase = v1alpha1.AcceptedPhase
		return sdk.Update(svcCopy)
	case v1alpha1.AcceptedPhase:
		sc, err := h.getServiceClass(svcCopy)
		if err != nil {
			svcCopy.Status.Phase = v1alpha1.FailedPhase
			sdk.Update(svcCopy)
			return err
		}

		parameters, err := json.Marshal(svcCopy.Spec.Params)
		if err != nil {
			svcCopy.Status.Phase = v1alpha1.FailedPhase
			sdk.Update(svcCopy)
			return err
		}
		si := buildServiceInstance(svcCopy.Namespace, svcCopy.Spec.ClusterServiceClassExternalName, parameters, *sc)
		siHandle, err := h.serviceCatalogClient.Servicecatalog().ServiceInstances(svcCopy.Namespace).Create(&si)
		if err != nil {
			svcCopy.Status.Phase = v1alpha1.FailedPhase
			sdk.Update(svcCopy)
			return err
		}
		if svcCopy.ObjectMeta.Labels == nil {
			svcCopy.ObjectMeta.Labels = map[string]string{}
		}
		svcCopy.Status.ServiceInstance = string(siHandle.ObjectMeta.Name)
		svcCopy.Status.Phase = v1alpha1.ProvisioningPhase
		return sdk.Update(svcCopy)

	case v1alpha1.ProvisioningPhase:
		si, err := h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(svcCopy.Namespace).Get(svcCopy.Status.ServiceInstance, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, cnd := range si.Status.Conditions {
			if cnd.Type == "Ready" && cnd.Status == "True" {
				svcCopy.Status.Phase = v1alpha1.CompletePhase
				svcCopy.Status.Ready = true
				sdk.Update(svcCopy)
			}
		}
	}

	return nil
}

func (h *Handler) handleSharedServiceDelete(service *v1alpha1.SharedService) error {
	return h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(service.Namespace).Delete(service.Status.ServiceInstance, &metav1.DeleteOptions{})
}

func (h *Handler) allocateSharedServiceInstanceWithCapacity(serviceType string) (*v1beta1.ServiceInstance, error) {
	// look up sharedserviceinstanc of the given type and increment the capcity
	// look up the service instance referenced in the sharedserviceinstance and return it
	//work around for testing
	sharedServiceInstsnce := os.Getenv("SSI")
	return h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(h.operatorNS).Get(sharedServiceInstsnce, metav1.GetOptions{})
}

func (h *Handler) getParamValue(slice *v1alpha1.SharedServiceSlice, sharedServiceInstanceID string, key string, paramSchema *schema.Schema) (interface{}, error) {
	// todo don't have  sharedserviceconfig to work with yet but we will do the following
	// look up the param in the shared service config
	// look up the param in the shared service instance secret (named after the service)
	// if not there return nil
	// it will then be pulled from the shared service slice params
	//hard coded for keycloak right now
	// get the secret we create from the apb
	//TODO this is currently broken but as we cannot deploy more than one keycloak until we change the apb it works !!

	ls := metav1.LabelSelector{MatchLabels: map[string]string{"serviceName": slice.Spec.ServiceType, "serviceInstanceID": sharedServiceInstanceID}}
	fmt.Println("creating label selector ", ls.String())
	superSecretCredList, err := h.k8client.CoreV1().Secrets(h.operatorNS).List(metav1.ListOptions{LabelSelector: "serviceInstanceID=" + sharedServiceInstanceID + ",serviceName=" + slice.Spec.ServiceType})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, errors.Wrap(err, "failed to find credentials secret for service ")
	}
	if len(superSecretCredList.Items) != 1 {
		fmt.Println("found none or more than one secret this is bad")
		return nil, errors.New("found more than one credential secret for service instance " + slice.Status.SharedServiceInstance)
	}

	if v, ok := slice.Spec.Params[key]; ok {
		return v, nil
	}
	superSecretCreds := superSecretCredList.Items[0]

	if v, ok := superSecretCreds.Data[key]; ok {
		return string(v), nil
	}

	return nil, nil
	//take a service look up the SharedServiceConfig for that service pull out the default params as a map of key values

}

//TODO not happy with the signature here we are returning the parent and slice ids as strings which is not clear
func (h *Handler) provisionSlice(serviceSlice *v1alpha1.SharedServiceSlice, si *v1beta1.ServiceInstance, serviceType, plan string) (string, error) {
	fmt.Println("provisioning slice")
	// find shared service with capacity of the given type

	availablePlans, err := h.serviceCatalogClient.ServicecatalogV1beta1().ClusterServicePlans().List(metav1.ListOptions{FieldSelector: "spec.externalName=shared"})
	if err != nil {
		return "", errors.Wrap(err, "failed to get service plans")
	}
	if len(availablePlans.Items) != 1 {
		//this is bad
		return "", errors.New(fmt.Sprintf("expected a single plan with the name shared but found %v", len(availablePlans.Items)))
	}
	ap := availablePlans.Items[0]
	fmt.Println("schema ", string(ap.Spec.ServiceInstanceCreateParameterSchema.Raw))
	paramSchema, err := schema.Read(bytes.NewBuffer(ap.Spec.ServiceInstanceCreateParameterSchema.Raw))
	if err != nil {
		logrus.Error("failed to read schema", err)
	}

	params := map[string]interface{}{}

	if paramSchema != nil {
		for name, p := range paramSchema.Properties {
			fmt.Println("property ", p.Type, name)
			val, err := h.getParamValue(serviceSlice, si.Spec.ExternalID, name, p)
			if err != nil {
				// have to bail out no way forward
				return "", err
			}
			params[name] = val
		}
	}

	pData, err := json.Marshal(params)
	if err != nil {
		return "", errors.Wrap(err, "failed to encode params")
	}

	fmt.Println("params for slice provision ", string(pData))

	provisionInstance := &v1beta1.ServiceInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "servicecatalog.k8s.io/v1beta1",
			Kind:       "ServiceInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    h.operatorNS,
			GenerateName: serviceType + "-",
		},
		Spec: v1beta1.ServiceInstanceSpec{
			PlanReference: v1beta1.PlanReference{
				ClusterServiceClassExternalName: si.Spec.ClusterServiceClassExternalName,
				ClusterServicePlanExternalName:  plan,
			},
			//TODO should prob come from secret
			Parameters: &runtime.RawExtension{
				Raw: pData,
			},
		},
	}

	fmt.Println("would have provisioned slice ", provisionInstance, "shared service ", si)
	csi, err := h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(h.operatorNS).Create(provisionInstance)
	if err != nil {
		return "", err
	}
	return csi.Name, nil
}

type serviceProvisionFailedErr struct{
	Message string
}
func (spe *serviceProvisionFailedErr)Error()string{
	return spe.Message
}

func isProvisionErr(err error)bool{
	_,ok := err.(*serviceProvisionFailedErr)
	return ok
}

func (h *Handler) checkServiceInstanceReady(sid string) (bool, error) {
	fmt.Println("checking service instance ready ", sid)
	if sid == "" {
		return false, nil
	}
	si, err := h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(h.operatorNS).Get(sid, metav1.GetOptions{})
	if err != nil {

	}
	if si == nil {
		return false, nil
	}
	for _, c := range si.Status.Conditions {
		fmt.Println(c.Type, c.Status)
		if c.Type == v1beta1.ServiceInstanceConditionReady {
			return c.Status == v1beta1.ConditionTrue, nil
		}
		if c.Type == v1beta1.ServiceInstanceConditionFailed{
			glog.Info("instance in condition failed")
			return false, &serviceProvisionFailedErr{Message:c.Message}
		}
	}
	return false, nil
}

func (h *Handler) handleSharedServiceSliceCreateUpdate(service *v1alpha1.SharedServiceSlice) error {
	fmt.Println("called handleSharedServiceSliceCreateUpdate", service.Status.Phase, service.Status.Action)
	ssCopy := service.DeepCopy()
	if ssCopy.Status.Phase != v1alpha1.AcceptedPhase && (ssCopy.Status.Phase != v1alpha1.CompletePhase || ssCopy.Status.Phase != v1alpha1.FailedPhase ) {
		ssCopy.Status.Phase = v1alpha1.AcceptedPhase
		return sdk.Update(ssCopy)
	}
	if ssCopy.Status.Action == "provisioned" {
		// look up the secret and save to the shared service slice and set the status to complete
		ssCopy.Status.Phase = v1alpha1.CompletePhase
		return sdk.Update(ssCopy)
	}
	if ssCopy.Status.Action == "provisioning" {
		fmt.Print("provisioning")
		ready, err := h.checkServiceInstanceReady(ssCopy.Status.SliceServiceInstance)
		if err != nil {
			if isProvisionErr(err){
				fmt.Println("Failed to provision")
				ssCopy.Status.Phase = v1alpha1.FailedPhase
				ssCopy.Status.Action = "provisioning"
				return sdk.Update(ssCopy)
			}
			return err
		}
		if ready {
			ssCopy.Status.Phase = v1alpha1.CompletePhase
			ssCopy.Status.Action = "provisioned"
			return sdk.Update(ssCopy)
		}
		return nil
	}
	if ssCopy.Labels == nil {
		ssCopy.Labels = map[string]string{}
	}

	if ssCopy.Status.Action != "provisioning" && ssCopy.Status.SharedServiceInstance == "" {
		si, err := h.allocateSharedServiceInstanceWithCapacity(ssCopy.Spec.ServiceType)
		if err != nil {
			return errors.Wrap(err, "unexpected error when looking for a service instance with capacity")
		}
		if si == nil {
			// todo update status
			fmt.Println("no si found with capcity")
			return errors.New("failed to find a service instance with capacity")
		}
		ssCopy.Status.SharedServiceInstance = si.Name
		ssCopy.Labels["SharedServiceInstance"] = si.Name
		return sdk.Update(ssCopy)
	}
	if ssCopy.Status.Action != "provisioning" && ssCopy.Status.SharedServiceInstance != "" {
		ssi, err := h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(h.operatorNS).Get(ssCopy.Status.SharedServiceInstance, metav1.GetOptions{})
		if err != nil {
			return err
		}
		sliceID, err := h.provisionSlice(ssCopy, ssi, ssCopy.Spec.ServiceType, "managed")
		if err != nil && !apierrors.IsNotFound(err) {
			// if is a not found err return
			return err
		}
		ssCopy.Status.Action = "provisioning"
		ssCopy.Labels["SliceServiceInstance"] = sliceID
		ssCopy.Status.SliceServiceInstance = sliceID
		return sdk.Update(ssCopy)
	}

	return nil
}

func (h *Handler) handleSharedServiceSliceDelete(service *v1alpha1.SharedServiceSlice) error {
	if err := h.serviceCatalogClient.ServicecatalogV1beta1().ServiceInstances(h.operatorNS).Delete(service.Status.SliceServiceInstance, &metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrap(err, "slice was deleted but failed to delete the backing service instance")
	}
	return nil
}

func (h *Handler) handleSharedServiceClientCreateUpdate(serviceClient *v1alpha1.SharedServiceClient) error {
	fmt.Println("called handleSharedServiceClientCreateUpdate")
	return nil
}

func (h *Handler) handleSharedServiceClientDelete(serviceClient *v1alpha1.SharedServiceClient) error {
	fmt.Println("called handleSharedServiceClientDelete")
	return nil
}
