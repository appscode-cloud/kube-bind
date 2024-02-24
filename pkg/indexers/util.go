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

package indexers

import (
	"fmt"

	"k8s.io/client-go/tools/cache"
)

// Append is a helper function that merged a set of indexers.
func Append(indexers ...cache.Indexers) (cache.Indexers, error) {
	ret := cache.Indexers{}
	for _, ind := range indexers {
		for k, v := range ind {
			if _, found := ret[k]; found {
				return nil, fmt.Errorf("duplicate indexer: %s", k)
			}
			ret[k] = v
		}
	}
	return ret, nil
}

func AppendOrDie(indexers ...cache.Indexers) cache.Indexers {
	ret, err := Append(indexers...)
	if err != nil {
		panic(err)
	}
	return ret
}

// AddIfNotPresentOrDie tries to add everything from toAdd to indexer's indexers that does not already exist. It panics
// if it encounters an error.
func AddIfNotPresentOrDie(indexer cache.Indexer, toAdd cache.Indexers) {
	existing := indexer.GetIndexers()
	for indexName := range toAdd {
		if _, exists := existing[indexName]; exists {
			delete(toAdd, indexName)
		}
	}

	if err := indexer.AddIndexers(toAdd); err != nil {
		panic(fmt.Errorf("error adding indexers: %w", err))
	}
}
