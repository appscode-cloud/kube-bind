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

package resources

import (
	"context"

	"go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	bindclient "go.bytebuilders.dev/kube-bind/client/clientset/versioned"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func CreateClusterBinding(ctx context.Context, client bindclient.Interface, ns, secretName, clusterName string) error {
	logger := klog.FromContext(ctx)

	clusterBinding := &v1alpha1.ClusterBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClusterBindingName,
			Namespace: ns,
		},
		Spec: v1alpha1.ClusterBindingSpec{
			ProviderClusterName: clusterName,
			KubeconfigSecretRef: v1alpha1.LocalSecretKeyRef{
				Name: secretName,
				Key:  "kubeconfig",
			},
		},
	}

	logger.V(3).Info("Creating ClusterBinding")
	_, err := client.KubeBindV1alpha1().ClusterBindings(ns).Create(ctx, clusterBinding, metav1.CreateOptions{})
	return err
}
