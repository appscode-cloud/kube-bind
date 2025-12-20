/*
Copyright AppsCode Inc. and Contributors

Licensed under the AppsCode Community License 1.0.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/appscode/licenses/raw/1.0.0/AppsCode-Community-1.0.0.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package models

import (
	"fmt"

	bindclient "go.bytebuilders.dev/kube-bind/client/clientset/versioned"
	bindinformers "go.bytebuilders.dev/kube-bind/client/informers/externalversions"
	bindlisters "go.bytebuilders.dev/kube-bind/client/listers/kubebind/v1alpha1"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/cluster/serviceexport/multinsinformer"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/dynamic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	dynamicclient "k8s.io/client-go/dynamic"
	kubernetesinformers "k8s.io/client-go/informers"
	kubernetesclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type ProviderInfo struct {
	// consumerSecretRefKey is the namespace/name value of the APIServiceBinding kubeconfig secret reference.
	Namespace, NamespaceUID, ClusterID, ConsumerSecretRefKey string
	Config                                                   *rest.Config
	Client                                                   dynamicclient.Interface
	BindClient                                               *bindclient.Clientset
	KubeClient                                               *kubernetesclient.Clientset
	BindInformer                                             bindinformers.SharedInformerFactory
	KubeInformer                                             kubernetesinformers.SharedInformerFactory
	DynamicServiceNamespaceInformer                          dynamic.Informer[bindlisters.APIServiceNamespaceLister]
	ProviderDynamicInformer                                  multinsinformer.GetterInformer
}

func GetProviderInfoWithClusterID(providerInfos []*ProviderInfo, clusterID string) (*ProviderInfo, error) {
	for _, info := range providerInfos {
		if info.ClusterID == clusterID || clusterID == "" {
			return info, nil
		}
	}
	return nil, fmt.Errorf("no provider information found with cluster id: %s", clusterID)
}

func GetProviderInfoWithProviderNamespace(providerInfos []*ProviderInfo, providerNamespace string) (*ProviderInfo, error) {
	for _, info := range providerInfos {
		if info.Namespace == providerNamespace {
			return info, nil
		}
	}
	return nil, fmt.Errorf("no provider information found with namespace: %s", providerNamespace)
}

func IsMatchProvider(provider *ProviderInfo, obj any) bool {
	unstr, found := obj.(*unstructured.Unstructured)
	if !found {
		return false
	}
	annos := unstr.GetAnnotations()

	return annos[AnnotationProviderClusterID] == provider.ClusterID
}

func GetProviderFromObjectInterface(providerInfos []*ProviderInfo, obj any) (*ProviderInfo, error) {
	unstr, found := obj.(*unstructured.Unstructured)
	if !found {
		return nil, fmt.Errorf("failed to convert interface into unstructured")
	}
	annos := unstr.GetAnnotations()
	for _, provider := range providerInfos {
		if annos[AnnotationProviderClusterID] == provider.ClusterID {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("no provider found for object %s", unstr.GetName())
}
