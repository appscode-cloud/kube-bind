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

package plugin

import (
	"context"
	"fmt"
	"time"

	"go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	"go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1/helpers"
	bindclient "go.bytebuilders.dev/kube-bind/client/clientset/versioned"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	conditionsapi "kmodules.xyz/client-go/api/v1"
	"kmodules.xyz/client-go/conditions"
)

func (b *BindAPIServiceOptions) createAPIServiceBindings(ctx context.Context, config *rest.Config, request *v1alpha1.APIServiceExportRequest, secretName, remoteNs string) ([]*v1alpha1.APIServiceBinding, error) {
	bindClient, err := bindclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	apiextensionsClient, err := apiextensionsclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	var bindings []*v1alpha1.APIServiceBinding
	for _, resource := range request.Spec.Resources {
		name := resource.Resource + "." + resource.Group
		existing, err := bindClient.KubeBindV1alpha1().APIServiceBindings().Get(ctx, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		} else if err == nil {
			hasSecret := false
			for _, p := range existing.Spec.Providers {
				if p.Kubeconfig.Namespace == models.KonnectorNamespace && p.Kubeconfig.Name == secretName {
					hasSecret = true
					_, _ = fmt.Fprintf(b.Options.ErrOut, "✅ Existing APIServiceBinding \"%s\" already has the secret \"%s\".\n", existing.Name, secretName) // nolint: errcheck
					break
				}
			}

			if hasSecret {
				continue
			}

			_, _ = fmt.Fprintf(b.Options.ErrOut, "✅ Updating existing APIServiceBinding %s.\n", existing.Name) // nolint: errcheck

			existing.Spec.Providers = append(existing.Spec.Providers, v1alpha1.Provider{
				Kubeconfig: v1alpha1.ClusterSecretKeyRef{
					LocalSecretKeyRef: v1alpha1.LocalSecretKeyRef{
						Name: secretName,
						Key:  "kubeconfig",
					},
					Namespace: models.KonnectorNamespace,
				},
				RemoteNamespace: remoteNs,
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
					return nil, fmt.Errorf("CustomResourceDefinition %s exists, but is not owned by kube-bind", crd.Name)
				}
			}
			continue
		}

		// create new APIServiceBinding.
		first := true
		if err := wait.PollUntilContextCancel(context.Background(), 1*time.Second, false, func(ctx context.Context) (bool, error) {
			if !first {
				first = false
				fmt.Fprint(b.Options.ErrOut, ".") // nolint: errcheck
			}
			created, err := bindClient.KubeBindV1alpha1().APIServiceBindings().Create(ctx, &v1alpha1.APIServiceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resource.Resource + "." + resource.Group,
					Namespace: models.KonnectorNamespace,
				},
				Spec: v1alpha1.APIServiceBindingSpec{
					Providers: []v1alpha1.Provider{
						{
							Kubeconfig: v1alpha1.ClusterSecretKeyRef{
								LocalSecretKeyRef: v1alpha1.LocalSecretKeyRef{
									Name: secretName,
									Key:  "kubeconfig",
								},
								Namespace: models.KonnectorNamespace,
							},
							RemoteNamespace: remoteNs,
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

			_, _ = fmt.Fprintf(b.Options.ErrOut, "✅ Created APIServiceBinding %s.%s\n", resource.Resource, resource.Group) // nolint: errcheck
			bindings = append(bindings, created)
			return true, nil
		}); err != nil {
			_, _ = fmt.Fprintln(b.Options.ErrOut, "") // nolint: errcheck
			return nil, err
		}
	}

	return bindings, nil
}
