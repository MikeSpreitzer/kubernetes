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

// Package options contains all of the primary arguments for a network
// policy agent.
package options

import (
	_ "net/http/pprof"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	"k8s.io/kubernetes/pkg/kubelet/qos"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"
	"k8s.io/kubernetes/pkg/master/ports"
	"k8s.io/kubernetes/pkg/util"

	"github.com/spf13/pflag"
)

const (
	defaultRootDir = "/var/lib/netpolagt"
)

// NetworkPolicyAgent encapsulates all of the parameters necessary for
// starting up a network policy agent. These can either be set via
// command line or directly.  Cribbed from KubeletServer.
type NetworkPolicyAgent struct {
	componentconfig.NetworkPolicyAgentConfiguration

	AuthPath                 util.StringFlag // Deprecated -- use KubeConfig instead
	NetworkPolicyAgentConfig util.StringFlag
	APIServerList            []string

	RunOnce bool

	// Insert a probability of random errors during calls to the master.
	ChaosChance float64
	// Crash immediately, rather than eating panics.
	ReallyCrashForTesting bool
}

// NewKubeletServer will create a new KubeletServer with default values.
func NewNetworkPolicyAgent() *NetworkPolicyAgent {
	return &NetworkPolicyAgent{
		NetworkPolicyAgentConfig: util.NewStringFlag("/var/lib/kubelet/netpolagt"),

		NetworkPolicyAgentConfiguration: componentconfig.NetworkPolicyAgentConfiguration{
			Address:                        "0.0.0.0",
			CertDirectory:                  "/var/run/kubernetes",
			EventBurst:                     10,
			EventRecordQPS:                 5.0,
			EnableCustomMetrics:            false,
			EnableDebuggingHandlers:        true,
			EnableServer:                   true,
			FileCheckFrequency:             unversioned.Duration{20 * time.Second},
			HTTPCheckFrequency:             unversioned.Duration{20 * time.Second},
			MasterServiceNamespace:         api.NamespaceDefault,
			NodeStatusUpdateFrequency:      unversioned.Duration{10 * time.Second},
			LockFilePath:                   "",
			StreamingConnectionIdleTimeout: unversioned.Duration{4 * time.Hour},
			KubeAPIQPS:                     5.0,
			KubeAPIBurst:                   10,
			BabysitDaemons:                 false,
		},
	}
}

// AddFlags adds flags for a specific NetworkPolicyAgent to the specified FlagSet
func (s *NetworkPolicyAgent) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.Config, "config", s.Config, "Path to the config file or directory of files")
	fs.DurationVar(&s.FileCheckFrequency.Duration, "file-check-frequency", s.FileCheckFrequency.Duration, "Duration between checking config files for new data")
	fs.Var(componentconfig.IPVar{&s.Address}, "address", "The IP address for the Kubelet to serve on (set to 0.0.0.0 for all interfaces)")
	fs.StringVar(&s.TLSCertFile, "tls-cert-file", s.TLSCertFile, ""+
		"File containing x509 Certificate for HTTPS.  (CA cert, if any, concatenated after server cert). "+
		"If --tls-cert-file and --tls-private-key-file are not provided, a self-signed certificate and key "+
		"are generated for the public address and saved to the directory passed to --cert-dir.")
	fs.StringVar(&s.TLSPrivateKeyFile, "tls-private-key-file", s.TLSPrivateKeyFile, "File containing x509 private key matching --tls-cert-file.")
	fs.StringVar(&s.CertDirectory, "cert-dir", s.CertDirectory, "The directory where the TLS certs are located (by default /var/run/kubernetes). "+
		"If --tls-cert-file and --tls-private-key-file are provided, this flag will be ignored.")
	fs.StringVar(&s.NeutronEndpoint, "neutron-endpoint", s.NeutronEndpoint, "If non-empty, use this for the Neutron endpoint to communicate with")
	fs.Float32Var(&s.EventRecordQPS, "event-qps", s.EventRecordQPS, "If > 0, limit event creations per second to this value. If 0, unlimited.")
	fs.IntVar(&s.EventBurst, "event-burst", s.EventBurst, "Maximum size of a bursty event records, temporarily allows event records to burst to this number, while still not exceeding event-qps. Only used if --event-qps > 0")
	fs.BoolVar(&s.RunOnce, "runonce", s.RunOnce, "If true, exit after spawning pods from local manifests or remote urls. Exclusive with --api-servers, and --enable-server")
	fs.Var(&s.NetworkPolicyAgentConfig, "netpolagtconfig", "Path to a kubeconfig file, specifying how to authenticate to API server (the master location is set by the api-servers flag).")
	fs.StringSliceVar(&s.APIServerList, "api-servers", []string{}, "List of Kubernetes API servers for publishing events, and reading pods and services. (ip:port), comma separated.")
	fs.DurationVar(&s.StreamingConnectionIdleTimeout.Duration, "streaming-connection-idle-timeout", s.StreamingConnectionIdleTimeout.Duration, "Maximum time a streaming connection can be idle before the connection is automatically closed. 0 indicates no timeout. Example: '5m'")
	fs.BoolVar(&s.ReallyCrashForTesting, "really-crash-for-testing", s.ReallyCrashForTesting, "If true, when panics occur crash. Intended for testing.")
	fs.Float64Var(&s.ChaosChance, "chaos-chance", s.ChaosChance, "If > 0.0, introduce random client errors and latency. Intended for testing. [default=0.0]")
	fs.Float32Var(&s.KubeAPIQPS, "kube-api-qps", s.KubeAPIQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&s.KubeAPIBurst, "kube-api-burst", s.KubeAPIBurst, "Burst to use while talking with kubernetes apiserver")
}
