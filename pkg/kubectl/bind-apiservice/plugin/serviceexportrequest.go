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
	bindclient "go.bytebuilders.dev/kube-bind/client/clientset/versioned"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/rest"
)

func (b *BindAPIServiceOptions) createServiceExportRequest(
	ctx context.Context,
	remoteConfig *rest.Config,
	ns string,
	request *v1alpha1.APIServiceExportRequest,
) (*v1alpha1.APIServiceExportRequest, error) {
	bindRemoteClient, err := bindclient.NewForConfig(remoteConfig)
	if err != nil {
		return nil, err
	}

	// create request in the service provider cluster
	if request.Name == "" {
		request.GenerateName = "export-"
	}
	created, err := bindRemoteClient.KubeBindV1alpha1().APIServiceExportRequests(ns).Create(ctx, request, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	} else if err != nil && request.Name == "" {
		return nil, err
	} else if err != nil {
		request.GenerateName = request.Name + "-"
		request.Name = ""
		created, err = bindRemoteClient.KubeBindV1alpha1().APIServiceExportRequests(ns).Create(ctx, request, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	// wait for the request to be Successful, Failed or deleted
	var result *v1alpha1.APIServiceExportRequest
	if err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		request, err := bindRemoteClient.KubeBindV1alpha1().APIServiceExportRequests(ns).Get(ctx, created.Name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		} else if apierrors.IsNotFound(err) {
			return false, fmt.Errorf("APIServiceExportRequest %s was deleted by the service provider", created.Name)
		}
		if request.Status.Phase == v1alpha1.APIServiceExportRequestPhaseSucceeded {
			result = request
			return true, nil
		}
		if request.Status.Phase == v1alpha1.APIServiceExportRequestPhaseFailed {
			return false, fmt.Errorf("binding request failed: %s", request.Status.TerminalMessage)
		}
		return false, nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (b *BindAPIServiceOptions) printTable(ctx context.Context, config *rest.Config, bindings []*v1alpha1.APIServiceBinding) error {
	printer := printers.NewTablePrinter(printers.PrintOptions{
		WithKind: true,
		Kind:     v1alpha1.SchemeGroupVersion.WithKind("APIServiceBinding").GroupKind(),
	})

	tableConfig := rest.CopyConfig(config)
	tableConfig.APIPath = "/apis"
	tableConfig.AcceptContentTypes = fmt.Sprintf("application/json;as=Table;v=%s;g=%s", metav1.SchemeGroupVersion.Version, metav1.GroupName)
	tableConfig.GroupVersion = &v1alpha1.SchemeGroupVersion
	scheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		return err
	}
	tableConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme)
	tableClient, err := rest.RESTClientFor(tableConfig)
	if err != nil {
		return err
	}

	var bindingsTable metav1.Table
	for _, binding := range bindings {
		var singularTable metav1.Table
		if err := tableClient.Get().Resource("apiservicebindings").Name(binding.Name).Do(ctx).Into(&singularTable); err != nil {
			return err
		}
		if len(bindingsTable.Rows) == 0 {
			bindingsTable = singularTable
		} else {
			bindingsTable.Rows = append(bindingsTable.Rows, singularTable.Rows...)
		}
	}
	return printer.PrintObj(&bindingsTable, b.Options.Out)
}
