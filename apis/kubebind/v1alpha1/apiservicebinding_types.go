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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	conditionsapi "kmodules.xyz/client-go/api/v1"
)

const (
	ResourceKindAPIServiceBinding = "APIServiceBinding"
	ResourceAPIServiceBinding     = "apiservicebinding"
	ResourceAPIServiceBindings    = "apiservicebindings"
)

const (
	// APIServiceBindingConditionSecretValid is set when the secret is valid.
	APIServiceBindingConditionSecretValid conditionsapi.ConditionType = "SecretValid"

	// APIServiceBindingConditionInformersSynced is set when the informers can sync.
	APIServiceBindingConditionInformersSynced conditionsapi.ConditionType = "InformersSynced"

	// APIServiceBindingConditionHeartbeating is set when the ClusterBinding of the service provider
	// is successfully heartbeated.
	APIServiceBindingConditionHeartbeating conditionsapi.ConditionType = "Heartbeating"

	// APIServiceBindingConditionConnected means the APIServiceBinding has been connected to a APIServiceExport.
	APIServiceBindingConditionConnected conditionsapi.ConditionType = "Connected"

	// APIServiceBindingConditionSchemaInSync is set to true when the APIServiceExport's
	// schema is applied to the consumer cluster.
	APIServiceBindingConditionSchemaInSync conditionsapi.ConditionType = "SchemaInSync"

	// DownstreamFinalizer is put on downstream objects to block their deletion until
	// the upstream object has been deleted.
	DownstreamFinalizer = "kubebind.io/syncer"
)

// APIServiceBinding binds an API service represented by a APIServiceExport
// in a service provider cluster into a consumer cluster. This object lives in
// the consumer cluster.
//
// +crd
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Cluster,categories=kube-bindings,shortName=sb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Resources",type="string",JSONPath=`.metadata.annotations.kube-bind\.appscode\.com/resources`,priority=1
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`,priority=0
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].message`,priority=0
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`,priority=0
type APIServiceBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec specifies how an API service from a service provider should be bound in the
	// local consumer cluster.
	Spec APIServiceBindingSpec `json:"spec"`

	// status contains reconciliation information for a service binding.
	Status APIServiceBindingStatus `json:"status,omitempty"`
}

func (in *APIServiceBinding) GetConditions() conditionsapi.Conditions {
	return in.Status.Conditions
}

func (in *APIServiceBinding) SetConditions(conditions conditionsapi.Conditions) {
	in.Status.Conditions = conditions
}

type APIServiceBindingSpec struct {
	// +required
	// +kubebuilder:validation:Required
	// Providers contains the provider ClusterIdentity and KubeconfigSecretRef of the provider cluster
	Providers []Provider `json:"providers,omitempty"`
}

type Provider struct {
	ClusterIdentity `json:",inline"`
	RemoteNamespace string `json:"remoteNamespace,omitempty"`

	Kubeconfig ClusterSecretKeyRef `json:"kubeconfig,omitempty"`
}

type ClusterIdentity struct {
	ClusterUID  string `json:"clusterUID"`
	ClusterName string `json:"clusterName"`
}

type APIServiceBindingStatus struct {
	// conditions is a list of conditions that apply to the APIServiceBinding.
	Conditions conditionsapi.Conditions `json:"conditions,omitempty"`
}

// APIServiceBindingList is a list of APIServiceBindings.
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type APIServiceBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []APIServiceBinding `json:"items"`
}

type LocalSecretKeyRef struct {
	// Name of the referent.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// The key of the secret to select from.  Must be "kubeconfig".
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=kubeconfig
	Key string `json:"key"`
}

type ClusterSecretKeyRef struct {
	LocalSecretKeyRef `json:",inline"`

	// Namespace of the referent.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}
