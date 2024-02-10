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

package serviceexport

import (
	"context"
	"sync"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	dynamicclient "k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	kubewarev1alpha1 "go.kubeware.dev/kubeware/pkg/apis/kubeware/v1alpha1"
	"go.kubeware.dev/kubeware/pkg/konnector/controllers/cluster/serviceexport/multinsinformer"
	"go.kubeware.dev/kubeware/pkg/konnector/controllers/cluster/serviceexport/spec"
	"go.kubeware.dev/kubeware/pkg/konnector/controllers/cluster/serviceexport/status"
	konnectormodels "go.kubeware.dev/kubeware/pkg/konnector/models"
	conditionsapi "kmodules.xyz/client-go/api/v1"
	"kmodules.xyz/client-go/conditions"
)

type syncInfo struct {
	clusterID    string
	exporterName string
}

type reconciler struct {
	consumerConfig *rest.Config
	lock           sync.Mutex
	syncContext    map[syncInfo]syncContext // by ClusterID and CRD name

	providerInfos []*konnectormodels.ProviderInfo

	getCRD            func(name string) (*apiextensionsv1.CustomResourceDefinition, error)
	getServiceBinding func(name string) (*kubewarev1alpha1.APIServiceBinding, error)
}

type syncContext struct {
	generation int64
	cancel     func()
}

