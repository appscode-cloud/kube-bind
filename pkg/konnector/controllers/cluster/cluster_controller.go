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

package cluster

import (
	"context"
	"reflect"
	"time"

	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	bindclient "go.bytebuilders.dev/kube-bind/client/clientset/versioned"
	bindinformers "go.bytebuilders.dev/kube-bind/client/informers/externalversions"
	bindlisters "go.bytebuilders.dev/kube-bind/client/listers/kubebind/v1alpha1"
	"go.bytebuilders.dev/kube-bind/pkg/indexers"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/cluster/clusterbinding"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/cluster/namespacedeletion"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/cluster/servicebinding"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/cluster/serviceexport"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/dynamic"
	konnectormodels "go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	crdlisters "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubernetesinformers "k8s.io/client-go/informers"
	kubernetesclient "k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	conditionsapi "kmodules.xyz/client-go/api/v1"
	"kmodules.xyz/client-go/conditions"
)

const (
	controllerName = "kube-bind-konnector-cluster"

	heartbeatInterval = 5 * time.Minute // TODO: make configurable
)

// NewController returns a new controller handling one cluster connection.
func NewController(
	reconcileServiceBinding func(binding *kubebindv1alpha1.APIServiceBinding) bool,
	consumerConfig *rest.Config,
	providerInfos []*konnectormodels.ProviderInfo,
	namespaceInformer dynamic.Informer[corelisters.NamespaceLister],
	serviceBindingInformer dynamic.Informer[bindlisters.APIServiceBindingLister],
	crdInformer dynamic.Informer[crdlisters.CustomResourceDefinitionLister],
) (*controller, error) {
	consumerConfig = rest.CopyConfig(consumerConfig)
	consumerConfig = rest.AddUserAgent(consumerConfig, controllerName)

	for _, provider := range providerInfos {
		provider.Config = rest.CopyConfig(provider.Config)
		provider.Config = rest.AddUserAgent(provider.Config, controllerName)

		// create shared informer factories
		var err error
		if provider.BindClient, err = bindclient.NewForConfig(provider.Config); err != nil {
			return nil, err
		}
		if provider.KubeClient, err = kubernetesclient.NewForConfig(provider.Config); err != nil {
			return nil, err
		}

		provider.BindInformer = bindinformers.NewSharedInformerFactoryWithOptions(provider.BindClient, time.Minute*30, bindinformers.WithNamespace(provider.Namespace))
		provider.KubeInformer = kubernetesinformers.NewSharedInformerFactoryWithOptions(provider.KubeClient, time.Minute*30, kubernetesinformers.WithNamespace(provider.Namespace))
	}

	consumerBindClient, err := bindclient.NewForConfig(consumerConfig)
	if err != nil {
		return nil, err
	}
	consumerKubeClient, err := kubernetesclient.NewForConfig(consumerConfig)
	if err != nil {
		return nil, err
	}
	consumerSecretInformers := kubernetesinformers.NewSharedInformerFactoryWithOptions(consumerKubeClient, time.Minute*30)

	// create controllers
	clusterbindingCtrl, err := clusterbinding.NewController(
		heartbeatInterval,
		consumerConfig,
		serviceBindingInformer,
		consumerSecretInformers.Core().V1().Secrets(),
		providerInfos,
	)
	if err != nil {
		return nil, err
	}
	namespacedeletionCtrl, err := namespacedeletion.NewController(
		providerInfos,
		namespaceInformer,
	)
	if err != nil {
		return nil, err
	}
	servicebindingCtrl, err := servicebinding.NewController(
		reconcileServiceBinding,
		consumerConfig,
		serviceBindingInformer,
		crdInformer,
		providerInfos,
	)
	if err != nil {
		return nil, err
	}
	serviceexportCtrl, err := serviceexport.NewController(
		consumerConfig,
		serviceBindingInformer,
		crdInformer,
		providerInfos,
	)
	if err != nil {
		return nil, err
	}

	var factories []SharedInformerFactory
	for _, provider := range providerInfos {
		factories = append(factories, provider.BindInformer)
		factories = append(factories, provider.KubeInformer)
	}
	factories = append(factories, consumerSecretInformers)

	return &controller{
		providerInfos: providerInfos,
		bindClient:    consumerBindClient,

		factories: factories,

		serviceBindingLister:  serviceBindingInformer.Lister(),
		serviceBindingIndexer: serviceBindingInformer.Informer().GetIndexer(),

		clusterbindingCtrl:         clusterbindingCtrl,
		namespacedeletionCtrl:      namespacedeletionCtrl,
		servicebindingCtrl:         servicebindingCtrl,
		serviceresourcebindingCtrl: serviceexportCtrl,
	}, nil
}

