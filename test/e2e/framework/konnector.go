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
	"context"
	"testing"

	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	// "go.bytebuilders.dev/kube-bind/deploy/crd"
	"go.bytebuilders.dev/kube-bind/pkg/konnector"
	"go.bytebuilders.dev/kube-bind/pkg/konnector/options"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/rest"
	"kmodules.xyz/client-go/apiextensions"
)

func StartKonnector(t *testing.T, clientConfig *rest.Config, args ...string) *konnector.Server {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	crdClient, err := apiextensionsclient.NewForConfig(clientConfig)
	require.NoError(t, err)
	err = apiextensions.RegisterCRDs(crdClient, []*apiextensions.CustomResourceDefinition{
		kubebindv1alpha1.APIServiceBinding{}.CustomResourceDefinition(),
	})
	require.NoError(t, err)

	fs := pflag.NewFlagSet("konnector", pflag.ContinueOnError)
	options := options.NewOptions()
	options.AddFlags(fs)
	err = fs.Parse(args)
	require.NoError(t, err)

	completed, err := options.Complete()
	require.NoError(t, err)

	config, err := konnector.NewConfig(completed)
	require.NoError(t, err)

	server, err := konnector.NewServer(config)
	require.NoError(t, err)
	prepared, err := server.PrepareRun(ctx)
	require.NoError(t, err)

	prepared.OptionallyStartInformers(ctx)
	go func() {
		err := prepared.Run(ctx)
		select {
		case <-ctx.Done():
		default:
			require.NoError(t, err)
		}
	}()

	return server
}
