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

package status

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	kmc "kmodules.xyz/client-go/client"

	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubebindv1alpha1 "github.com/kube-bind/kube-bind/pkg/apis/kubebind/v1alpha1"
)

type reconciler struct {
	getServiceNamespace func(upstreamNamespace string) (*kubebindv1alpha1.APIServiceNamespace, error)

	getConsumerObject            func(ns, name string) (*unstructured.Unstructured, error)
	updateConsumerObjectStatus   func(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	updateConsumerObject         func(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	createConsumerObject         func(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	findConnectedObject          func(ctx context.Context, obj *unstructured.Unstructured) ([]*unstructured.Unstructured, error)
	getConnectedObject           func(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	createOrPatchConnectedObject func(ctx context.Context, obj *unstructured.Unstructured, transformFunc kmc.TransformFunc) error
	patchConnectedObjectStatus   func(ctx context.Context, obj *unstructured.Unstructured) error
	userConfigurable             func() bool

	deleteProviderObject func(ctx context.Context, ns, name string) error
}

// reconcile syncs upstream status to consumer objects.
func (r *reconciler) reconcile(ctx context.Context, obj *unstructured.Unstructured) error {

	logger := klog.FromContext(ctx)
	klog.Info("Reconciling:", obj.GetNamespace(), "/", obj.GetName())

	obj = obj.DeepCopy()

	ns := obj.GetNamespace()
	if ns != "" {
		sn, err := r.getServiceNamespace(ns)
		if err != nil && !kerr.IsNotFound(err) {
			return err
		} else if kerr.IsNotFound(err) {
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

	if !r.userConfigurable() {
		downstream, err := r.getConsumerObject(ns, obj.GetName())
		if err == nil {
			downstreamSpec, _, err := unstructured.NestedFieldNoCopy(downstream.Object, "spec")
			if err != nil {
				logger.Error(err, "failed to get downstream spec")
				return nil
			}
			upstreamSpec, foundUpstreamSpec, err := unstructured.NestedFieldNoCopy(obj.Object, "spec")
			if err != nil {
				logger.Error(err, "failed to get upstream spec")
				return nil
			}

			if foundUpstreamSpec && !reflect.DeepEqual(downstreamSpec, upstreamSpec) {
				if err := unstructured.SetNestedField(downstream.Object, upstreamSpec, "spec"); err != nil {
					bs, err := json.Marshal(upstreamSpec)
					if err != nil {
						logger.Error(err, "failed to marshal downstream spec", "spec", fmt.Sprintf("%s", downstreamSpec))
						return nil // nothing we can do
					}
					logger.Error(err, "failed to set spec", "spec", string(bs))
					return nil // nothing we can do
				}
				if _, er := r.updateConsumerObject(ctx, downstream); er != nil {
					return er
				}
			}

			// upstream status sync with downstream status

			downstreamStatus, _, err := unstructured.NestedFieldNoCopy(downstream.Object, "status")
			if err != nil {
				logger.Error(err, "failed to get downstream status")
				return nil
			}
			upstreamStatus, foundUpstreamStatus, err := unstructured.NestedFieldNoCopy(obj.Object, "status")
			if err != nil {
				logger.Error(err, "failed to get upstream status")
				return nil
			}

			if foundUpstreamStatus && !reflect.DeepEqual(downstreamStatus, upstreamStatus) {
				if err := unstructured.SetNestedField(downstream.Object, upstreamStatus, "status"); err != nil {
					bs, err := json.Marshal(upstreamStatus)
					if err != nil {
						logger.Error(err, "failed to marshal upstream status", "status", fmt.Sprintf("%s", upstreamStatus))
						return nil // nothing we can do
					}
					logger.Error(err, "failed to set upstream status", "status", string(bs))
					return nil // nothing we can do
				}
				if _, er := r.updateConsumerObjectStatus(ctx, downstream); er != nil {
					klog.Error("failed to update consumer object status")
					return er
				}
			}
		} else if kerr.IsNotFound(err) {
			upstream := obj.DeepCopy()
			upstream.SetUID("")
			upstream.SetResourceVersion("")
			upstream.SetNamespace(ns)
			upstream.SetManagedFields(nil)
			upstream.SetDeletionTimestamp(nil)
			upstream.SetDeletionGracePeriodSeconds(nil)
			upstream.SetOwnerReferences(nil)
			upstream.SetFinalizers(nil)
			if _, er := r.createConsumerObject(ctx, upstream); er != nil {
				return er
			}
		} else {
			logger.Error(err, "failed to get downstream consumer object")
			return err
		}

		objList, err := r.findConnectedObject(ctx, obj.DeepCopy())
		if err != nil {
			klog.Error("failed to find connected object")
			return err
		}

		for _, o := range objList {
			if err := r.createOrUpdateConsumerObject(context.TODO(), o.DeepCopy()); err != nil {
				klog.Error("failed to create/update consumer object")
				return err
			}
		}

		return nil
	}

	downstream, err := r.getConsumerObject(ns, obj.GetName())
	if err != nil && !kerr.IsNotFound(err) {
		logger.Info("failed to get downstream object", "error", err, "downstreamNamespace", ns, "downstreamName", obj.GetName())
		return err
	} else if kerr.IsNotFound(err) {
		// downstream is gone. Delete upstream too. Note that we cannot rely on the spec controller because
		// due to konnector restart it might have missed the deletion event.
		logger.Info("Deleting upstream object because downstream is gone", "downstreamNamespace", ns, "downstreamName", obj.GetName())
		if err := r.deleteProviderObject(ctx, obj.GetNamespace(), obj.GetName()); err != nil {
			return err
		}
		return nil
	}

	orig := downstream
	downstream = downstream.DeepCopy()
	status, found, err := unstructured.NestedFieldNoCopy(obj.Object, "status")
	if err != nil {
		runtime.HandleError(err)
		return nil // nothing we can do here
	}
	if found {
		if err := unstructured.SetNestedField(downstream.Object, status, "status"); err != nil {
			runtime.HandleError(err)
			return nil // nothing we can do here
		}
	} else {
		unstructured.RemoveNestedField(downstream.Object, "status")
	}
	if !reflect.DeepEqual(orig, downstream) {
		logger.Info("Updating downstream object status", "downstreamNamespace", ns, "downstreamName", obj.GetName())
		if _, err := r.updateConsumerObjectStatus(ctx, downstream); err != nil {
			return err
		}
	}

	return nil
}

func (r *reconciler) createOrUpdateConsumerObject(ctx context.Context, obj *unstructured.Unstructured) error {
	curObj, err := r.getConnectedObject(ctx, obj.DeepCopy())
	if err == nil {
		data, dataFound, err := unstructured.NestedFieldCopy(obj.Object, "data")
		if err != nil {
			return err
		}
		if dataFound {
			if err := unstructured.SetNestedField(curObj.Object, data, "data"); err != nil {
				return err
			}
		}

		spec, specFound, err := unstructured.NestedFieldCopy(obj.Object, "spec")
		if err != nil {
			return err
		}
		if specFound {
			if err := unstructured.SetNestedField(curObj.Object, spec, "spec"); err != nil {
				return err
			}
		}

		curObj.SetLabels(obj.GetLabels())
		curObj.SetAnnotations(obj.GetAnnotations())

		return r.createOrPatchConnectedObject(ctx, curObj.DeepCopy(), func(ob client.Object, createOp bool) client.Object {
			return curObj
		})
	} else if kerr.IsNotFound(err) {
		return r.createOrPatchConnectedObject(ctx, obj, func(_ client.Object, _ bool) client.Object {
			obj.SetOwnerReferences(nil)
			obj.SetUID("")
			obj.SetResourceVersion("")
			return obj
		})
	} else {
		klog.Error(err, "failed to get connected object")
		return err
	}
}
