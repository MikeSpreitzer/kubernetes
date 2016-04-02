/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

// Package app makes it easy to create a kubelet server for various contexts.
package app

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/rackspace/gophercloud"
	"github.com/rackspace/gophercloud/openstack"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/kubernetes/cmd/netpolagt/app/options"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/chaosclient"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/client/record"
	"k8s.io/kubernetes/pkg/client/restclient"
	unversionedcore "k8s.io/kubernetes/pkg/client/typed/generated/core/unversioned"
	clientauth "k8s.io/kubernetes/pkg/client/unversioned/auth"
	"k8s.io/kubernetes/pkg/client/unversioned/clientcmd"
	clientcmdapi "k8s.io/kubernetes/pkg/client/unversioned/clientcmd/api"
	"k8s.io/kubernetes/pkg/kubelet"
	"k8s.io/kubernetes/pkg/kubelet/config"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/kubelet/server"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"
	"k8s.io/kubernetes/pkg/util/configz"
	"k8s.io/kubernetes/pkg/util/io"
	nodeutil "k8s.io/kubernetes/pkg/util/node"
	"k8s.io/kubernetes/pkg/util/runtime"
	"k8s.io/kubernetes/pkg/util/wait"
)

// bootstrapping interface for kubelet, targets the initialization protocol
type NetworkPolicyAgentBootstrap interface {
	BirthCry()
	Run(<-chan kubetypes.PodUpdate)
	RunOnce(<-chan kubetypes.PodUpdate) ([]kubelet.RunPodResult, error)
}

// create and initialize a Kubelet instance
type NetworkPolicyAgentBuilder func(kc *NetworkPolicyAgentConfig) (NetworkPolicyAgentBootstrap, *config.PodConfig, error)

// NewNetpolagtCommand creates a *cobra.Command object with default parameters
func NewNetpolagtCommand() *cobra.Command {
	s := options.NewNetworkPolicyAgent()
	s.AddFlags(pflag.CommandLine)
	cmd := &cobra.Command{
		Use:  "netpolagt",
		Long: `The netpolagt takes primary responsibility for implementing network policies.  Keep one copy of it running, somewhere.  It monitors the policies in the k8s API server and the related stuff in Neutron, and keeps them in sync.`,
		Run: func(cmd *cobra.Command, args []string) {
		},
	}

	return cmd
}

// UnsecuredNetworkPolicyAgentConfig returns a NetworkPolicyAgentConfig suitable for being run, or an error if the server setup
// is not valid.  It will not start any background processes, and does not include authentication/authorization
func UnsecuredNetworkPolicyAgentConfig(s *options.NetworkPolicyAgent) (*NetworkPolicyAgentConfig, error) {
	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		return nil, err
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		return nil, err
	}

	eo := gophercloud.EndpointOpts{Region: os.Getenv("OS_REGION_NAME")}
	neutronClient, err := openstack.NewNetworkV2(provider, eo)
	if err != nil {
		return nil, err
	}

	return &NetworkPolicyAgentConfig{
		Address:                        net.ParseIP(s.Address),
		Auth:                           nil, // default does not enforce auth[nz]
		ConfigFile:                     s.Config,
		NeutronClient:                  neutronClient,
		EventBurst:                     s.EventBurst,
		EventRecordQPS:                 s.EventRecordQPS,
		FileCheckFrequency:             s.FileCheckFrequency.Duration,
		KubeClient:                     nil,
		OSInterface:                    kubecontainer.RealOS{},
		Runonce:                        s.RunOnce,
		StandaloneMode:                 (len(s.APIServerList) == 0),
		StreamingConnectionIdleTimeout: s.StreamingConnectionIdleTimeout.Duration,
		Writer: &io.StdWriter{},
	}, nil
}

// Run runs the specified Network Policy Agent for the given config.  This should never exit.
// The kcfg argument may be nil - if so, it is initialized from the settings on KubeletServer.
// Otherwise, the caller is assumed to have set up the NetworkPolicyAgentConfig object and all defaults
// will be ignored.
func Run(s *options.NetworkPolicyAgent, kcfg *NetworkPolicyAgentConfig) error {
	err := run(s, kcfg)
	if err != nil {
		glog.Errorf("Failed running netpolagt: %v", err)
	}
	return err
}

