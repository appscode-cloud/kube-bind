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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	componentbaseversion "k8s.io/component-base/version"
	"k8s.io/klog/v2"

	kubewarev1alpha1 "go.kubeware.dev/kubeware/pkg/apis/kubeware/v1alpha1"
	conditionsapi "go.kubeware.dev/kubeware/pkg/apis/third_party/conditions/apis/conditions/v1alpha1"
	"go.kubeware.dev/kubeware/pkg/apis/third_party/conditions/util/conditions"
	konnectormodels "go.kubeware.dev/kubeware/pkg/konnector/models"
	"go.kubeware.dev/kubeware/pkg/version"
)

type reconciler struct {
	heartbeatInterval time.Duration

	getProviderSecret    func(porvider *konnectormodels.ProviderInfo) (*corev1.Secret, error)
	getConsumerSecret    func(provider *konnectormodels.ProviderInfo) (*corev1.Secret, error)
	updateConsumerSecret func(ctx context.Context, secret *corev1.Secret) (*corev1.Secret, error)
	getProviderInfo      func(clusterID string) (*konnectormodels.ProviderInfo, error)
	createConsumerSecret func(ctx context.Context, secret *corev1.Secret) (*corev1.Secret, error)
	providerInfos        []*konnectormodels.ProviderInfo
}

func (r *reconciler) reconcile(ctx context.Context, binding *kubewarev1alpha1.ClusterBinding) error {
	var errs []error

	provider, err := konnectormodels.GetProviderInfoWithProviderNamespace(r.providerInfos, binding.Namespace)
	if err != nil {
		return err
	}

	if err := r.ensureConsumerSecret(ctx, binding, provider); err != nil {
		errs = append(errs, err)
	}

	if err := r.ensureHeartbeat(ctx, binding); err != nil {
		errs = append(errs, err)
	}

	if err := r.ensureKonnectorVersion(ctx, binding); err != nil {
		errs = append(errs, err)
	}

	conditions.SetSummary(binding)

	return utilerrors.NewAggregate(errs)
}

func (r *reconciler) ensureHeartbeat(ctx context.Context, binding *kubewarev1alpha1.ClusterBinding) error {
	binding.Status.HeartbeatInterval.Duration = r.heartbeatInterval
	if now := time.Now(); binding.Status.LastHeartbeatTime.IsZero() || now.After(binding.Status.LastHeartbeatTime.Add(r.heartbeatInterval/2)) {
		binding.Status.LastHeartbeatTime.Time = now
	}

	return nil
}

func (r *reconciler) ensureConsumerSecret(ctx context.Context, binding *kubewarev1alpha1.ClusterBinding, provider *konnectormodels.ProviderInfo) error {
	logger := klog.FromContext(ctx)

	providerSecret, err := r.getProviderSecret(provider)
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		conditions.MarkFalse(
			binding,
			kubewarev1alpha1.ClusterBindingConditionSecretValid,
			"ProviderSecretNotFound",
			conditionsapi.ConditionSeverityWarning,
			"Provider secret %s/%s not found",
			binding.Namespace, binding.Spec.KubeconfigSecretRef.Name,
		)
		return nil
	}

	if _, found := providerSecret.Data[binding.Spec.KubeconfigSecretRef.Key]; !found {
		conditions.MarkFalse(
			binding,
			kubewarev1alpha1.ClusterBindingConditionSecretValid,
			"ProviderSecretInvalid",
			conditionsapi.ConditionSeverityWarning,
			"Provider secret %s/%s is missing %q string key.",
			provider.Namespace,
			binding.Spec.KubeconfigSecretRef.Name,
			binding.Spec.KubeconfigSecretRef.Key,
		)
		return nil
	}

	consumerSecret, err := r.getConsumerSecret(provider)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if consumerSecret == nil {
		ns, name, err := cache.SplitMetaNamespaceKey(provider.ConsumerSecretRefKey)
		if err != nil {
			return err
		}
		consumerSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ns,
				Namespace: name,
			},
			Data: providerSecret.Data,
			Type: providerSecret.Type,
		}
		logger.V(2).Info("Creating consumer secret", "namespace", ns, "name", name)
		if _, err := r.createConsumerSecret(ctx, &consumerSecret); err != nil {
			return err
		}
	} else {
		consumerSecret.Data = providerSecret.Data
		consumerSecret.Type = providerSecret.Type

		logger.V(2).Info("Updating consumer secret", "namespace", consumerSecret.Namespace, "name", consumerSecret.Name)
		if _, err := r.updateConsumerSecret(ctx, consumerSecret); err != nil {
			return err
		}

		// TODO: create events
	}

	conditions.MarkTrue(
		binding,
		kubewarev1alpha1.ClusterBindingConditionSecretValid,
	)

	return nil
}

func (r *reconciler) ensureKonnectorVersion(ctx context.Context, binding *kubewarev1alpha1.ClusterBinding) error {
	gitVersion := componentbaseversion.Get().GitVersion
	ver, err := version.BinaryVersion(gitVersion)
	if err != nil {
		binding.Status.KonnectorVersion = "unknown"

		conditions.MarkFalse(
			binding,
			kubewarev1alpha1.ClusterBindingConditionValidVersion,
			"ParseError",
			conditionsapi.ConditionSeverityWarning,
			"Konnector binary version string %q cannot be parsed: %v",
			componentbaseversion.Get().GitVersion,
			err,
		)
		return nil
	}

	binding.Status.KonnectorVersion = ver

	conditions.MarkTrue(
		binding,
		kubewarev1alpha1.ClusterBindingConditionValidVersion,
	)

	return nil
}
