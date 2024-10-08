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

package spec

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	konnectormodels "go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

type reconciler struct {
	getProviderInfo        func(obj *unstructured.Unstructured) (*konnectormodels.ProviderInfo, error)
	getServiceNamespace    func(provider *konnectormodels.ProviderInfo, name string) (*v1alpha1.APIServiceNamespace, error)
	createServiceNamespace func(ctx context.Context, provider *konnectormodels.ProviderInfo, sn *v1alpha1.APIServiceNamespace) (*v1alpha1.APIServiceNamespace, error)

	getProviderObject    func(provider *konnectormodels.ProviderInfo, ns, name string) (*unstructured.Unstructured, error)
	createProviderObject func(ctx context.Context, provider *konnectormodels.ProviderInfo, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	updateProviderObject func(ctx context.Context, provider *konnectormodels.ProviderInfo, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	deleteProviderObject func(ctx context.Context, provider *konnectormodels.ProviderInfo, ns, name string) error

	updateConsumerObject func(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)

	requeue func(obj *unstructured.Unstructured, after time.Duration) error
}

// reconcile syncs downstream objects (metadata and spec) with upstream objects.
func (r *reconciler) reconcile(ctx context.Context, obj *unstructured.Unstructured) error {
	logger := klog.FromContext(ctx)

	provider, err := r.getProviderInfo(obj)
	if err != nil {
		klog.Errorf("failed to get provider information: %s", err)
		return err
	}

	klog.Infof("reconciling object %s/%s for provider %s", obj.GetNamespace(), obj.GetName(), provider.ClusterID)

	ns := obj.GetNamespace()
	if ns != "" {
		sn, err := r.getServiceNamespace(provider, ns)
		if err != nil && !errors.IsNotFound(err) {
			return err
		} else if errors.IsNotFound(err) {
			logger.V(1).Info("creating APIServiceNamespace", "namespace", ns)
			sn, err = r.createServiceNamespace(ctx, provider, &v1alpha1.APIServiceNamespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ns,
					Namespace: provider.Namespace,
				},
			})
			if err != nil {
				return err
			}
		}
		if sn.Status.Namespace == "" {
			// note: the service provider might implement this synchronously in admission. if so, we can skip the requeue.
			logger.V(1).Info("waiting for APIServiceNamespace to be ready", "namespace", ns)
			return r.requeue(obj, 1*time.Second)
		}

		logger = logger.WithValues("upstreamNamespace", sn.Status.Namespace)
		ctx = klog.NewContext(ctx, logger)

		// continue with upstream namespace
		ns = sn.Status.Namespace
	}

	upstream, err := r.getProviderObject(provider, ns, obj.GetName())
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		if obj.GetDeletionTimestamp() != nil && !obj.GetDeletionTimestamp().IsZero() {
			logger.V(2).Info("object is already deleting, don't sync")

			if _, err := r.removeDownstreamFinalizer(ctx, obj); err != nil {
				return err
			}

			return nil
		}

		if obj, err = r.ensureDownstreamFinalizer(ctx, obj); err != nil {
			klog.Errorln(err)
			return err
		}

		// clean up object
		upstream = obj.DeepCopy()
		upstream.SetUID("")
		upstream.SetResourceVersion("")
		upstream.SetNamespace(ns)
		upstream.SetManagedFields(nil)
		upstream.SetDeletionTimestamp(nil)
		upstream.SetDeletionGracePeriodSeconds(nil)
		upstream.SetOwnerReferences(nil)
		upstream.SetFinalizers(nil)
		unstructured.RemoveNestedField(upstream.Object, "status")

		logger.Info("Creating upstream object")
		if _, err := r.createProviderObject(ctx, provider, upstream); err != nil && !errors.IsAlreadyExists(err) {
			return err
		} else if errors.IsAlreadyExists(err) {
			logger.Info("Upstream object already exists. Waiting for requeue.") // the upstream object will lead to a requeue
		}
	}

	// here the upstream already exists. Update everything but the status.

	if obj.GetDeletionTimestamp() != nil && !obj.GetDeletionTimestamp().IsZero() {
		if upstream.GetDeletionTimestamp() != nil && !upstream.GetDeletionTimestamp().IsZero() {
			logger.V(2).Info("upstream is already deleting, wait for it")
			return nil // we will get an event when the upstream is deleted
		}

		logger.V(1).Info("object is already deleting downstream, deleting upstream too")
		if err := r.deleteProviderObject(ctx, provider, ns, obj.GetName()); err != nil && !errors.IsNotFound(err) {
			return err
		}

		if _, err := r.removeDownstreamFinalizer(ctx, obj); err != nil {
			return err
		}

		logger.V(2).Info("upstream deleted, finalizer removed in downstream, waiting for downstream deletion to finish")
		return nil // we will get an event when the upstream is deleted
	}

	// just in case, checking for finalizer
	if obj, err = r.ensureDownstreamFinalizer(ctx, obj); err != nil {
		klog.Errorln(err)
		return err
	}

	downstreamSpec, foundDownstreamSpec, err := unstructured.NestedFieldNoCopy(obj.Object, "spec")
	if err != nil {
		logger.Error(err, "failed to get downstream spec")
		return nil
	}
	upstreamSpec, _, err := unstructured.NestedFieldNoCopy(upstream.Object, "spec")
	if err != nil {
		logger.Error(err, "failed to get upstream spec")
		return nil
	}
	if reflect.DeepEqual(downstreamSpec, upstreamSpec) {
		return nil // nothing to do
	}

	upstream = upstream.DeepCopy()
	if foundDownstreamSpec {
		if err := unstructured.SetNestedField(upstream.Object, downstreamSpec, "spec"); err != nil {
			bs, err := json.Marshal(downstreamSpec)
			if err != nil {
				logger.Error(err, "failed to marshal downstream spec", "spec", fmt.Sprintf("%s", downstreamSpec))
				return nil // nothing we can do
			}
			logger.Error(err, "failed to set spec", "spec", string(bs))
			return nil // nothing we can do
		}
	} else {
		unstructured.RemoveNestedField(upstream.Object, "spec")
	}

	logger.Info("Updating upstream object")
	upstream.SetManagedFields(nil) // server side apply does not want this
	if _, err := r.updateProviderObject(ctx, provider, upstream); err != nil {
		return err
	}

	return nil
}

func (r *reconciler) ensureDownstreamFinalizer(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	logger := klog.FromContext(ctx)

	// check that downstream has our finalizer
	found := false
	for _, f := range obj.GetFinalizers() {
		if f == v1alpha1.DownstreamFinalizer {
			found = true
			break
		}
	}

	if !found {
		logger.V(2).Info("adding finalizer to downstream object")
		obj = obj.DeepCopy()
		obj.SetFinalizers(append(obj.GetFinalizers(), v1alpha1.DownstreamFinalizer))
		var err error
		if obj, err = r.updateConsumerObject(ctx, obj); err != nil {
			return nil, err
		}
	}

	return obj, nil
}

func (r *reconciler) removeDownstreamFinalizer(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	logger := klog.FromContext(ctx)

	var finalizers []string
	found := false
	for _, f := range obj.GetFinalizers() {
		if f == v1alpha1.DownstreamFinalizer {
			found = true
			continue
		}
		finalizers = append(finalizers, f)
	}

	if found {
		logger.V(2).Info("removing finalizer from downstream object")
		obj = obj.DeepCopy()
		obj.SetFinalizers(finalizers)
		var err error
		if obj, err = r.updateConsumerObject(ctx, obj); err != nil {
			return nil, err
		}
	}

	return obj, nil
}
