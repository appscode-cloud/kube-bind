/*
Copyright The Kube Bind Authors.

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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	v1alpha1 "github.com/kube-bind/kube-bind/pkg/apis/kubebind/v1alpha1"
)

// APIServiceProviderLister helps list APIServiceProviders.
// All objects returned here must be treated as read-only.
type APIServiceProviderLister interface {
	// List lists all APIServiceProviders in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.APIServiceProvider, err error)
	// APIServiceProviders returns an object that can list and get APIServiceProviders.
	APIServiceProviders(namespace string) APIServiceProviderNamespaceLister
	APIServiceProviderListerExpansion
}

// aPIServiceProviderLister implements the APIServiceProviderLister interface.
type aPIServiceProviderLister struct {
	indexer cache.Indexer
}

// NewAPIServiceProviderLister returns a new APIServiceProviderLister.
func NewAPIServiceProviderLister(indexer cache.Indexer) APIServiceProviderLister {
	return &aPIServiceProviderLister{indexer: indexer}
}

// List lists all APIServiceProviders in the indexer.
func (s *aPIServiceProviderLister) List(selector labels.Selector) (ret []*v1alpha1.APIServiceProvider, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.APIServiceProvider))
	})
	return ret, err
}

// APIServiceProviders returns an object that can list and get APIServiceProviders.
func (s *aPIServiceProviderLister) APIServiceProviders(namespace string) APIServiceProviderNamespaceLister {
	return aPIServiceProviderNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// APIServiceProviderNamespaceLister helps list and get APIServiceProviders.
// All objects returned here must be treated as read-only.
type APIServiceProviderNamespaceLister interface {
	// List lists all APIServiceProviders in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.APIServiceProvider, err error)
	// Get retrieves the APIServiceProvider from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.APIServiceProvider, error)
	APIServiceProviderNamespaceListerExpansion
}

// aPIServiceProviderNamespaceLister implements the APIServiceProviderNamespaceLister
// interface.
type aPIServiceProviderNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all APIServiceProviders in the indexer for a given namespace.
func (s aPIServiceProviderNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.APIServiceProvider, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.APIServiceProvider))
	})
	return ret, err
}

// Get retrieves the APIServiceProvider from the indexer for a given namespace and name.
func (s aPIServiceProviderNamespaceLister) Get(name string) (*v1alpha1.APIServiceProvider, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("apiserviceprovider"), name)
	}
	return obj.(*v1alpha1.APIServiceProvider), nil
}