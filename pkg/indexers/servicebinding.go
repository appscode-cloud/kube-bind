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

package indexers

import (
	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/pkg/apis/kubebind/v1alpha1"
)

const (
	ByServiceBindingKubeconfigSecret = "byKubeconfigSecret"
)

func IndexServiceBindingByKubeconfigSecret(obj interface{}) ([]string, error) {
	binding, ok := obj.(*kubebindv1alpha1.APIServiceBinding)
	if !ok {
		return nil, nil
	}
	return ByServiceBindingKubeconfigSecretKey(binding), nil
}

func ByServiceBindingKubeconfigSecretKey(binding *kubebindv1alpha1.APIServiceBinding) []string {
	refs := binding.Spec.KubeconfigSecretRefs
	var secretRefs []string
	for _, ref := range refs {
		secretRefs = append(secretRefs, ref.Namespace+"/"+ref.Name)
	}
	return secretRefs
}
