/*
Copyright 2016 The Rook Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Some of the code was modified from https://github.com/coreos/etcd-operator
which also has the apache 2.0 license.
*/

// Package kit for Kubernetes operators
package kit

import (
	"fmt"
	"time"

	"k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	errorsUtil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/util/version"
)

const (
	interval = 500 * time.Millisecond
	timeout  = 60 * time.Second
)

// CustomResource is for creating a Kubernetes TPR/CRD
type CustomResource struct {
	// Name of the custom resource
	Name string

	// Plural of the custom resource in plural
	Plural string

	// Group the custom resource belongs to
	Group string

	// Version which should be defined in a const above
	Version string

	// Scope of the CRD. Namespaced or cluster
	Scope apiextensionsv1beta1.ResourceScope

	// Kind is the serialized interface of the resource.
	Kind string
}

// CreateCustomResource creates a single custom resource,and wait for it to be active
// The resource is of kind CRD if the Kubernetes server is 1.7.0.
// The resource is of kind TPR if the Kubernetes server is before 1.7.0.
func CreateCustomResource(resource CustomResource, clientset kubernetes.Interface) error {

	// CRD is available on v1.7.0. TPR became deprecated on v1.7.0
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("Error getting server version: %v", err)
	}
	kubeVersion := version.MustParseSemantic(serverVersion.GitVersion)

	if kubeVersion.AtLeast(version.MustParseSemantic("v1.7.0")) {
		return createCRD(resource)
	}

	// Create TPR for Kubernetes version less than v1.7.0
	return createTPR(resource, clientset)
}

func createCRD(resource CustomResource) error {
	logger.Infof("creating %s CRD", resource.Name)
	crdName := fmt.Sprintf("%s.%s", resource.Plural, resource.Group)
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crdName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   resource.Group,
			Version: resource.Version,
			Scope:   resource.Scope,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Singular: resource.Name,
				Plural:   resource.Plural,
				Kind:     resource.Kind,
			},
		},
	}
	clientset, err := getApiextensionsClientset()
	if err != nil {
		return fmt.Errorf("failed to get Apiextensions client. %+v", err)
	}
	_, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create %s CRD. %+v", resource.Name, err)
		}
	}

	// wait for CRD being established
	err = wait.Poll(interval, timeout, func() (bool, error) {
		crd, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crdName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, err
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					fmt.Printf("Name conflict: %v\n", cond.Reason)
				}
			}
		}
		return false, err
	})
	if err != nil {
		deleteErr := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crdName, nil)
		if deleteErr != nil {
			return errorsUtil.NewAggregate([]error{err, deleteErr})
		}
		return err
	}
	return nil
}

func createTPR(resource CustomResource, clientset kubernetes.Interface) error {
	logger.Infof("creating %s TPR", resource.Name)
	tprName := fmt.Sprintf("%s.%s", resource.Name, resource.Group)
	tpr := &v1beta1.ThirdPartyResource{
		ObjectMeta: metav1.ObjectMeta{
			Name: tprName,
		},
		Versions: []v1beta1.APIVersion{
			{Name: resource.Version},
		},
		Description: fmt.Sprintf("ThirdPartyResource for Rook %s", resource.Name),
	}
	_, err := clientset.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create %s TPR. %+v", resource.Name, err)
		}
	}

	// wait for TPR being established
	restcli := clientset.CoreV1().RESTClient()
	uri := fmt.Sprintf("apis/%s/%s/%s", resource.Group, resource.Version, resource.Plural)
	err = wait.Poll(interval, timeout, func() (bool, error) {
		_, err := restcli.Get().RequestURI(uri).DoRaw()
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil

	})
	if err != nil {
		deleteErr := clientset.ExtensionsV1beta1().ThirdPartyResources().Delete(tprName, nil)
		if deleteErr != nil {
			return errorsUtil.NewAggregate([]error{err, deleteErr})
		}
		return err
	}
	return nil
}
