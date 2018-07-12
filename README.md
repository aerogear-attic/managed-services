# Managed Services

## Managed Services Operator

## Managed Services Broker

### Deploying the broker

An OpenShift template in the `templates` directory of this repo is used to deploy the broker to a running OpenShift cluster. This assumes that you've installed the Service Catalog onto your cluster and also have the [`svcat` command line tool](https://github.com/kubernetes-incubator/service-catalog/blob/master/docs/install.md) installed.

```sh
# Build and push the broker image
make build_broker_image
make push_broker 

# Switch to a new project
oc new-project managed-services-broker

# Process the template and create the broker deployment
oc process -f templates/broker.template.yaml | oc create -f -

# Verify that the broker has been registered correctly
svcat get brokers

# View the status of the broker
oc describe clusterservicebroker managed-services-broker
```

### TLS Encryption

An explanation of how the broker uses TLS/SSL encryption can be found [here](./docs/broker_tls.md).