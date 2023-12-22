package models

import (
	"errors"
	"fmt"

	dynamicclient "k8s.io/client-go/dynamic"
	kubernetesinformers "k8s.io/client-go/informers"
	kubernetesclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	bindclient "github.com/kube-bind/kube-bind/pkg/client/clientset/versioned"
	bindinformers "github.com/kube-bind/kube-bind/pkg/client/informers/externalversions"
	bindlisters "github.com/kube-bind/kube-bind/pkg/client/listers/kubebind/v1alpha1"
	"github.com/kube-bind/kube-bind/pkg/konnector/controllers/cluster/serviceexport/multinsinformer"
	"github.com/kube-bind/kube-bind/pkg/konnector/controllers/dynamic"
)

type ProviderInfo struct {
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
	return nil, errors.New(fmt.Sprintf("no provider information found with cluster id: %s", clusterID))
}

func GetProviderInfoWithProviderNamespace(providerInfos []*ProviderInfo, providerNamespace string) (*ProviderInfo, error) {
	for _, info := range providerInfos {
		if info.Namespace == providerNamespace {
			return info, nil
		}
	}
	return nil, errors.New(fmt.Sprintf("no provider information found with cluster id: %s", providerNamespace))
}
