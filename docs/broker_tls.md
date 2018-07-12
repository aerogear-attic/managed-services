
# How the broker uses TLS/SSL

When deploying to an OpenShift cluster, the broker is configured for TLS/SSL using the CA built into OpenShift. 
This is done by adding an OpenShift specific annotation to the broker's `Service` definition: 

```yaml
...
kind: Service
metadata:
  name: msb
  labels:
    app:  managed-services-broker
    service: msb
  annotations:
    service.alpha.openshift.io/serving-cert-secret-name: msb-tls
...
```

The annotation uses the built in CA to generate a signed cert and key in a secret called `msb-tls`. The certs are then added as environment variables to the broker container:

```yaml
env:
- name: TLS_CERT
    valueFrom:
    secretKeyRef:
        name: msb-tls
        key: tls.crt
- name: TLS_KEY
    valueFrom:
    secretKeyRef:
        name: msb-tls
        key: tls.key
```

The Service Catalog must be provided with the caBundle so that it can validate the certificate signing chain. 
The CA (base64 encoded) is added to the `ClusterServiceBroker` definition, in `spec.caBundle`:

```yaml
kind: ClusterServiceBroker
  metadata:
    name: managed-services-broker
  spec:
    caBundle: LS0tLS1CRUd...
```

The caBundle is retrieved by running the following command, and then converting to base64: 
```sh
oc get secret -n kube-service-catalog -o go-template='{{ range .items }}{{ if eq .type "kubernetes.io/service-account-token" }}{{ index .data "service-ca.crt" }}{{end}}{{"\n"}}{{end}}' | tail -n1
```