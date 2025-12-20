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

package kubernetes

import (
	"go.bytebuilders.dev/kube-bind/contrib/example-backend/kubernetes/resources"

	corev1 "k8s.io/api/core/v1"
)

const (
	NamespacesByIdentity = "namespacesByIdentity"
)

func IndexNamespacesByIdentity(obj any) ([]string, error) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil, nil
	}

	if id, found := ns.Annotations[resources.IdentityAnnotationKey]; found {
		return []string{id}, nil
	}

	return nil, nil
}
