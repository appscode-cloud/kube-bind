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

package servicebinding

import (
	"context"

	conditionsapi "kmodules.xyz/client-go/api/v1"
	"kmodules.xyz/client-go/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/clientcmd"

	kubebindv1alpha1 "github.com/kube-bind/kube-bind/pkg/apis/kubebind/v1alpha1"
)

type reconciler struct {
	getConsumerSecret func(ns, name string) (*corev1.Secret, error)
}

func (r *reconciler) reconcile(ctx context.Context, binding *kubebindv1alpha1.APIServiceBinding) error {
	var errs []error

	if err := r.ensureValidKubeconfigSecret(ctx, binding); err != nil {
		errs = append(errs, err)
	}

	conditions.SetSummary(binding)

	return utilerrors.NewAggregate(errs)
}

func (r *reconciler) ensureValidKubeconfigSecret(ctx context.Context, binding *kubebindv1alpha1.APIServiceBinding) error {
	for _, ref := range binding.Spec.KubeconfigSecretRefs {
		secret, err := r.getConsumerSecret(ref.Namespace, ref.Name)
		if err != nil && !errors.IsNotFound(err) {
			return err
		} else if errors.IsNotFound(err) {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretNotFound",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s not found. Rerun kubectl bind for repair.",
				ref.Namespace, ref.Name,
			)
			return nil
		}

		kubeconfig, found := secret.Data[ref.Key]
		if !found {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretInvalid",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s is missing %q string key.",
				ref.Namespace,
				ref.Name,
				ref.Key,
			)
			return nil
		}

		cfg, err := clientcmd.Load(kubeconfig)
		if err != nil {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretInvalid",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s has an invalid kubeconfig: %v",
				ref.Namespace,
				ref.Name,
				err,
			)
			return nil
		}
		kubeContext, found := cfg.Contexts[cfg.CurrentContext]
		if !found {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretInvalid",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s has an invalid kubeconfig: current context %q not found",
				ref.Namespace,
				ref.Name,
				cfg.CurrentContext,
			)
			return nil
		}
		if kubeContext.Namespace == "" {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretInvalid",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s has an invalid kubeconfig: current context %q has no namespace set",
				ref.Namespace,
				ref.Name,
				cfg.CurrentContext,
			)
			return nil
		}
		if _, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig)); err != nil {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretInvalid",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s has an invalid kubeconfig: %v",
				ref.Namespace,
				ref.Name,
				err,
			)
			return nil
		}
	}

	conditions.MarkTrue(
		binding,
		kubebindv1alpha1.APIServiceBindingConditionSecretValid,
	)

	return nil
}
