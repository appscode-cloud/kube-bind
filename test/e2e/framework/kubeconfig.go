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

package framework

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func ClientConfig(t *testing.T) *rest.Config {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = os.Getenv("KUBECONFIG")
	clientcmdConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil)
	config, err := clientcmdConfig.ClientConfig()
	require.NoError(t, err)

	return config
}

func RestToKubeconfig(config *rest.Config, namespace string) clientcmdapi.Config {
	return clientcmdapi.Config{
		Kind:       "Config",
		APIVersion: "v1",
		Clusters: map[string]*clientcmdapi.Cluster{
			"default": {
				Server:                   config.Host,
				CertificateAuthorityData: config.CAData,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"default": {
				Cluster:   "default",
				Namespace: namespace,
				AuthInfo:  "default",
			},
		},
		CurrentContext: "default",
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"default": {
				Token: config.BearerToken,
			},
		},
	}
}
