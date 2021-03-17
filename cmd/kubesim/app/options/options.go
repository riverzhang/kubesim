/*
Copyright 2020 The Kubernetes Authors.

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

package options

import (
	"github.com/spf13/pflag"

	"k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

type KubesimConfig struct {
	Pod        *v1.Pod
	KubeClient clientset.Interface
	Options    *KubesimOptions
}

type KubesimOptions struct {
	KubeConfig                 string
	SchedulerConfigFile        string
	DefaultSchedulerConfigFile string
	PolicyFile                 string
	Port                       string
	Verbose                    bool
	PodSpecFile                string
	OutputFormat               string
	AddNodeStrategy            string
	SkipInClusterLookup        bool
	SKipSyncCluster            bool
	ClientCertClientCA         string
	RequestHeaderClientCAFile  string
	MaxLimit                   int32
}

func NewKubesimConfig(opt *KubesimOptions) *KubesimConfig {
	return &KubesimConfig{
		Options: opt,
	}
}

func NewKubesimOptions() *KubesimOptions {
	return &KubesimOptions{}
}

func (s *KubesimOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to the kube config file to use for the analysis.")
	fs.StringVar(&s.Port, "port", s.Port, "scheduler emulator service port")
	fs.BoolVar(&s.SkipInClusterLookup, "skipinclusterlookup", s.SkipInClusterLookup, "SkipInClusterLookup indicates missing authentication configuration should not be retrieved from the cluster configmap")
	fs.StringVar(&s.ClientCertClientCA, "clientcertclientCA", s.ClientCertClientCA, "If set skipinclusterlookup to false, it's need to specify the value, like '/home/ca.pem', your can find it under the /etc/kubernetes/ssl.")
	fs.StringVar(&s.RequestHeaderClientCAFile, "requestclientCAfile", s.ClientCertClientCA, "If set skipinclusterlookup to false, it's need to specify the value, like '/home/ca.pem', your can find it under the /etc/kubernetes/ssl.")
	fs.StringVar(&s.SchedulerConfigFile, "config", s.SchedulerConfigFile, "Paths to files containing scheduler configuration in WriteConfigTo")
	fs.StringVar(&s.PolicyFile, "policyfile", s.PolicyFile, "Paths to Policy file of scheduler")
	fs.StringVar(&s.AddNodeStrategy, "addnodestrategy", s.AddNodeStrategy, "prd and sit can be seclect, prd is default value.")
	fs.StringVar(&s.DefaultSchedulerConfigFile, "default-config", s.DefaultSchedulerConfigFile, "Path to JSON or YAML file containing scheduler configuration.")
	fs.BoolVar(&s.Verbose, "verbose", true, "Verbose mode")
	fs.BoolVar(&s.SKipSyncCluster, "skipsync", false, "Skip sync cluster resource mode, it will create a empty cluster.")
	fs.StringVarP(&s.OutputFormat, "output", "o", s.OutputFormat, "Output format. One of: json|yaml (Note: output is not versioned or guaranteed to be stable across releases).")
}
