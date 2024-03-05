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

package konnector

import (
	"context"
	"reflect"
	"sync"

	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	konnectormodels "go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

//const namespaceKubeSystem = "kube-system"

type startable interface {
	Start(ctx context.Context)
}

type reconciler struct {
	lock        sync.RWMutex
	controllers map[string]*controllerContext // by service binding name

	providerInfos []*konnectormodels.ProviderInfo

	newClusterController func(providerInfos []*konnectormodels.ProviderInfo, reconcileServiceBinding func(binding *kubebindv1alpha1.APIServiceBinding) bool) (startable, error)
	getSecret            func(ns, name string) (*corev1.Secret, error)
}

type controllerContext struct {
	kubeconfig      []string
	cancel          func()
	serviceBindings sets.Set[string] // when this is empty, the Controller should be stopped by closing the context
}

type providerIdentifier struct {
	kubeconfig, secretRefName, secretRefNamespace, clusterUID string
}

func (r *reconciler) reconcile(ctx context.Context, binding *kubebindv1alpha1.APIServiceBinding) error {
	logger := klog.FromContext(ctx)

	var kubeconfigs []string
	var identifiers []providerIdentifier

	refs := binding.Spec.KubeconfigSecretRefs
	for _, ref := range refs {
		secret, err := r.getSecret(ref.Namespace, ref.Name)
		if err != nil && !errors.IsNotFound(err) {
			return err
		} else if errors.IsNotFound(err) {
			logger.V(2).Info("secret not found", "secret", ref.Namespace+"/"+ref.Name)
		} else {
			kubeconfigs = append(kubeconfigs, string(secret.Data[ref.Key]))
			idf := providerIdentifier{
				kubeconfig:         string(secret.Data[ref.Key]),
				secretRefName:      ref.Name,
				secretRefNamespace: ref.Namespace,
			}
			for _, p := range binding.Status.Providers {
				if p.Kubeconfig.Namespace == ref.Namespace {
					idf.clusterUID = p.ClusterUID
				}
			}
			identifiers = append(identifiers, idf)
		}
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	ctrlContext, found := r.controllers[binding.Name]

	// stop existing with old kubeconfig
	if found && !reflect.DeepEqual(ctrlContext.kubeconfig, kubeconfigs) {
		logger.V(2).Info("stopping old Controller for APIServiceBinding", "apiservicebinding", binding.Namespace+"/"+binding.Name)
		ctrlContext.serviceBindings.Delete(binding.Name)
		if len(ctrlContext.serviceBindings) == 0 {
			ctrlContext.cancel()
		}
		delete(r.controllers, binding.Name)
	}

	// no need to start a new one
	if kubeconfigs == nil {
		return nil
	}

	// find existing with new kubeconfig
	// no need to match with the old controller context, create a new instead
	for _, ctrlContext := range r.controllers {
		if reflect.DeepEqual(ctrlContext.kubeconfig, kubeconfigs) {
			// add to it
			logger.V(2).Info("adding to existing Controller", "secret", binding.Namespace+"/"+binding.Name)
			r.controllers[binding.Name] = ctrlContext
			ctrlContext.serviceBindings.Insert(binding.Name)
			return nil
		}
	}

	var providerInfos []*konnectormodels.ProviderInfo
	for _, identifier := range identifiers {
		var provider konnectormodels.ProviderInfo

		// extract which namespace this kubeconfig points to
		cfg, err := clientcmd.Load([]byte(identifier.kubeconfig))
		if err != nil {
			logger.Error(err, "invalid kubeconfig in secret", "namespace", identifier.secretRefNamespace, "name", identifier.secretRefName)
			return nil // nothing we can do here. The APIServiceBinding Controller will set a condition
		}
		kubeContext, found := cfg.Contexts[cfg.CurrentContext]
		if !found {
			logger.Error(err, "kubeconfig in secret does not have a current context", "namespace", identifier.secretRefNamespace, "name", identifier.secretRefName)
			return nil // nothing we can do here. The APIServiceBinding Controller will set a condition
		}
		if kubeContext.Namespace == "" {
			logger.Error(err, "kubeconfig in secret does not have a namespace set for the current context", "namespace", identifier.secretRefNamespace, "name", identifier.secretRefName)
			return nil // nothing we can do here. The APIServiceBinding Controller will set a condition
		}
		provider.Namespace = kubeContext.Namespace
		provider.Config, err = clientcmd.RESTConfigFromKubeConfig([]byte(identifier.kubeconfig))
		if err != nil {
			logger.Error(err, "invalid kubeconfig in secret", "namespace", identifier.secretRefNamespace, "name", identifier.secretRefName)
			return nil // nothing we can do here. The APIServiceBinding Controller will set a condition
		}
		provider.ConsumerSecretRefKey = identifier.secretRefNamespace + "/" + identifier.secretRefName

		// set cluster uid
		//kubeclient, err := kubernetesclient.NewForConfig(provider.Config)
		//if err != nil {
		//	return err
		//}
		//ns, err := kubeclient.CoreV1().Namespaces().Get(ctx, namespaceKubeSystem, metav1.GetOptions{})
		//if err != nil {
		//	klog.Error(err.Error())
		//	return err
		//}
		//provider.ClusterID = string(ns.GetUID())

		provider.ClusterID = identifier.clusterUID

		providerInfos = append(providerInfos, &provider)
	}

	ctrlCtx, cancel := context.WithCancel(ctx)
	r.controllers[binding.Name] = &controllerContext{
		kubeconfig:      kubeconfigs,
		cancel:          cancel,
		serviceBindings: sets.New[string](binding.Name),
	}

	// create new because there is none yet for this kubeconfig
	logger.V(2).Info("starting new Controller", "binding", binding.Namespace+"/"+binding.Name)
	ctrl, err := r.newClusterController(
		providerInfos,
		func(svcBinding *kubebindv1alpha1.APIServiceBinding) bool {
			r.lock.RLock()
			defer r.lock.RUnlock()
			return r.controllers[binding.Name].serviceBindings.Has(svcBinding.Name)
		},
	)
	if err != nil {
		logger.Error(err, "failed to start new cluster Controller")
		return err
	}

	go ctrl.Start(ctrlCtx)

	return nil
}
