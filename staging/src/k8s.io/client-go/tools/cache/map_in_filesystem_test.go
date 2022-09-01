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
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

func TestMapInFilesystem(t *testing.T) {
	testParent, err := os.MkdirTemp("", "miftest")
	if err != nil {
		t.Error(err)
	}
	defer os.RemoveAll(testParent)
	testHead := filepath.Join(testParent, "hed")
	testScheme := runtime.NewScheme()
	appsv1.AddToScheme(testScheme)
	ser := json.NewSerializerWithOptions(json.DefaultMetaFactory, testScheme, testScheme, json.SerializerOptions{})
	// codecFactory := serializer.NewCodecFactory(testScheme)
	// codec := codecFactory.LegacyCodec(appsv1.SchemeGroupVersion)
	mif, err := NewMapinFilesystem("test1", testHead, ser)
	if err != nil {
		t.Fatalf("Failed to create test1, err=%v", err)
	}
	expectMapContents(t, "test1@0", mif, false, map[string]interface{}{})
	key1 := "/ \abc\u2044"
	val1 := &appsv1.ReplicaSet{
		TypeMeta:   metav1.TypeMeta{Kind: "ReplicaSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Namespace: "testns", Name: "name1"},
	}
	key2 := "."
	val2 := &appsv1.ReplicaSet{
		TypeMeta:   metav1.TypeMeta{Kind: "ReplicaSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Namespace: "testns", Name: "name2"},
	}
	key3 := "+%@?:;\\a"
	val3 := &appsv1.ReplicaSet{
		TypeMeta:   metav1.TypeMeta{Kind: "ReplicaSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Namespace: "testns", Name: "name3"},
	}
	mif.Put(key1, val1)
	expectMapContents(t, "test1@1", mif, false, map[string]interface{}{key1: val1})
	mif.Put(key2, val2)
	expectMapContents(t, "test1@2", mif, false, map[string]interface{}{key1: val1, key2: val2})
	// Start over, check that contents persisted
	mif, err = NewMapinFilesystem("test2", testHead, ser)
	if err != nil {
		t.Fatalf("Failed to create test2, err=%v", err)
	}
	expectMapContents(t, "test2@3", mif, false, map[string]interface{}{key1: val1, key2: val2})
	mif.Delete(key2)
	expectMapContents(t, "test2@4", mif, false, map[string]interface{}{key1: val1})
	replacement := map[string]interface{}{key1: val1, key3: val3}
	mif.Replace(replacement)
	expectMapContents(t, "test2@5", mif, false, replacement)
	// Start over again, check that persistence works after replacement
	mif, err = NewMapinFilesystem("test3", testHead, ser)
	if err != nil {
		t.Fatalf("Failed to create test3, err=%v", err)
	}
	expectMapContents(t, "test3@6", mif, false, replacement)

}
