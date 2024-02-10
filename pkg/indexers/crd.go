package indexers

import (
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	kubewarev1alpha1 "go.kubeware.dev/kubeware/pkg/apis/kubeware/v1alpha1"
)

const (
	CRDByServiceBinding = "CRDByServiceBinding"
)

func IndexCRDByServiceBinding(obj interface{}) ([]string, error) {
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return nil, nil
	}

	bindings := []string{}
	for _, ref := range crd.OwnerReferences {
		parts := strings.SplitN(ref.APIVersion, "/", 2)
		if parts[0] != kubewarev1alpha1.SchemeGroupVersion.Group || ref.Kind != "APIServiceBinding" {
			continue
		}
		bindings = append(bindings, ref.Name)
	}
	return bindings, nil
}
