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

	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/clientcmd"
	conditionsapi "kmodules.xyz/client-go/api/v1"
	"kmodules.xyz/client-go/conditions"
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
	for _, p := range binding.Spec.Providers {
		secret, err := r.getConsumerSecret(p.Kubeconfig.Namespace, p.Kubeconfig.Name)
		if err != nil && !errors.IsNotFound(err) {
			return err
		} else if errors.IsNotFound(err) {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretNotFound",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s not found. Rerun kubectl bind for repair.",
				p.Kubeconfig.Namespace, p.Kubeconfig.Name,
			)
			return nil
		}

		kubeconfig, found := secret.Data[p.Kubeconfig.Key]
		if !found {
			conditions.MarkFalse(
				binding,
				kubebindv1alpha1.APIServiceBindingConditionSecretValid,
				"KubeconfigSecretInvalid",
				conditionsapi.ConditionSeverityError,
				"Kubeconfig secret %s/%s is missing %q string key.",
				p.Kubeconfig.Namespace,
				p.Kubeconfig.Name,
				p.Kubeconfig.Key,
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
				p.Kubeconfig.Namespace,
				p.Kubeconfig.Name,
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
				p.Kubeconfig.Namespace,
				p.Kubeconfig.Name,
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
				p.Kubeconfig.Namespace,
				p.Kubeconfig.Name,
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
				p.Kubeconfig.Namespace,
				p.Kubeconfig.Name,
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
