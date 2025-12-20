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

package indexers

import (
	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
)

const (
	ByServiceBindingKubeconfigSecret = "byKubeconfigSecret"
)

func IndexServiceBindingByKubeconfigSecret(obj any) ([]string, error) {
	binding, ok := obj.(*kubebindv1alpha1.APIServiceBinding)
	if !ok {
		return nil, nil
	}
	return ByServiceBindingKubeconfigSecretKey(binding), nil
}

func ByServiceBindingKubeconfigSecretKey(binding *kubebindv1alpha1.APIServiceBinding) []string {
	ps := binding.Spec.Providers
	var secretRefs []string
	for _, p := range ps {
		secretRefs = append(secretRefs, p.Kubeconfig.Namespace+"/"+p.Kubeconfig.Name)
	}
	return secretRefs
}
