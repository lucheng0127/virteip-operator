# virteip-operator
**virteip-operator** is an extension for kubevirt, aim to provide an access to kubevirt vmi through elastic ip.

User create an eipbinding CRD, and operator will create a job to call [kube-eip](https://github.com/lucheng0127/kube-eip) provide an elastic ip access to kubevirt vmi.

## Description
The operator watch the EipBinding and kubevirt VMI resources, if EipBinding or kubevirt VMI info change, compare it if need change create a job to call kube-eip agent to clean up or apply a new eip binding rules to target hyper. And the eip binding rules will make kubevirt vmi accessable by elastic ip defined in EipBinding.

EipBinding Example
```
apiVersion: virteip.github.com/v1
kind: EipBinding
metadata:
  labels:
    app.kubernetes.io/name: eipbinding
    app.kubernetes.io/instance: eipbinding-sample
    app.kubernetes.io/part-of: virteip-operator
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/created-by: virteip-operator
  name: eipbinding-sample
spec:
  vmi: cirros
  eip: 192.168.137.50
```
* vmi: instance name of kubevirt virtual machine
* eip: elastic ipv4 address

## Getting Started

### Prerequisites
- nerdctl version 2.0.0-beta.2+.
- Access to a Kubernetes cluster.

### To Deploy on the cluster
**Current tested IMG**:

* quay.io/shawnlu0127/virteip-operator:20240321
* quay.io/shawnlu0127/eipctl:20240319 (job pod image)

_The eipctl images x86 only right now, for arm64 check the readme of [kube-eip](https://github.com/lucheng0127/kube-eip) to build arm64 image_

Change the image in code and rebuild the operator image
```
# virteip-operator/internal/controller/eipbinding_controller.go line 44
```
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/virteip-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified. 
And it is required to have access to pull the image from the working environment. 
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/virteip-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin 
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/virteip-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/virteip-operator/<tag or branch>/dist/install.yaml
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

