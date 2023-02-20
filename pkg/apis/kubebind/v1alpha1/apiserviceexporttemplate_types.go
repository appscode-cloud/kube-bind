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

package v1alpha1

import (
	resourcemeta "kmodules.xyz/resource-metadata/apis/meta/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +crd
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`,priority=0

// APIServiceExportTemplate : TODO
type APIServiceExportTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec specifies the connections & resource sync information for an APIServiceExport object
	Spec APIServiceExportTemplateSpec `json:"spec"`
}

type APIServiceExportTemplateSpec struct {
	PermissionClaims []PermissionClaimTemplate `json:"permissionClaims"`
}

type PermissionClaimTemplate struct {
	resourcemeta.ResourceConnection `json:",inline"`
	Verbs                           ClaimResourceVerbTemplate `json:"verbs"`
}

type ClaimResourceVerbTemplate struct {
	Provider []string `json:"provider"`
	Consumer []string `json:"consumer"`
}

// APIServiceExportTemplateList is the objects list that represents the APIServiceExportTemplate.
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type APIServiceExportTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []APIServiceExportTemplateList `json:"items"`
}
