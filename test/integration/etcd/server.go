/*
Copyright 2018 The Kubernetes Authors.

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

package etcd

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	genericapiserveroptions "k8s.io/apiserver/pkg/server/options"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubernetes/cmd/kube-apiserver/app"
	"k8s.io/kubernetes/cmd/kube-apiserver/app/options"
	"k8s.io/kubernetes/test/integration"
	"k8s.io/kubernetes/test/integration/framework"
	netutils "k8s.io/utils/net"

	// install all APIs
	_ "k8s.io/kubernetes/pkg/controlplane"
)

// This key is for testing purposes only and is not considered secure.
const ecdsaPrivateKey = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIEZmTmUhuanLjPA2CLquXivuwBDHTt5XYwgIr/kA1LtRoAoGCCqGSM49
AwEHoUQDQgAEH6cuzP8XuD5wal6wf9M6xDljTOPLX2i8uIp/C/ASqiIGUeeKQtX0
/IR3qCXyThP/dbCiHrF3v1cuhBOHY8CLVg==
-----END EC PRIVATE KEY-----`

// StartRealAPIServerOrDie starts an API server that is appropriate for use in tests that require one of every resource
func StartRealAPIServerOrDie(t *testing.T, configFuncs ...func(*options.ServerRunOptions)) *APIServer {
	certDir, err := os.MkdirTemp("", t.Name())
	if err != nil {
		t.Fatal(err)
	}

	_, defaultServiceClusterIPRange, err := netutils.ParseCIDRSloppy("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}

	listener, _, err := genericapiserveroptions.CreateListener("tcp", "127.0.0.1:0", net.ListenConfig{})
	if err != nil {
		t.Fatal(err)
	}

	saSigningKeyFile, err := os.CreateTemp("/tmp", "insecure_test_key")
	if err != nil {
		t.Fatalf("create temp file failed: %v", err)
	}
	defer os.RemoveAll(saSigningKeyFile.Name())
	if err = os.WriteFile(saSigningKeyFile.Name(), []byte(ecdsaPrivateKey), 0666); err != nil {
		t.Fatalf("write file %s failed: %v", saSigningKeyFile.Name(), err)
	}

	kubeAPIServerOptions := options.NewServerRunOptions()
	kubeAPIServerOptions.SecureServing.Listener = listener
	kubeAPIServerOptions.SecureServing.ServerCert.CertDirectory = certDir
	kubeAPIServerOptions.ServiceAccountSigningKeyFile = saSigningKeyFile.Name()
	kubeAPIServerOptions.Etcd.StorageConfig.Transport.ServerList = []string{framework.GetEtcdURL()}
	kubeAPIServerOptions.Etcd.DefaultStorageMediaType = runtime.ContentTypeJSON // force json we can easily interpret the result in etcd
	kubeAPIServerOptions.ServiceClusterIPRanges = defaultServiceClusterIPRange.String()
	kubeAPIServerOptions.Authentication.APIAudiences = []string{"https://foo.bar.example.com"}
	kubeAPIServerOptions.Authentication.ServiceAccounts.Issuers = []string{"https://foo.bar.example.com"}
	kubeAPIServerOptions.Authentication.ServiceAccounts.KeyFiles = []string{saSigningKeyFile.Name()}
	kubeAPIServerOptions.Authorization.Modes = []string{"RBAC"}
	kubeAPIServerOptions.Admission.GenericAdmission.DisablePlugins = []string{"ServiceAccount"}
	kubeAPIServerOptions.APIEnablement.RuntimeConfig["api/all"] = "true"
	for _, f := range configFuncs {
		f(kubeAPIServerOptions)
	}
	completedOptions, err := app.Complete(kubeAPIServerOptions)
	if err != nil {
		t.Fatal(err)
	}

	if errs := completedOptions.Validate(); len(errs) != 0 {
		t.Fatalf("failed to validate ServerRunOptions: %v", utilerrors.NewAggregate(errs))
	}

	// get etcd client before starting API server
	rawClient, kvClient, err := integration.GetEtcdClients(completedOptions.Etcd.StorageConfig.Transport)
	if err != nil {
		t.Fatal(err)
	}

	// make sure we start with a clean slate
	if _, err := kvClient.Delete(context.Background(), "/registry/", clientv3.WithPrefix()); err != nil {
		t.Fatal(err)
	}

	stopCh := make(chan struct{})

	kubeAPIServer, err := app.CreateServerChain(completedOptions)
	if err != nil {
		t.Fatal(err)
	}

	kubeClientConfig := restclient.CopyConfig(kubeAPIServer.GenericAPIServer.LoopbackClientConfig)

	// we make lots of requests, don't be slow
	kubeClientConfig.QPS = 99999
	kubeClientConfig.Burst = 9999

	// we make requests to all resources, don't log warnings about deprecated ones
	restclient.SetDefaultWarningHandler(restclient.NoWarnings{})

	kubeClient := clientset.NewForConfigOrDie(kubeClientConfig)

	errCh := make(chan error)
	go func() {
		// Catch panics that occur in this go routine so we get a comprehensible failure
		defer func() {
			if err := recover(); err != nil {
				t.Errorf("Unexpected panic trying to start API server: %#v", err)
			}
		}()
		defer close(errCh)

		prepared, err := kubeAPIServer.PrepareRun()
		if err != nil {
			errCh <- err
			return
		}
		if err := prepared.Run(stopCh); err != nil {
			errCh <- err
			t.Error(err)
			return
		}
	}()

	lastHealth := ""
	attempt := 0
	if err := wait.PollImmediate(time.Second, time.Minute, func() (done bool, err error) {
		select {
		case err := <-errCh:
			return false, err
		default:
		}

		// wait for the server to be healthy
		result := kubeClient.RESTClient().Get().AbsPath("/healthz").Do(context.TODO())
		content, _ := result.Raw()
		lastHealth = string(content)
		if errResult := result.Error(); errResult != nil {
			attempt++
			if attempt < 10 {
				t.Log("waiting for server to be healthy")
			} else {
				t.Log(errResult)
			}
			return false, nil
		}
		var status int
		result.StatusCode(&status)
		return status == http.StatusOK, nil
	}); err != nil {
		t.Log(lastHealth)
		t.Fatal(err)
	}

	// create CRDs so we can make sure that custom resources do not get lost
	CreateTestCRDs(t, apiextensionsclientset.NewForConfigOrDie(kubeClientConfig), false, GetCustomResourceDefinitionData()...)

	// force cached discovery reset
	discoveryClient := cacheddiscovery.NewMemCacheClient(kubeClient.Discovery())
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	restMapper.Reset()

	_, serverResources, err := kubeClient.Discovery().ServerGroupsAndResources()
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		// Closing stopCh is stopping apiserver and cleaning up
		// after itself, including shutting down its storage layer.
		close(stopCh)

		// If the apiserver was started, let's wait for it to
		// shutdown clearly.
		err, ok := <-errCh
		if ok && err != nil {
			t.Error(err)
		}
		rawClient.Close()
		if err := os.RemoveAll(certDir); err != nil {
			t.Log(err)
		}
	}

	return &APIServer{
		Client:    kubeClient,
		Dynamic:   dynamic.NewForConfigOrDie(kubeClientConfig),
		Config:    kubeClientConfig,
		KV:        kvClient,
		Mapper:    restMapper,
		Resources: GetResources(t, serverResources),
		Cleanup:   cleanup,
	}
}

// APIServer represents a running API server that is ready for use
// The Cleanup func must be deferred to prevent resource leaks
type APIServer struct {
	Client    clientset.Interface
	Dynamic   dynamic.Interface
	Config    *restclient.Config
	KV        clientv3.KV
	Mapper    meta.RESTMapper
	Resources []Resource
	Cleanup   func()
}

// Resource contains REST mapping information for a specific resource and extra metadata such as delete collection support
type Resource struct {
	Mapping             *meta.RESTMapping
	HasDeleteCollection bool
}

// GetResources fetches the Resources associated with serverResources that support get and create
func GetResources(t *testing.T, serverResources []*metav1.APIResourceList) []Resource {
	var resources []Resource

	for _, discoveryGroup := range serverResources {
		for _, discoveryResource := range discoveryGroup.APIResources {
			// this is a subresource, skip it
			if strings.Contains(discoveryResource.Name, "/") {
				continue
			}
			hasCreate := false
			hasGet := false
			hasDeleteCollection := false
			for _, verb := range discoveryResource.Verbs {
				if verb == "get" {
					hasGet = true
				}
				if verb == "create" {
					hasCreate = true
				}
				if verb == "deletecollection" {
					hasDeleteCollection = true
				}
			}
			if !(hasCreate && hasGet) {
				continue
			}

			resourceGV, err := schema.ParseGroupVersion(discoveryGroup.GroupVersion)
			if err != nil {
				t.Fatal(err)
			}
			gvk := resourceGV.WithKind(discoveryResource.Kind)
			if len(discoveryResource.Group) > 0 || len(discoveryResource.Version) > 0 {
				gvk = schema.GroupVersionKind{
					Group:   discoveryResource.Group,
					Version: discoveryResource.Version,
					Kind:    discoveryResource.Kind,
				}
			}
			gvr := resourceGV.WithResource(discoveryResource.Name)

			resources = append(resources, Resource{
				Mapping: &meta.RESTMapping{
					Resource:         gvr,
					GroupVersionKind: gvk,
					Scope:            scope(discoveryResource.Namespaced),
				},
				HasDeleteCollection: hasDeleteCollection,
			})
		}
	}

	return resources
}

func scope(namespaced bool) meta.RESTScope {
	if namespaced {
		return meta.RESTScopeNamespace
	}
	return meta.RESTScopeRoot
}

// JSONToUnstructured converts a JSON stub to unstructured.Unstructured and
// returns a dynamic resource client that can be used to interact with it
func JSONToUnstructured(stub, namespace string, mapping *meta.RESTMapping, dynamicClient dynamic.Interface) (dynamic.ResourceInterface, *unstructured.Unstructured, error) {
	typeMetaAdder := map[string]interface{}{}
	if err := json.Unmarshal([]byte(stub), &typeMetaAdder); err != nil {
		return nil, nil, err
	}

	// we don't require GVK on the data we provide, so we fill it in here.  We could, but that seems extraneous.
	typeMetaAdder["apiVersion"] = mapping.GroupVersionKind.GroupVersion().String()
	typeMetaAdder["kind"] = mapping.GroupVersionKind.Kind

	if mapping.Scope == meta.RESTScopeRoot {
		namespace = ""
	}

	return dynamicClient.Resource(mapping.Resource).Namespace(namespace), &unstructured.Unstructured{Object: typeMetaAdder}, nil
}

// CreateTestCRDs creates the given CRDs, any failure causes the test to Fatal.
// If skipCrdExistsInDiscovery is true, the CRDs are only checked for the Established condition via their Status.
// If skipCrdExistsInDiscovery is false, the CRDs are checked via discovery, see CrdExistsInDiscovery.
func CreateTestCRDs(t *testing.T, client apiextensionsclientset.Interface, skipCrdExistsInDiscovery bool, crds ...*apiextensionsv1.CustomResourceDefinition) {
	for _, crd := range crds {
		createTestCRD(t, client, skipCrdExistsInDiscovery, crd)
	}
}

func createTestCRD(t *testing.T, client apiextensionsclientset.Interface, skipCrdExistsInDiscovery bool, crd *apiextensionsv1.CustomResourceDefinition) {
	if _, err := client.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create %s CRD; %v", crd.Name, err)
	}
	if skipCrdExistsInDiscovery {
		if err := waitForEstablishedCRD(client, crd.Name); err != nil {
			t.Fatalf("Failed to establish %s CRD; %v", crd.Name, err)
		}
		return
	}
	if err := wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return CrdExistsInDiscovery(client, crd), nil
	}); err != nil {
		t.Fatalf("Failed to see %s in discovery: %v", crd.Name, err)
	}
}

func waitForEstablishedCRD(client apiextensionsclientset.Interface, name string) error {
	return wait.PollImmediate(500*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1.Established:
				if cond.Status == apiextensionsv1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	})
}

// CrdExistsInDiscovery checks to see if the given CRD exists in discovery at all served versions.
func CrdExistsInDiscovery(client apiextensionsclientset.Interface, crd *apiextensionsv1.CustomResourceDefinition) bool {
	var versions []string
	for _, v := range crd.Spec.Versions {
		if v.Served {
			versions = append(versions, v.Name)
		}
	}
	for _, v := range versions {
		if !crdVersionExistsInDiscovery(client, crd, v) {
			return false
		}
	}
	return true
}

func crdVersionExistsInDiscovery(client apiextensionsclientset.Interface, crd *apiextensionsv1.CustomResourceDefinition, version string) bool {
	resourceList, err := client.Discovery().ServerResourcesForGroupVersion(crd.Spec.Group + "/" + version)
	if err != nil {
		return false
	}
	for _, resource := range resourceList.APIResources {
		if resource.Name == crd.Spec.Names.Plural {
			return true
		}
	}
	return false
}
