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

package servicebinding

import (
	"context"
	"fmt"
	"time"

	"go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	bindclient "go.bytebuilders.dev/kube-bind/client/clientset/versioned"
	bindlisters "go.bytebuilders.dev/kube-bind/client/listers/kubebind/v1alpha1"
	"go.bytebuilders.dev/kube-bind/pkg/committer"
	"go.bytebuilders.dev/kube-bind/pkg/indexers"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/controllers/dynamic"
	konnectormodels "go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionslisters "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	controllerName = "kube-bind-konnector-cluster-servicebinding"
)

// NewController returns a new controller for ServiceBindings.
func NewController(
	reconcileServiceBinding func(binding *v1alpha1.APIServiceBinding) bool,
	consumerConfig *rest.Config,
	serviceBindingInformer dynamic.Informer[bindlisters.APIServiceBindingLister],
	crdInformer dynamic.Informer[apiextensionslisters.CustomResourceDefinitionLister],
	providerInfos []*konnectormodels.ProviderInfo,
) (*controller, error) {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)

	logger := klog.Background().WithValues("controller", controllerName)

	for _, provider := range providerInfos {
		provider.Config = rest.CopyConfig(provider.Config)
		provider.Config = rest.AddUserAgent(provider.Config, controllerName)
		var err error
		provider.BindClient, err = bindclient.NewForConfig(provider.Config)
		if err != nil {
			return nil, err
		}
	}

	consumerConfig = rest.CopyConfig(consumerConfig)
	consumerConfig = rest.AddUserAgent(consumerConfig, controllerName)

	consumerBindClient, err := bindclient.NewForConfig(consumerConfig)
	if err != nil {
		return nil, err
	}
	apiextensionsClient, err := apiextensionsclient.NewForConfig(consumerConfig)
	if err != nil {
		return nil, err
	}

	c := &controller{
		queue: queue,

		serviceBindingInformer: serviceBindingInformer,

		crdInformer:    crdInformer,
		providerInfors: providerInfos,

		reconciler: reconciler{
			providerInfos: providerInfos,

			reconcileServiceBinding: reconcileServiceBinding,

			getServiceExport: func(provider *konnectormodels.ProviderInfo, name string) (*v1alpha1.APIServiceExport, error) {
				return provider.BindInformer.KubeBind().V1alpha1().APIServiceExports().Lister().APIServiceExports(provider.Namespace).Get(name)
			},
			getServiceBinding: func(name string) (*v1alpha1.APIServiceBinding, error) {
				return serviceBindingInformer.Lister().Get(name)
			},
			getClusterBinding: func(ctx context.Context, provider *konnectormodels.ProviderInfo) (*v1alpha1.ClusterBinding, error) {
				return provider.BindClient.KubeBindV1alpha1().ClusterBindings(provider.Namespace).Get(ctx, "cluster", metav1.GetOptions{})
			},
			updateServiceExportStatus: func(ctx context.Context, export *v1alpha1.APIServiceExport, clusterID string) (*v1alpha1.APIServiceExport, error) {
				provider, err := konnectormodels.GetProviderInfoWithClusterID(providerInfos, clusterID)
				if err != nil {
					return nil, err
				}
				return provider.BindClient.KubeBindV1alpha1().APIServiceExports(provider.Namespace).UpdateStatus(ctx, export, metav1.UpdateOptions{})
			},
			getCRD: func(name string) (*apiextensionsv1.CustomResourceDefinition, error) {
				return crdInformer.Lister().Get(name)
			},
			updateCRD: func(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (*apiextensionsv1.CustomResourceDefinition, error) {
				return apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, crd, metav1.UpdateOptions{})
			},
			createCRD: func(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (*apiextensionsv1.CustomResourceDefinition, error) {
				return apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
			},
		},

		commit: committer.NewCommitter[*v1alpha1.APIServiceBinding, *v1alpha1.APIServiceBindingSpec, *v1alpha1.APIServiceBindingStatus](
			func(ns string) committer.Patcher[*v1alpha1.APIServiceBinding] {
				return consumerBindClient.KubeBindV1alpha1().APIServiceBindings()
			},
		),
	}

	for _, provider := range providerInfos {
		indexers.AddIfNotPresentOrDie(provider.BindInformer.KubeBind().V1alpha1().APIServiceExports().Informer().GetIndexer(), cache.Indexers{
			indexers.ServiceExportByCustomResourceDefinition: indexers.IndexServiceExportByCustomResourceDefinition,
		})

		_, err = provider.BindInformer.KubeBind().V1alpha1().APIServiceExports().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.enqueueServiceExport(logger, obj, provider)
			},
			UpdateFunc: func(_, newObj interface{}) {
				c.enqueueServiceExport(logger, newObj, provider)
			},
			DeleteFunc: func(obj interface{}) {
				c.enqueueServiceExport(logger, obj, provider)
			},
		})
		if err != nil {
			return nil, err
		}
	}

	return c, nil
}

