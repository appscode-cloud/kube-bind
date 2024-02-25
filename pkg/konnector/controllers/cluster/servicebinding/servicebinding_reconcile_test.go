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
	"testing"

	"go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	konnectormodels "go.bytebuilders.dev/kube-bind/pkg/konnector/models"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	conditionsapi "kmodules.xyz/client-go/api/v1"
)

func TestEnsureCRDs(t *testing.T) {
	tests := []struct {
		name             string
		bindingName      string
		getServiceExport func(provider *konnectormodels.ProviderInfo, name string) (*v1alpha1.APIServiceExport, error)
		getCRD           func(name string) (*apiextensionsv1.CustomResourceDefinition, error)
		expectConditions conditionsapi.Conditions
	}{
		{
			name:             "create-when-crd-missing",
			bindingName:      "foo",
			getCRD:           newGetCRD("bar", newCRD("bar")),
			getServiceExport: newGetServiceExport("foo", newServiceExport("foo")),
			expectConditions: conditionsapi.Conditions{
				conditionsapi.Condition{Type: "Connected", Status: "True"},
			},
		},
		/*
			{
				name:             "fail-when-external-crd-present",
				bindingName:      "foo",
				getCRD:           newGetCRD("foo", newCRD("foo")),
				getServiceExport: newGetServiceExport("foo", newServiceExport("foo")),
				expectConditions: conditionsapi.Conditions{
					conditionsapi.Condition{
						Type: "Connected", Status: "False",
						Severity: "Error",
						Reason:   "ForeignCustomResourceDefinition",
						Message:  "CustomResourceDefinition foo is not owned by kube-bind.appscode.com.",
					},
				},
			},
		*/
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &reconciler{
				getCRD:           tt.getCRD,
				getServiceExport: tt.getServiceExport,
				createCRD: func(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (*apiextensionsv1.CustomResourceDefinition, error) {
					return crd.DeepCopy(), nil
				},
				updateCRD: func(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (*apiextensionsv1.CustomResourceDefinition, error) {
					return crd.DeepCopy(), nil
				},
			}
			b := newBinding(tt.bindingName)
			err := r.ensureCRDs(context.Background(), b)
			require.NoError(t, err)

			for i := range b.Status.Conditions {
				b.Status.Conditions[i].LastTransitionTime = metav1.Time{} // this is hard to compare
			}
			require.Equal(t, tt.expectConditions, b.Status.Conditions)
		})
	}
}

func newGetCRD(name string, crd *apiextensionsv1.CustomResourceDefinition) func(name string) (*apiextensionsv1.CustomResourceDefinition, error) {
	return func(n string) (*apiextensionsv1.CustomResourceDefinition, error) {
		if n == name {
			return crd, nil
		}
		return nil, errors.NewNotFound(apiextensionsv1.SchemeGroupVersion.WithResource("customresourcedefinitions").GroupResource(), "not found")
	}
}

func newCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newGetServiceExport(name string, crd *v1alpha1.APIServiceExport) func(provider *konnectormodels.ProviderInfo, name string) (*v1alpha1.APIServiceExport, error) {
	return func(provider *konnectormodels.ProviderInfo, n string) (*v1alpha1.APIServiceExport, error) {
		if n == name {
			return crd, nil
		}
		return nil, errors.NewNotFound(v1alpha1.SchemeGroupVersion.WithResource("apiserviceexports").GroupResource(), "not found")
	}
}

func newServiceExport(name string) *v1alpha1.APIServiceExport {
	return &v1alpha1.APIServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.APIServiceExportSpec{},
	}
}

func newBinding(name string) *v1alpha1.APIServiceBinding {
	return &v1alpha1.APIServiceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}
