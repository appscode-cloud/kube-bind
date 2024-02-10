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

package plugin

import (
	"context"
	"fmt"
	"time"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	kubewarev1alpha1 "go.kubeware.dev/kubeware/pkg/apis/kubeware/v1alpha1"
	"go.kubeware.dev/kubeware/pkg/apis/kubeware/v1alpha1/helpers"
	bindclient "go.kubeware.dev/kubeware/pkg/client/clientset/versioned"
	conditionsapi "kmodules.xyz/client-go/api/v1"
	"kmodules.xyz/client-go/conditions"
)

const (
	kubeconfigSecretNamespace = "kubeware"
)

func (b *BindAPIServiceOptions) createAPIServiceBindings(ctx context.Context, config *rest.Config, request *kubewarev1alpha1.APIServiceExportRequest, secretName string) ([]*kubewarev1alpha1.APIServiceBinding, error) {
	bindClient, err := bindclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	apiextensionsClient, err := apiextensionsclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	var bindings []*kubewarev1alpha1.APIServiceBinding
	for _, resource := range request.Spec.Resources {
		name := resource.Resource + "." + resource.Group
		existing, err := bindClient.KubeBindV1alpha1().APIServiceBindings().Get(ctx, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		} else if err == nil {
			//if existing.Spec.KubeconfigSecretRef.Namespace != "kubeware" || existing.Spec.KubeconfigSecretRef.Name != secretName {
			//	return nil, fmt.Errorf("found existing APIServiceBinding %s not from this service provider", name)
			//}

			hasSecret := false
			for _, secRef := range existing.Spec.KubeconfigSecretRefs {
				if secRef.Namespace == kubeconfigSecretNamespace && secRef.Name == secretName {
					hasSecret = true
					fmt.Fprintf(b.Options.IOStreams.ErrOut, "✅ Existing APIServiceBinding \"%s\" already has the secret \"%s\".\n", existing.Name, secretName) // nolint: errcheck
					break
				}
			}
			if hasSecret {
				continue
			}

			fmt.Fprintf(b.Options.IOStreams.ErrOut, "✅ Updating existing APIServiceBinding %s.\n", existing.Name) // nolint: errcheck

			existing.Spec.KubeconfigSecretRefs = append(existing.Spec.KubeconfigSecretRefs, kubewarev1alpha1.ClusterSecretKeyRef{
				LocalSecretKeyRef: kubewarev1alpha1.LocalSecretKeyRef{
					Name: secretName,
					Key:  "kubeconfig",
				},
				Namespace: kubeconfigSecretNamespace,
			})

			existing, err = bindClient.KubeBindV1alpha1().APIServiceBindings().Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to update the api service binding %s", existing.Name)
			}

			bindings = append(bindings, existing)

			// checking CRD to match the binding
			crd, err := apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, resource.Resource+"."+resource.Group, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return nil, err
			} else if err == nil {
				if !helpers.IsOwnedByBinding(existing.Name, existing.UID, crd.OwnerReferences) {
					return nil, fmt.Errorf("CustomResourceDefinition %s exists, but is not owned by kubeware", crd.Name)
				}
			}
			continue
		}

		// create new APIServiceBinding.
		first := true
		if err := wait.PollUntilContextCancel(context.Background(), 1*time.Second, false, func(ctx context.Context) (bool, error) {
			if !first {
				first = false
				fmt.Fprint(b.Options.IOStreams.ErrOut, ".") // nolint: errcheck
			}
			created, err := bindClient.KubeBindV1alpha1().APIServiceBindings().Create(ctx, &kubewarev1alpha1.APIServiceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resource.Resource + "." + resource.Group,
					Namespace: "kubeware",
				},
				Spec: kubewarev1alpha1.APIServiceBindingSpec{
					KubeconfigSecretRefs: []kubewarev1alpha1.ClusterSecretKeyRef{
						{
							LocalSecretKeyRef: kubewarev1alpha1.LocalSecretKeyRef{
								Name: secretName,
								Key:  "kubeconfig",
							},
							Namespace: "kubeware",
						},
					},
				},
			}, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			// best effort status update to have "Pending" in the Ready condition
			conditions.MarkFalse(created,
				conditionsapi.ReadyCondition,
				"Pending",
				conditionsapi.ConditionSeverityInfo,
				"Pending",
			)
			_, _ = bindClient.KubeBindV1alpha1().APIServiceBindings().UpdateStatus(ctx, created, metav1.UpdateOptions{}) // nolint:errcheck

			fmt.Fprintf(b.Options.IOStreams.ErrOut, "✅ Created APIServiceBinding %s.%s\n", resource.Resource, resource.Group) // nolint: errcheck
			bindings = append(bindings, created)
			return true, nil
		}); err != nil {
			fmt.Fprintln(b.Options.IOStreams.ErrOut, "") // nolint: errcheck
			return nil, err
		}
	}

	return bindings, nil
}