type (
	Resource   = committer.Resource[*v1alpha1.APIServiceBindingSpec, *v1alpha1.APIServiceBindingStatus]
	CommitFunc = func(context.Context, *Resource, *Resource) error
)

// controller reconciles ServiceBindings with there ServiceExports counterparts.
type controller struct {
	queue workqueue.RateLimitingInterface

	serviceBindingInformer dynamic.Informer[bindlisters.APIServiceBindingLister]

	crdInformer    dynamic.Informer[apiextensionslisters.CustomResourceDefinitionLister]
	providerInfors []*konnectormodels.ProviderInfo

	reconciler

	commit CommitFunc
}

func (c *controller) enqueueServiceBinding(logger klog.Logger, obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	logger.V(2).Info("queueing APIServiceBinding", "key", key)
	c.queue.Add(key)
}

func (c *controller) enqueueServiceExport(logger klog.Logger, _ interface{}, provider *konnectormodels.ProviderInfo) {
	bindings, err := c.serviceBindingInformer.Informer().GetIndexer().ByIndex(indexers.ByServiceBindingKubeconfigSecret, provider.ConsumerSecretRefKey)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	klog.Infof("binding interface size: %d", len(bindings))
	fmt.Println(bindings)

	for _, obj := range bindings {
		binding := obj.(*v1alpha1.APIServiceBinding)
		key, err := cache.MetaNamespaceKeyFunc(binding)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		logger.V(2).Info("queueing APIServiceBinding", "key", key, "reason", "APIServiceExport")
		c.queue.Add(key)
	}
}

func (c *controller) enqueueCRD(logger klog.Logger, obj interface{}) {
	name, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	var exports []interface{}
	for _, provider := range c.providerInfors {
		exps, err := provider.BindInformer.KubeBind().V1alpha1().APIServiceExports().Informer().GetIndexer().ByIndex(indexers.ServiceExportByCustomResourceDefinition, name)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		exports = append(exports, exps...)
	}
	for _, obj := range exports {
		export := obj.(*v1alpha1.APIServiceExport)
		key := export.Name
		logger.V(2).Info("queueing APIServiceBinding", "key", key, "reason", "CustomResourceDefinition", "CustomResourceDefinitionKey", name)
		c.queue.Add(key)
	}
}

// Start starts the controller, which stops when ctx.Done() is closed.
func (c *controller) Start(ctx context.Context, numThreads int) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	logger := klog.FromContext(ctx).WithValues("controller", controllerName)

	logger.Info("Starting controller")
	defer logger.Info("Shutting down controller")

	c.serviceBindingInformer.Informer().AddDynamicEventHandler(ctx, controllerName, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueServiceBinding(logger, obj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.enqueueServiceBinding(logger, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueServiceBinding(logger, obj)
		},
	})

	c.crdInformer.Informer().AddDynamicEventHandler(ctx, controllerName, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueCRD(logger, obj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.enqueueCRD(logger, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueCRD(logger, obj)
		},
	})

	for i := 0; i < numThreads; i++ {
		go wait.UntilWithContext(ctx, c.startWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *controller) startWorker(ctx context.Context) {
	defer runtime.HandleCrash()

	for c.processNextWorkItem(ctx) {
	}
}

func (c *controller) processNextWorkItem(ctx context.Context) bool {
	// Wait until there is a new item in the working queue
	k, quit := c.queue.Get()
	if quit {
		return false
	}
	key := k.(string)

	logger := klog.FromContext(ctx).WithValues("key", key)
	ctx = klog.NewContext(ctx, logger)
	logger.V(2).Info("processing key")

	// No matter what, tell the queue we're done with this key, to unblock
	// other workers.
	defer c.queue.Done(key)

	if err := c.process(ctx, key); err != nil {
		runtime.HandleError(fmt.Errorf("%q controller failed to sync %q, err: %w", controllerName, key, err))
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

func (c *controller) process(ctx context.Context, key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(err)
		return nil // we cannot do anything
	}

	logger := klog.FromContext(ctx)

	obj, err := c.serviceBindingInformer.Lister().Get(name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		logger.Error(err, "APIServiceBinding disappeared")
		return nil
	}

	old := obj
	obj = obj.DeepCopy()

	var errs []error
	if err := c.reconcile(ctx, obj); err != nil {
		errs = append(errs, err)
	}

	// Regardless of whether reconcile returned an error or not, always try to patch status if needed. Return the
	// reconciliation error at the end.

	// If the object being reconciled changed as a result, update it.
	oldResource := &Resource{ObjectMeta: old.ObjectMeta, Spec: &old.Spec, Status: &old.Status}
	newResource := &Resource{ObjectMeta: obj.ObjectMeta, Spec: &obj.Spec, Status: &obj.Status}
	if err := c.commit(ctx, oldResource, newResource); err != nil {
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}
