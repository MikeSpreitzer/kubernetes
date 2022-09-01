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

import (
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

func TestMapInMemory(t *testing.T) {
	mim := NewMapInMemory()
	expectMapContents(t, "map@0", mim, true, map[string]interface{}{})
	val1 := 1
	val2 := 2
	val3 := 3
	mim.Put("key1", val1)
	expectMapContents(t, "map@1", mim, true, map[string]interface{}{"key1": val1})
	mim.Put("key2", val2)
	expectMapContents(t, "map@2", mim, true, map[string]interface{}{"key1": val1, "key2": val2})
	mim.Delete("key1")
	expectMapContents(t, "map@3", mim, true, map[string]interface{}{"key2": val2})
	replacement := map[string]interface{}{"key1": val1, "key3": val3}
	mim.Replace(replacement)
	expectMapContents(t, "map@4", mim, true, replacement)
}

func expectMapContents(t *testing.T, descr string, subject Map, accurateSize bool, contents map[string]interface{}) {
	if actual, expected := subject.IsEmpty(), len(contents) == 0; actual != expected {
		t.Fatalf("%s.IsEmpty() => %v, expected %v", descr, actual, expected)
	}
	if accurateSize {
		if actual, expected := subject.CheapLengthEstimate(), len(contents); actual != expected {
			t.Fatalf("%s.CheapLengthEstimate() => %v, expected %v", descr, actual, expected)
		}
	}
	numGot := 0
	err := subject.Enumerate(func(key string, val interface{}) error {
		numGot++
		if _, ok := contents[key]; !ok {
			t.Fatalf("%s.Enumerate() enumerated bad key %q", descr, key)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s failed to Enumerate: %v", descr, err)
	}
	if numGot != len(contents) {
		t.Fatalf("%s enumerated %d items", descr, numGot)
	}
	for key, val := range contents {
		ret, ok := subject.Get(key)
		if !ok {
			t.Fatalf("%s.Get(%s) said not OK", descr, key)
		}
		if !apiequality.Semantic.DeepEqual(val, ret) {
			t.Fatalf("%s.Get(%s) returned %v not %v", descr, key, ret, val)
		}
	}
	_, ok := subject.Get("keyXXX")
	if ok {
		t.Fatalf("%s.Get(keyXXX) said OK", descr)
	}
}
