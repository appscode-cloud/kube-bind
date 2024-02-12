package models

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	dynamicclient "k8s.io/client-go/dynamic"
	kubernetesinformers "k8s.io/client-go/informers"
	kubernetesclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	bindclient "go.bytebuilders.dev/kube-bind/pkg/client/clientset/versioned"
	bindinformers "go.bytebuilders.dev/kube-bind/pkg/client/informers/externalversions"
	bindlisters "go.bytebuilders.dev/kube-bind/pkg/client/listers/kubebind/v1alpha1"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/cluster/serviceexport/multinsinformer"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/dynamic"
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

func IsMatchProvider(provider *ProviderInfo, obj interface{}) bool {
	unstr, found := obj.(*unstructured.Unstructured)
	if !found {
		return false
	}
	annos := unstr.GetAnnotations()

	return annos[AnnotationProviderClusterID] == provider.ClusterID
}

func GetProviderFromObjectInterface(providerInfos []*ProviderInfo, obj interface{}) (*ProviderInfo, error) {
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
