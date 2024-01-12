/*
Copyright 2022 The Kube Bind Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterbinding

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	kubernetesclient "k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	kubebindv1alpha1 "github.com/kube-bind/kube-bind/pkg/apis/kubebind/v1alpha1"
	conditionsapi "github.com/kube-bind/kube-bind/pkg/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kube-bind/kube-bind/pkg/apis/third_party/conditions/util/conditions"
	bindclient "github.com/kube-bind/kube-bind/pkg/client/clientset/versioned"
	bindlisters "github.com/kube-bind/kube-bind/pkg/client/listers/kubebind/v1alpha1"
	"github.com/kube-bind/kube-bind/pkg/committer"
	"github.com/kube-bind/kube-bind/pkg/indexers"
	"github.com/kube-bind/kube-bind/pkg/konnector/controllers/dynamic"
	konnectormodels "github.com/kube-bind/kube-bind/pkg/konnector/models"
)

const (
	controllerName = "kube-bind-konnector-clusterbinding"
)

// NewController returns a new controller for ClusterBindings.
func NewController(
	heartbeatInterval time.Duration,
	consumerConfig *rest.Config,
	serviceBindingInformer dynamic.Informer[bindlisters.APIServiceBindingLister],
	consumerSecretInformer coreinformers.SecretInformer,
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
		provider.KubeClient, err = kubernetesclient.NewForConfig(provider.Config)
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
	consumerKubeClient, err := kubernetesclient.NewForConfig(consumerConfig)
	if err != nil {
		return nil, err
	}

	c := &controller{
		queue: queue,

		consumerBindClient:     consumerBindClient,
		consumerKubeClient:     consumerKubeClient,
		providerInfos:          providerInfos,
		serviceBindingInformer: serviceBindingInformer,
		consumerSecretLister:   consumerSecretInformer.Lister(),

		reconciler: reconciler{
			heartbeatInterval: heartbeatInterval,
			providerInfos:     providerInfos,

			getProviderInfo: func(clusterID string) (*konnectormodels.ProviderInfo, error) {
				for _, provider := range providerInfos {
					if provider.ClusterID == clusterID {
						return provider, nil
					}
				}
				return nil, fmt.Errorf(fmt.Sprintf("no provider registered for cluster id: %s in ClusterBinding controller", clusterID))
			},
			getProviderSecret: func(provider *konnectormodels.ProviderInfo) (*corev1.Secret, error) {
				cb, err := provider.BindInformer.KubeBind().V1alpha1().ClusterBindings().Lister().ClusterBindings(provider.Namespace).Get("cluster")
				if err != nil {
					return nil, err
				}
				ref := &cb.Spec.KubeconfigSecretRef
				return provider.KubeInformer.Core().V1().Secrets().Lister().Secrets(provider.Namespace).Get(ref.Name)
			},
			getConsumerSecret: func(provider *konnectormodels.ProviderInfo) (*corev1.Secret, error) {
				ns, name, err := cache.SplitMetaNamespaceKey(provider.ConsumerSecretRefKey)
				if err != nil {
					return nil, err
				}
				return consumerSecretInformer.Lister().Secrets(ns).Get(name)
			},
			createConsumerSecret: func(ctx context.Context, secret *corev1.Secret) (*corev1.Secret, error) {
				return consumerKubeClient.CoreV1().Secrets(secret.Namespace).Create(ctx, secret, metav1.CreateOptions{})
			},
			updateConsumerSecret: func(ctx context.Context, secret *corev1.Secret) (*corev1.Secret, error) {
				return consumerKubeClient.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
			},
		},

		commit: committer.NewCommitter[*kubebindv1alpha1.ClusterBinding, *kubebindv1alpha1.ClusterBindingSpec, *kubebindv1alpha1.ClusterBindingStatus](
			func(ns string) committer.Patcher[*kubebindv1alpha1.ClusterBinding] {
				providerInfo, err := konnectormodels.GetProviderInfoWithProviderNamespace(providerInfos, ns)
				if err != nil {
					klog.Warningf(err.Error())
					return nil
				}
				return providerInfo.BindClient.KubeBindV1alpha1().ClusterBindings(ns)
			},
		),
	}

	for _, provider := range providerInfos {
		provider.BindInformer.KubeBind().V1alpha1().ClusterBindings().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.enqueueClusterBinding(logger, obj)
			},
			UpdateFunc: func(_, newObj interface{}) {
				c.enqueueClusterBinding(logger, newObj)
			},
			DeleteFunc: func(obj interface{}) {
				c.enqueueClusterBinding(logger, obj)
			},
		})

		provider.KubeInformer.Core().V1().Secrets().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.enqueueProviderSecret(logger, obj, c.providerInfos)
			},
			UpdateFunc: func(_, newObj interface{}) {
				c.enqueueProviderSecret(logger, newObj, c.providerInfos)
			},
			DeleteFunc: func(obj interface{}) {
				c.enqueueProviderSecret(logger, obj, c.providerInfos)
			},
		})

		provider.BindInformer.KubeBind().V1alpha1().APIServiceExports().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.enqueueServiceExport(logger, obj)
			},
			UpdateFunc: func(old, newObj interface{}) {
				oldExport, ok := old.(*kubebindv1alpha1.APIServiceExport)
				if !ok {
					return
				}
				newExport, ok := old.(*kubebindv1alpha1.APIServiceExport)
				if !ok {
					return
				}
				if reflect.DeepEqual(oldExport.Status.Conditions, newExport.Status.Conditions) {
					return
				}
				c.enqueueServiceExport(logger, newObj)
			},
			DeleteFunc: func(obj interface{}) {
				c.enqueueServiceExport(logger, obj)
			},
		})

		consumerSecretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.enqueueConsumerSecret(logger, obj, provider.Namespace, provider.ConsumerSecretRefKey)
			},
			UpdateFunc: func(_, newObj interface{}) {
				c.enqueueConsumerSecret(logger, newObj, provider.Namespace, provider.ConsumerSecretRefKey)
			},
			DeleteFunc: func(obj interface{}) {
				c.enqueueConsumerSecret(logger, obj, provider.Namespace, provider.ConsumerSecretRefKey)
			},
		})
	}

	return c, nil
}

type Resource = committer.Resource[*kubebindv1alpha1.ClusterBindingSpec, *kubebindv1alpha1.ClusterBindingStatus]
type CommitFunc = func(context.Context, *Resource, *Resource) error

// controller reconciles ClusterBindings on the service provider cluster, including heartbeating.
type controller struct {
	queue workqueue.RateLimitingInterface

	consumerBindClient bindclient.Interface
	consumerKubeClient kubernetesclient.Interface

	clusterBindingIndexer cache.Indexer

	serviceBindingInformer dynamic.Informer[bindlisters.APIServiceBindingLister]

	consumerSecretLister corelisters.SecretLister
	providerInfos        []*konnectormodels.ProviderInfo

	reconciler

	commit CommitFunc
}

func (c *controller) enqueueClusterBinding(logger klog.Logger, obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	logger.V(2).Info("queueing ClusterBinding", "key", key)
	c.queue.Add(key)
}

func (c *controller) enqueueConsumerSecret(logger klog.Logger, obj interface{}, providerNamespace, consumerSecretRefKey string) {
	secretKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	if secretKey == consumerSecretRefKey {
		key := providerNamespace + "/cluster"
		logger.V(2).Info("queueing ClusterBinding", "key", key, "reason", "ConsumerSecret", "SecretKey", secretKey)
		c.queue.Add(key)
	}
}

func (c *controller) enqueueProviderSecret(logger klog.Logger, obj interface{}, providerInfos []*konnectormodels.ProviderInfo) {
	secretKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	ns, name, err := cache.SplitMetaNamespaceKey(secretKey)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	var clusterBindingLister bindlisters.ClusterBindingLister

	for _, info := range providerInfos {
		if info.Namespace == ns {
			clusterBindingLister = info.BindInformer.KubeBind().V1alpha1().ClusterBindings().Lister()
		}
	}

	if clusterBindingLister == nil {
		// no provider namespace matching with this obj ns
		return
	}

	binding, err := clusterBindingLister.ClusterBindings(ns).Get("cluster")
	if err != nil && !errors.IsNotFound(err) {
		runtime.HandleError(err)
		return
	} else if errors.IsNotFound(err) {
		return // skip this secret
	}
	if binding.Spec.KubeconfigSecretRef.Name != name {
		return // skip this secret
	}

	key := ns + "/cluster"
	logger.V(2).Info("queueing ClusterBinding", "key", key, "reason", "Secret", "SecretKey", secretKey)
	c.queue.Add(key)
}

func (c *controller) enqueueServiceExport(logger klog.Logger, obj interface{}) {
	seKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	ns, _, err := cache.SplitMetaNamespaceKey(seKey)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	key := ns + "/cluster"
	logger.V(2).Info("queueing ClusterBinding", "key", key, "reason", "APIServiceExport", "ServiceExportKey", seKey)
	c.queue.Add(key)
}

// Start starts the controller, which stops when ctx.Done() is closed.
func (c *controller) Start(ctx context.Context, numThreads int) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	logger := klog.FromContext(ctx).WithValues("controller", controllerName)

	logger.Info("Starting controller")
	defer logger.Info("Shutting down controller")

	for i := 0; i < numThreads; i++ {
		go wait.UntilWithContext(ctx, c.startWorker, time.Second)
	}

	// start the heartbeat
	// nolint:errcheck
	for _, provider := range c.providerInfos {
		wait.PollInfiniteWithContext(ctx, c.heartbeatInterval/2, func(ctx context.Context) (bool, error) {
			c.queue.Add(provider.Namespace + "/cluster")
			return false, nil
		})
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
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(err)
		return nil // we cannot do anything
	}
	if name != "cluster" {
		return nil // cannot happen by OpenAPI validation
	}

	var clusterBindingLister bindlisters.ClusterBindingLister
	var consumerSecretRefKey string

	for _, provider := range c.providerInfos {
		if provider.Namespace == ns {
			clusterBindingLister = provider.BindInformer.KubeBind().V1alpha1().ClusterBindings().Lister()
			consumerSecretRefKey = provider.ConsumerSecretRefKey
			break
		}
	}
	if clusterBindingLister == nil {
		return fmt.Errorf("can not get any cluster binding informer for cluster binding object of namespace: %s", ns)
	}

	logger := klog.FromContext(ctx)

	obj, err := clusterBindingLister.ClusterBindings(ns).Get("cluster")
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		logger.Error(err, "ClusterBinding disappeared")
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

		// try to update service bindings
		c.updateServiceBindings(ctx, func(binding *kubebindv1alpha1.APIServiceBinding) {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionHeartbeating,
				"ClusterBindingUpdateFailed",
				conditionsapi.ConditionSeverityWarning,
				"Failed to update service provider ClusterBinding: %v", err,
			)
		}, consumerSecretRefKey)
	} else {
		// try to update service bindings
		c.updateServiceBindings(ctx, func(binding *kubebindv1alpha1.APIServiceBinding) {
			conditions.MarkTrue(binding, kubebindv1alpha1.APIServiceBindingConditionHeartbeating)
		}, consumerSecretRefKey)
	}

	return utilerrors.NewAggregate(errs)
}

func (c *controller) updateServiceBindings(ctx context.Context, update func(*kubebindv1alpha1.APIServiceBinding), consumerSecretRefKey string) {
	logger := klog.FromContext(ctx)

	objs, err := c.serviceBindingInformer.Informer().GetIndexer().ByIndex(indexers.ByServiceBindingKubeconfigSecret, consumerSecretRefKey)
	if err != nil {
		logger.Error(err, "failed to list service bindings", "secretKey", consumerSecretRefKey)
		return
	}
	for _, obj := range objs {
		binding := obj.(*kubebindv1alpha1.APIServiceBinding)
		orig := binding
		binding = binding.DeepCopy()
		update(binding)
		if !reflect.DeepEqual(binding.Status.Conditions, orig.Status.Conditions) {
			logger.V(2).Info("updating service binding", "binding", binding.Name)
			if _, err := c.consumerBindClient.KubeBindV1alpha1().APIServiceBindings().UpdateStatus(ctx, binding, metav1.UpdateOptions{}); err != nil {
				logger.Error(err, "failed to update service binding", "binding", binding.Name)
				continue
			}
		}
	}
}