func (r *reconciler) reconcile(ctx context.Context, sync *syncInfo, export *kubewarev1alpha1.APIServiceExport) error {
	errs := []error{}

	if err := r.ensureControllers(ctx, sync, export); err != nil {
		errs = append(errs, err)
	}

	if export != nil {
		if err := r.ensureServiceBindingConditionCopied(export); err != nil {
			errs = append(errs, err)
		}
		if err := r.ensureCRDConditionsCopied(export); err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (r *reconciler) ensureControllers(ctx context.Context, sync *syncInfo, export *kubewarev1alpha1.APIServiceExport) error {
	logger := klog.FromContext(ctx)

	if export == nil {
		// stop dangling syncers on delete
		r.lock.Lock()
		defer r.lock.Unlock()
		if c, found := r.syncContext[*sync]; found {
			logger.V(1).Info("Stopping APIServiceExport sync", "reason", "APIServiceExport deleted")
			c.cancel()
			delete(r.syncContext, *sync)
		}
		return nil
	}

	var errs []error
	crd, err := r.getCRD(export.Name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		// stop it
		r.lock.Lock()
		defer r.lock.Unlock()
		if c, found := r.syncContext[syncInfo{
			clusterID:    sync.clusterID,
			exporterName: export.Name,
		}]; found {
			logger.V(1).Info("Stopping APIServiceExport sync", "reason", "NoCustomResourceDefinition")
			c.cancel()
			delete(r.syncContext, syncInfo{
				clusterID:    sync.clusterID,
				exporterName: export.Name,
			})
		}

		return nil
	}

	// any binding that references this resource?
	binding, err := r.getServiceBinding(export.Name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	if binding == nil {
		// stop it
		r.lock.Lock()
		defer r.lock.Unlock()
		if c, found := r.syncContext[syncInfo{
			clusterID:    sync.clusterID,
			exporterName: export.Name,
		}]; found {
			logger.V(1).Info("Stopping APIServiceExport sync", "reason", "NoAPIServiceBinding")
			c.cancel()
			delete(r.syncContext, syncInfo{
				clusterID:    sync.clusterID,
				exporterName: export.Name,
			})
		}

		return nil
	}

	r.lock.Lock()
	c, found := r.syncContext[syncInfo{
		clusterID:    sync.clusterID,
		exporterName: export.Name,
	}]
	if found {
		if c.generation == export.Generation {
			r.lock.Unlock()
			return nil // all as expected
		}

		// technically, we could be less aggressive here if nothing big changed in the resource, e.g. just schemas. But ¯\_(ツ)_/¯

		logger.V(1).Info("Stopping APIServiceExport sync", "reason", "GenerationChanged", "generation", export.Generation)
		c.cancel()
		delete(r.syncContext, syncInfo{
			clusterID:    sync.clusterID,
			exporterName: export.Name,
		})
	}
	r.lock.Unlock()

	// start a new syncer

	var syncVersion string
	for _, v := range export.Spec.Versions {
		if v.Served {
			syncVersion = v.Name
			break
		}
	}
	gvr := runtimeschema.GroupVersionResource{Group: export.Spec.Group, Version: syncVersion, Resource: export.Spec.Names.Plural}

	dynamicConsumerClient := dynamicclient.NewForConfigOrDie(r.consumerConfig)
	consumerInf := dynamicinformer.NewDynamicSharedInformerFactory(dynamicConsumerClient, time.Minute*30)

	for _, provider := range r.providerInfos {
		dynamicProviderClient := dynamicclient.NewForConfigOrDie(provider.Config)

		if pns, err := dynamicProviderClient.Resource(runtimeschema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "namespaces",
		}).Get(ctx, provider.Namespace, metav1.GetOptions{}); err != nil {
			return err
		} else {
			provider.NamespaceUID = string(pns.GetUID())
		}

		if crd.Spec.Scope == apiextensionsv1.ClusterScoped || export.Spec.InformerScope == kubewarev1alpha1.ClusterScope {
			factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicProviderClient, time.Minute*30)
			factory.ForResource(gvr).Lister() // wire the GVR up in the informer factory
			provider.ProviderDynamicInformer = multinsinformer.GetterInformerWrapper{
				GVR:      gvr,
				Delegate: factory,
			}
		} else {
			provider.ProviderDynamicInformer, err = multinsinformer.NewDynamicMultiNamespaceInformer(
				gvr,
				provider.Namespace,
				provider.Config,
				provider.DynamicServiceNamespaceInformer,
			)
			if err != nil {
				return err
			}
		}
	}

	specCtrl, err := spec.NewController(
		gvr,
		r.consumerConfig,
		consumerInf.ForResource(gvr),
		r.providerInfos,
	)
	if err != nil {
		runtime.HandleError(err)
		return nil // nothing we can do here
	}
	statusCtrl, err := status.NewController(
		gvr,
		r.consumerConfig,
		consumerInf.ForResource(gvr),
		r.providerInfos,
	)
	if err != nil {
		runtime.HandleError(err)
		return nil // nothing we can do here
	}

	ctx, cancel := context.WithCancel(ctx)

	consumerInf.Start(ctx.Done())
	for _, provider := range r.providerInfos {
		provider.ProviderDynamicInformer.Start(ctx)
	}

	go func() {
		// to not block the main thread
		consumerSynced := consumerInf.WaitForCacheSync(ctx.Done())
		logger.V(2).Info("Synced informers", "consumer", consumerSynced)

		for _, provider := range r.providerInfos {
			providerSynced := provider.ProviderDynamicInformer.WaitForCacheSync(ctx.Done())
			logger.V(2).Info("Synced informers", "provider", providerSynced)
		}

		go specCtrl.Start(ctx, 1)
		go statusCtrl.Start(ctx, 1)
	}()

	r.lock.Lock()
	defer r.lock.Unlock()
	if c, found := r.syncContext[syncInfo{
		clusterID:    sync.clusterID,
		exporterName: export.Name,
	}]; found {
		c.cancel()
	}
	r.syncContext[syncInfo{
		clusterID:    sync.clusterID,
		exporterName: export.Name,
	}] = syncContext{
		generation: export.Generation,
		cancel:     cancel,
	}

	return utilerrors.NewAggregate(errs)
}

func (r *reconciler) ensureServiceBindingConditionCopied(export *kubewarev1alpha1.APIServiceExport) error {
	binding, err := r.getServiceBinding(export.Name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		conditions.MarkFalse(
			export,
			kubewarev1alpha1.APIServiceExportConditionConnected,
			"APIServiceBindingNotFound",
			conditionsapi.ConditionSeverityInfo,
			"No APIServiceBinding exists.",
		)

		conditions.MarkFalse(
			export,
			kubewarev1alpha1.APIServiceExportConditionConsumerInSync,
			"NA",
			conditionsapi.ConditionSeverityInfo,
			"No APIServiceBinding exists.",
		)

		return nil
	}

	conditions.MarkTrue(export, kubewarev1alpha1.APIServiceExportConditionConnected)

	if inSync := conditions.Get(binding, kubewarev1alpha1.APIServiceBindingConditionSchemaInSync); inSync != nil {
		inSync := inSync.DeepCopy()
		inSync.Type = kubewarev1alpha1.APIServiceExportConditionConsumerInSync
		conditions.Set(export, inSync)
	} else {
		conditions.MarkFalse(
			export,
			kubewarev1alpha1.APIServiceExportConditionConsumerInSync,
			"Unknown",
			conditionsapi.ConditionSeverityInfo,
			"APIServiceBinding %s in the consumer cluster does not have a SchemaInSync condition.",
			binding.Name,
		)
	}

	return nil
}

func (r *reconciler) ensureCRDConditionsCopied(export *kubewarev1alpha1.APIServiceExport) error {
	crd, err := r.getCRD(export.Name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		return nil //nothing to copy.
	}

	exportIndex := map[conditionsapi.ConditionType]int{}
	for i, c := range export.Status.Conditions {
		exportIndex[c.Type] = i
	}
	for _, c := range crd.Status.Conditions {
		if conditionsapi.ConditionType(c.Type) == conditionsapi.ReadyCondition {
			continue
		}

		severity := conditionsapi.ConditionSeverityError
		if c.Status == apiextensionsv1.ConditionTrue {
			severity = conditionsapi.ConditionSeverityNone
		}
		copied := conditionsapi.Condition{
			Type:               conditionsapi.ConditionType(c.Type),
			Status:             metav1.ConditionStatus(c.Status),
			Severity:           severity, // CRD conditions have no severity
			LastTransitionTime: c.LastTransitionTime,
			Reason:             c.Reason,
			Message:            c.Message,
		}

		// update or append
		if i, found := exportIndex[conditionsapi.ConditionType(c.Type)]; found {
			export.Status.Conditions[i] = copied
		} else {
			export.Status.Conditions = append(export.Status.Conditions, copied)
		}
	}
	conditions.SetSummary(export)

	export.Status.AcceptedNames = crd.Status.AcceptedNames
	export.Status.StoredVersions = crd.Status.StoredVersions

	return nil
}