type GenericController interface {
	Start(ctx context.Context, numThreads int)
}

type SharedInformerFactory interface {
	Start(stopCh <-chan struct{})
	WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool
}

// controller holding all controller that are per provider cluster.
type controller struct {
	providerInfos []*konnectormodels.ProviderInfo

	bindClient bindclient.Interface

	serviceBindingLister  bindlisters.APIServiceBindingLister
	serviceBindingIndexer cache.Indexer

	factories []SharedInformerFactory

	clusterbindingCtrl         GenericController
	namespacedeletionCtrl      GenericController
	servicebindingCtrl         GenericController
	serviceresourcebindingCtrl GenericController
}

// Start starts the controller, which stops when ctx.Done() is closed.
func (c *controller) Start(ctx context.Context) {
	logger := klog.FromContext(ctx).WithValues("controller", controllerName)
	ctx = klog.NewContext(ctx, logger)

	logger.V(2).Info("starting factories")
	for _, factory := range c.factories {
		factory.Start(ctx.Done())
	}

	if err := wait.PollUntilContextCancel(ctx, heartbeatInterval, true, func(ctx context.Context) (bool, error) {
		waitCtx, cancel := context.WithDeadline(ctx, time.Now().Add(heartbeatInterval/2))
		defer cancel()

		logger.V(2).Info("waiting for cache sync")
		for _, factory := range c.factories {
			synced := factory.WaitForCacheSync(waitCtx.Done())
			logger.V(2).Info("cache sync", "synced", synced)
		}
		select {
		case <-ctx.Done():
			// timeout
			logger.Info("informers did not sync in time", "timeout", heartbeatInterval/2)
			c.updateServiceBindings(ctx, func(binding *kubebindv1alpha1.APIServiceBinding) {
				conditions.MarkFalse(
					binding,
					kubebindv1alpha1.APIServiceBindingConditionInformersSynced,
					"InformerSyncTimeout",
					conditionsapi.ConditionSeverityError,
					"Informers did not sync within %s",
					heartbeatInterval/2,
				)
			})

			return false, nil
		default:
			return true, nil
		}
	}); err != nil {
		runtime.HandleError(err)
		return
	}

	logger.V(2).Info("setting InformersSynced condition to true on service binding")
	c.updateServiceBindings(ctx, func(binding *kubebindv1alpha1.APIServiceBinding) {
		conditions.MarkTrue(binding, kubebindv1alpha1.APIServiceBindingConditionInformersSynced)
	})

	go c.clusterbindingCtrl.Start(ctx, 2)
	go c.namespacedeletionCtrl.Start(ctx, 2)
	go c.servicebindingCtrl.Start(ctx, 2)
	go c.serviceresourcebindingCtrl.Start(ctx, 2)

	<-ctx.Done()
}

func (c *controller) updateServiceBindings(ctx context.Context, update func(*kubebindv1alpha1.APIServiceBinding)) {
	logger := klog.FromContext(ctx)

	if c.providerInfos == nil {
		klog.Infof("no provider information found for cluster controller")
		return
	}
	objs, err := c.serviceBindingIndexer.ByIndex(indexers.ByServiceBindingKubeconfigSecret, c.providerInfos[0].ConsumerSecretRefKey)
	if err != nil {
		logger.Error(err, "failed to list service bindings")
		return
	}
	for _, obj := range objs {
		binding := obj.(*kubebindv1alpha1.APIServiceBinding)
		orig := binding
		binding = binding.DeepCopy()
		update(binding)
		if !reflect.DeepEqual(binding.Status.Conditions, orig.Status.Conditions) {
			logger.V(2).Info("updating service binding", "binding", binding.Name)
			if _, err := c.bindClient.KubeBindV1alpha1().APIServiceBindings().UpdateStatus(ctx, binding, metav1.UpdateOptions{}); err != nil {
				logger.Error(err, "failed to update service binding", "binding", binding.Name)
				continue
			}
		}
	}
}