func run(s *options.NetworkPolicyAgent, kcfg *NetworkPolicyAgentConfig) (err error) {
	if c, err := configz.New("componentconfig"); err == nil {
		c.Set(s.NetworkPolicyAgentConfiguration)
	} else {
		glog.Errorf("unable to register configz: %s", err)
	}
	if kcfg == nil {
		cfg, err := UnsecuredNetworkPolicyAgentConfig(s)
		if err != nil {
			return err
		}
		kcfg = cfg

		clientConfig, err := CreateAPIServerClientConfig(s)
		if err == nil {
			kcfg.KubeClient, err = clientset.NewForConfig(clientConfig)

			// make a separate client for events
			eventClientConfig := *clientConfig
			eventClientConfig.QPS = s.EventRecordQPS
			eventClientConfig.Burst = s.EventBurst
			kcfg.EventClient, err = clientset.NewForConfig(&eventClientConfig)
		}
		if err != nil && len(s.APIServerList) > 0 {
			glog.Warningf("No API client: %v", err)
		}
	}

	runtime.ReallyCrash = s.ReallyCrashForTesting
	rand.Seed(time.Now().UTC().UnixNano())

	if err := RunNetworkPolicyAgent(kcfg); err != nil {
		return err
	}

	if s.RunOnce {
		return nil
	}

	// run forever
	select {}
}

func authPathClientConfig(s *options.NetworkPolicyAgent, useDefaults bool) (*restclient.Config, error) {
	authInfo, err := clientauth.LoadFromFile(s.AuthPath.Value())
	if err != nil && !useDefaults {
		return nil, err
	}
	// If loading the default auth path, for backwards compatibility keep going
	// with the default auth.
	if err != nil {
		glog.Warningf("Could not load kubernetes auth path %s: %v. Continuing with defaults.", s.AuthPath, err)
	}
	if authInfo == nil {
		// authInfo didn't load correctly - continue with defaults.
		authInfo = &clientauth.Info{}
	}
	authConfig, err := authInfo.MergeWithConfig(restclient.Config{})
	if err != nil {
		return nil, err
	}
	authConfig.Host = s.APIServerList[0]
	return &authConfig, nil
}

func kubeconfigClientConfig(s *options.NetworkPolicyAgent) (*restclient.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: s.NetworkPolicyAgentConfig.Value()},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: s.APIServerList[0]}}).ClientConfig()
}

// createClientConfig creates a client configuration from the command line
// arguments. If either --auth-path or --kubeconfig is explicitly set, it
// will be used (setting both is an error). If neither are set first attempt
// to load the default kubeconfig file, then the default auth path file, and
// fall back to the default auth (none) without an error.
// TODO(roberthbailey): Remove support for --auth-path
func createClientConfig(s *options.NetworkPolicyAgent) (*restclient.Config, error) {
	if s.NetworkPolicyAgentConfig.Provided() && s.AuthPath.Provided() {
		return nil, fmt.Errorf("cannot specify both --kubeconfig and --auth-path")
	}
	if s.NetworkPolicyAgentConfig.Provided() {
		return kubeconfigClientConfig(s)
	}
	if s.AuthPath.Provided() {
		return authPathClientConfig(s, false)
	}
	// Try the kubeconfig default first, falling back to the auth path default.
	clientConfig, err := kubeconfigClientConfig(s)
	if err != nil {
		glog.Warningf("Could not load kubeconfig file %s: %v. Trying auth path instead.", s.NetworkPolicyAgentConfig, err)
		return authPathClientConfig(s, true)
	}
	return clientConfig, nil
}

// CreateAPIServerClientConfig generates a client.Config from command line flags,
// including api-server-list, via createClientConfig and then injects chaos into
// the configuration via addChaosToClientConfig. This func is exported to support
// integration with third party kubelet extensions (e.g. kubernetes-mesos).
func CreateAPIServerClientConfig(s *options.NetworkPolicyAgent) (*restclient.Config, error) {
	if len(s.APIServerList) < 1 {
		return nil, fmt.Errorf("no api servers specified")
	}
	// TODO: adapt Kube client to support LB over several servers
	if len(s.APIServerList) > 1 {
		glog.Infof("Multiple api servers specified.  Picking first one")
	}

	clientConfig, err := createClientConfig(s)
	if err != nil {
		return nil, err
	}

	// Override kubeconfig qps/burst settings from flags
	clientConfig.QPS = s.KubeAPIQPS
	clientConfig.Burst = s.KubeAPIBurst

	addChaosToClientConfig(s, clientConfig)
	return clientConfig, nil
}

// addChaosToClientConfig injects random errors into client connections if configured.
func addChaosToClientConfig(s *options.NetworkPolicyAgent, config *restclient.Config) {
	if s.ChaosChance != 0.0 {
		config.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			seed := chaosclient.NewSeed(1)
			// TODO: introduce a standard chaos package with more tunables - this is just a proof of concept
			// TODO: introduce random latency and stalls
			return chaosclient.NewChaosRoundTripper(rt, chaosclient.LogChaos, seed.P(s.ChaosChance, chaosclient.ErrSimulatedConnectionResetByPeer))
		}
	}
}

