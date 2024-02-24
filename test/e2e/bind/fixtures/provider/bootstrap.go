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

package provider

import (
	"context"
	"embed"
	"testing"

	"go.bytebuilders.dev/kube-bind/pkg/bootstrap"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

//go:embed *.yaml
var raw embed.FS

func Bootstrap(t *testing.T, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface, batteriesIncluded sets.Set[string]) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	err := bootstrap.Bootstrap(ctx, discoveryClient, dynamicClient, batteriesIncluded, raw)
	require.NoError(t, err)
}
