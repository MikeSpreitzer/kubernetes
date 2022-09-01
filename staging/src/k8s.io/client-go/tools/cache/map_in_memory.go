/*
Copyright 2022 The Kubernetes Authors.

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

package cache

// mapInMemory implements Map using Go's built-in memory-resident `map` construct.
type mapInMemory map[string]interface{}

var _ Map = &mapInMemory{}

// NewMapInMemory makes a new one
func NewMapInMemory() Map {
	ans := make(mapInMemory, 8)
	return &ans
}

func (mim *mapInMemory) Get(key string) (interface{}, bool) {
	item, ok := (*mim)[key]
	return item, ok
}

func (mim *mapInMemory) Put(key string, item interface{}) {
	(*mim)[key] = item
}

func (mim *mapInMemory) IsEmpty() bool {
	return len(*mim) == 0
}

func (mim *mapInMemory) CheapLengthEstimate() int {
	return len(*mim)
}

func (mim *mapInMemory) Delete(key string) {
	delete(*mim, key)
}

func (mim *mapInMemory) Enumerate(visitor func(string, interface{}) error) error {
	for key, item := range *mim {
		err := visitor(key, item)
		if err != nil {
			return err
		}
	}
	return nil
}

func (mim *mapInMemory) Replace(replacement map[string]interface{}) {
	(*mim) = replacement
}