// runkubelet is responsible for setting up and running a kubelet.  It is used in three different applications:
//   1 Integration tests
//   2 Kubelet binary
//   3 Standalone 'kubernetes' binary
// Eventually, #2 will be replaced with instances of #3
func RunNetworkPolicyAgent(kcfg *NetworkPolicyAgentConfig) error {
	eventBroadcaster := record.NewBroadcaster()
	hostname := nodeutil.GetHostname("")
	if kcfg.NodeName == "" {
		kcfg.NodeName = hostname // TODO: I just made this up
	}
	kcfg.Recorder = eventBroadcaster.NewRecorder(api.EventSource{Component: "netpolagt", Host: kcfg.NodeName})
	eventBroadcaster.StartLogging(glog.V(3).Infof)
	if kcfg.EventClient != nil {
		glog.V(4).Infof("Sending events to api server.")
		eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{kcfg.EventClient.Events("")})
	} else {
		glog.Warning("No api server defined - no events will be sent to API server.")
	}

	builder := kcfg.Builder
	if builder == nil {
		builder = CreateAndInitNetworkPolicyAgent
	}
	if kcfg.OSInterface == nil {
		kcfg.OSInterface = kubecontainer.RealOS{}
	}
	k, podCfg, err := builder(kcfg)
	if err != nil {
		return fmt.Errorf("failed to create netpolagt: %v", err)
	}

	// process pods and exit.
	if kcfg.Runonce {
		if _, err := k.RunOnce(podCfg.Updates()); err != nil {
			return fmt.Errorf("runonce failed: %v", err)
		}
		glog.Info("Started kubelet as runonce")
	} else {
		startKubelet(k, podCfg, kcfg)
		glog.Info("Started kubelet")
	}
	return nil
}

func startKubelet(k NetworkPolicyAgentBootstrap, podCfg *config.PodConfig, kc *NetworkPolicyAgentConfig) {
	// start the kubelet
	go wait.Until(func() { k.Run(podCfg.Updates()) }, 0, wait.NeverStop)
}

func makePodSourceConfig(kc *NetworkPolicyAgentConfig) *config.PodConfig {
	// source of all configuration
	cfg := config.NewPodConfig(config.PodConfigNotificationIncremental, kc.Recorder)

	// define file config source
	if kc.ConfigFile != "" {
		glog.Infof("Adding manifest file: %v", kc.ConfigFile)
		config.NewSourceFile(kc.ConfigFile, kc.NodeName, kc.FileCheckFrequency, cfg.Channel(kubetypes.FileSource))
	}

	if kc.KubeClient != nil {
		glog.Infof("Watching apiserver")
		config.NewSourceApiserver(kc.KubeClient, kc.NodeName, cfg.Channel(kubetypes.ApiserverSource))
	}
	return cfg
}

// NetworkPolicyAgentConfig is all of the parameters necessary for running a kubelet.
// TODO: This should probably be merged with NetworkPolicyAgent.  The extra object is a consequence of refactoring.
type NetworkPolicyAgentConfig struct {
	Address                        net.IP
	Auth                           server.AuthInterface
	Builder                        NetworkPolicyAgentBuilder
	ConfigFile                     string
	EventClient                    *clientset.Clientset
	EventBurst                     int
	EventRecordQPS                 float32
	FileCheckFrequency             time.Duration
	KubeClient                     *clientset.Clientset
	NeutronClient                  *gophercloud.ServiceClient
	NodeName                       string
	OSInterface                    kubecontainer.OSInterface
	Recorder                       record.EventRecorder
	Runonce                        bool
	StandaloneMode                 bool
	StreamingConnectionIdleTimeout time.Duration
	Writer                         io.Writer

	Options []kubelet.Option
}

func CreateAndInitNetworkPolicyAgent(kc *NetworkPolicyAgentConfig) (k NetworkPolicyAgentBootstrap, pc *config.PodConfig, err error) {
	// TODO: block until all sources have delivered at least one update to the channel, or break the sync loop
	// up into "per source" synchronizations
	// TODO: NetworkPolicyAgentConfig.KubeClient should be a client interface, but client interface misses certain methods
	// used by kubelet. Since NewMainKubelet expects a client interface, we need to make sure we are not passing
	// a nil pointer to it when what we really want is a nil interface.

	k, err = nil, nil // TODO: write this

	if err != nil {
		return nil, nil, err
	}

	k.BirthCry()

	// k.StartGarbageCollection()

	return k, pc, nil
}
