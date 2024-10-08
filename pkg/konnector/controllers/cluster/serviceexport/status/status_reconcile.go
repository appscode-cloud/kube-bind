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

package status

import (
	"context"
	"reflect"

	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	konnectormodels "go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

type reconciler struct {
	getProviderInfo func(obj *unstructured.Unstructured) (*konnectormodels.ProviderInfo, error)

	getServiceNamespace func(provider *konnectormodels.ProviderInfo, upstreamNamespace string) (*kubebindv1alpha1.APIServiceNamespace, error)

	getConsumerObject          func(provider *konnectormodels.ProviderInfo, ns, name string) (*unstructured.Unstructured, error)
	updateConsumerObjectStatus func(ctx context.Context, provider *konnectormodels.ProviderInfo, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)

	ensureStatusSecret func(ctx context.Context, provider *konnectormodels.ProviderInfo, upstream, downstream *unstructured.Unstructured, status interface{}, providerNS string) (string, error)

	deleteProviderObject func(ctx context.Context, provider *konnectormodels.ProviderInfo, ns, name string) error
}

// reconcile syncs upstream status to consumer objects.
func (r *reconciler) reconcile(ctx context.Context, obj *unstructured.Unstructured) error {
	logger := klog.FromContext(ctx)

	provider, err := r.getProviderInfo(obj)
	if err != nil {
		klog.ErrorS(err, "failed to get provider information")
		return err
	}

	ns := obj.GetNamespace()
	if ns != "" {
		sn, err := r.getServiceNamespace(provider, ns)
		if err != nil && !errors.IsNotFound(err) {
			return err
		} else if errors.IsNotFound(err) {
			runtime.HandleError(err)
			return err // hoping the APIServiceNamespace will be created soon. Otherwise, this item goes into backoff.
		}
		if sn.Status.Namespace == "" {
			runtime.HandleError(err)
			return err // hoping the status is set soon.
		}

		logger = logger.WithValues("upstreamNamespace", sn.Status.Namespace)
		ctx = klog.NewContext(ctx, logger)

		// continue with downstream namespace
		ns = sn.Name
	}

	downstream, err := r.getConsumerObject(provider, ns, obj.GetName())
	if err != nil && !errors.IsNotFound(err) {
		logger.Info("failed to get downstream object", "error", err, "downstreamNamespace", ns, "downstreamName", obj.GetName())
		return err
	} else if errors.IsNotFound(err) {
		// downstream is gone. Delete upstream too. Note that we cannot rely on the spec controller because
		// due to konnector restart it might have missed the deletion event.
		logger.Info("Deleting upstream object because downstream is gone", "downstreamNamespace", ns, "downstreamName", obj.GetName())
		if err := r.deleteProviderObject(ctx, provider, obj.GetNamespace(), obj.GetName()); err != nil {
			return err
		}
		return nil
	}

	orig := downstream
	downstream = downstream.DeepCopy()
	newStatus, found, err := unstructured.NestedFieldNoCopy(obj.Object, "status")
	if err != nil {
		runtime.HandleError(err)
		return nil // nothing we can do here
	}
	if found {
		newSecretRefName, err := r.ensureStatusSecret(ctx, provider, obj.DeepCopy(), downstream, newStatus, ns)
		if err != nil {
			klog.Errorln(err)
			return err
		}
		if err := unstructured.SetNestedField(downstream.Object, newStatus, "status"); err != nil {
			runtime.HandleError(err)
			klog.Errorln(err)
			return nil // nothing we can do here
		}
		if newSecretRefName != "" {
			if err = unstructured.SetNestedField(downstream.Object, newSecretRefName, "status", "secretRef", "name"); err != nil {
				klog.Errorln(err)
				return err
			}
		}
	} else {
		unstructured.RemoveNestedField(downstream.Object, "status")
	}
	if !reflect.DeepEqual(orig, downstream) {
		logger.Info("Updating downstream object status")
		if _, err := r.updateConsumerObjectStatus(ctx, provider, downstream); err != nil {
			return err
		}
	}

	return nil
}
