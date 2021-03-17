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

package app

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"

	"github.com/riverzhang/kubesim/cmd/kubesim/app/options"
	kubesimroutes "github.com/riverzhang/kubesim/pkg/routes"
	"github.com/riverzhang/kubesim/pkg/scheduler"
	"github.com/riverzhang/kubesim/pkg/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"

	clientset "k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	aflag "k8s.io/component-base/cli/flag"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/component-base/configz"
	"k8s.io/klog"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	schedoptions "k8s.io/kubernetes/cmd/kube-scheduler/app/options"
	"k8s.io/kubernetes/pkg/scheduler/framework/runtime"
)

// NewKubesimCommand creates a *cobra.Command object with default parameters and registryOptions
func NewKubesimCommand(registryOptions ...utils.Option) *cobra.Command {
	opt := options.NewKubesimOptions()
	cmd := &cobra.Command{
		Use:   "kubesim --kubeconfig KUBECONFIG --port PORT",
		Short: "kubesim provides a virtual kubernetes cluster interface for evaluating your scheduler.",
		Run: func(cmd *cobra.Command, args []string) {
			err := Validate(opt)
			if err != nil {
				fmt.Println(err)
				cmd.Help()
				return
			}
			err = Run(opt, registryOptions...)
			if err != nil {
				fmt.Println(err)
			}
		},
	}

	flags := cmd.Flags()
	flags.SetNormalizeFunc(aflag.WordSepNormalizeFunc)
	flags.AddGoFlagSet(flag.CommandLine)
	opt.AddFlags(flags)

	return cmd
}

func Validate(opt *options.KubesimOptions) error {
	_, present := os.LookupEnv("KS_INCLUSTER")
	if !present {
		if len(opt.KubeConfig) == 0 {
			return fmt.Errorf("kubeconfig is missing")
		}
	}
	return nil
}

func Run(opt *options.KubesimOptions, registryOptions ...utils.Option) error {
	// scheduler options
	conf := options.NewKubesimConfig(opt)

	// init simulator cluster client
	client := fakeclientset.NewSimpleClientset()

	// init realy cluster kubernetes client
	var cfg *restclient.Config
	if len(conf.Options.KubeConfig) != 0 {
		master, err := utils.GetMasterFromKubeConfig(conf.Options.KubeConfig)
		if err != nil {
			return fmt.Errorf("Failed to parse kubeconfig file: %v ", err)
		}

		cfg, err = clientcmd.BuildConfigFromFlags(master, conf.Options.KubeConfig)
		if err != nil {
			return fmt.Errorf("Unable to build config: %v", err)
		}

	} else {
		var err error
		cfg, err = restclient.InClusterConfig()
		if err != nil {
			return fmt.Errorf("Unable to build in cluster config: %v", err)
		}
	}
	var err error

	conf.KubeClient, err = clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}

	opts, err := schedoptions.NewOptions()
	if err != nil {
		return fmt.Errorf("unable to create scheduler options: %v", err)
	}

	// Inject scheduler config file
	if len(conf.Options.SchedulerConfigFile) > 0 {
		opts.ConfigFile = conf.Options.SchedulerConfigFile
	}

	if conf.Options.SkipInClusterLookup {
		if len(conf.Options.ClientCertClientCA) == 0 ||
			len(conf.Options.RequestHeaderClientCAFile) == 0 {
			klog.Errorf("Please give the ClientCertClientCA and RequestHeaderClientCAFile value.")
			return fmt.Errorf("please give the ClientCertClientCA and RequestHeaderClientCAFile value")
		}
	} else {
		opts.Authentication.ClientCert.ClientCA = conf.Options.ClientCertClientCA
		opts.Authentication.RequestHeader.ClientCAFile = conf.Options.RequestHeaderClientCAFile
	}
	if len(opts.Authorization.RemoteKubeConfigFile) == 0 {
		opts.Authorization.RemoteKubeConfigFile = conf.Options.KubeConfig
	}

	if len(conf.Options.PolicyFile) > 0 {
		opts.Deprecated.PolicyConfigFile = conf.Options.PolicyFile
		opts.Deprecated.UseLegacyPolicyConfig = true
	}

	opts.ComponentConfig.LeaderElection.LeaderElect = true
	opts.CombinedInsecureServing.BindPort = 10261
	opts.SecureServing.SecureServingOptions.BindPort = 10262

	if opts.SecureServing != nil {
		if err := opts.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
			return fmt.Errorf("error creating self-signed certificates: %v", err)
		}
	}

	if len(conf.Options.AddNodeStrategy) == 0 {
		conf.Options.AddNodeStrategy = "prd"
	}

	cc, err := InitKubeSchedulerConfiguration(opts, client)
	if err != nil {
		return fmt.Errorf("failed to init kube scheduler configuration: %v ", err)
	}

	kubesim, err := scheduler.New(conf.Options.AddNodeStrategy, cc, client, conf.KubeClient, registryOptions...)
	if err != nil {
		return err
	}

	// sync kubernetes cluster
	if !opt.SKipSyncCluster {
		err = kubesim.SyncWithClient(conf.KubeClient)
		if err != nil {
			return err
		}
	}

	err = kubesim.Run()
	if err != nil {
		return err
	}

	if !opt.SKipSyncCluster {
		err = kubesim.NodeWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.PodWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.PVWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.PVCWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.StorageClassWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.CSINodeWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.CSIDriverWatch(conf.KubeClient)
		if err != nil {
			return err
		}

		err = kubesim.VolumeAttachmentsWatch(conf.KubeClient)
		if err != nil {
			return err
		}
	}

	// Scheduler emulator process
	appsemulator := scheduler.NewAppsEmulator(conf.KubeClient, kubesim)
	horizontalemulator := scheduler.NewHorizontalEmulator(conf.KubeClient, kubesim)
	verticalemulator := scheduler.NewVerticalEmulator(conf.KubeClient, kubesim)

	// Prediction node
	prediction := scheduler.NewPredictionEmulator(conf.KubeClient, kubesim)

	// Scheduler emulator report
	emulatorreport := scheduler.NewEmulatorReport(conf.KubeClient, kubesim)

	// Simulator kubernetes node
	addsimulatornode := scheduler.NewAddNodesEmulator(conf.KubeClient, kubesim)
	listsimulatornode := scheduler.NewListNodesEmulator(conf.KubeClient, kubesim)
	deletesimulatornodes := scheduler.NewDeleteNodesEmulator(conf.KubeClient, kubesim)
	listsimulatornodeinfo := scheduler.NewListNodeInfoEmulator(conf.KubeClient, kubesim)

	// Deprecated interface
	deprecatedprediction := scheduler.NewDeprecatedPredictionEmulator(conf.KubeClient, kubesim)
	deprecatedappsemulator := scheduler.NewDeprecatedAppsEmulator(conf.KubeClient, kubesim)

	// Add router
	router := httprouter.New()
	kubesimroutes.AddPProf(router)
	kubesimroutes.AddVersion(router)

	// Apps emulator router
	kubesimroutes.AddAppsEmulator(router, appsemulator)
	kubesimroutes.AddHorizontalEmulator(router, horizontalemulator)
	kubesimroutes.AddVerticalEmulator(router, verticalemulator)

	// Prediction node router
	kubesimroutes.AddPredictionEmulator(router, prediction)

	// Scheduler emulator report router
	kubesimroutes.AddEmulatorReport(router, emulatorreport)

	// Simulator node router
	kubesimroutes.AddNodesEmulator(router, addsimulatornode)
	kubesimroutes.ListNodesEmulator(router, listsimulatornode)
	kubesimroutes.ListNodeInfoEmulator(router, listsimulatornodeinfo)
	kubesimroutes.DeleteNodesEmulator(router, deletesimulatornodes)

	// deprecated kubesim router
	kubesimroutes.AddDeprecatedPredictionEmulator(router, deprecatedprediction)
	kubesimroutes.AddDeprecatedAppsEmulator(router, deprecatedappsemulator)

	klog.Infof("server starting on the port: %s", conf.Options.Port)
	if err := http.ListenAndServe(":"+conf.Options.Port, router); err != nil {
		klog.Errorf("fatal: %v", err)
	}

	return nil
}

func InitKubeSchedulerConfiguration(opts *schedoptions.Options, client clientset.Interface) (*schedconfig.CompletedConfig, error) {
	c := &schedconfig.Config{}

	if err := opts.ApplyTo(c); err != nil {
		return nil, fmt.Errorf("unable to get scheduler config: %v", err)
	}

	// Prepare kube clients.
	coreBroadcaster := record.NewBroadcaster()

	// Set up leader election if enabled.
	var leaderElectionConfig *leaderelection.LeaderElectionConfig
	var err error
	if c.ComponentConfig.LeaderElection.LeaderElect {
		// Use the scheduler name in the first profile to record leader election.
		coreRecorder := coreBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: c.ComponentConfig.Profiles[0].SchedulerName})
		leaderElectionConfig, err = makeLeaderElectionConfig(c.ComponentConfig.LeaderElection, client, coreRecorder)
		if err != nil {
			return nil, err
		}
	}

	c.Client = client
	c.EventBroadcaster = events.NewEventBroadcasterAdapter(client)
	c.LeaderElection = leaderElectionConfig
	c.InformerFactory = informers.NewSharedInformerFactory(client, 0)

	// Get the completed config
	cc := c.Complete()

	// Configz registration.
	if cz, err := configz.New("componentconfig"); err == nil {
		cz.Set(cc.ComponentConfig)
	} else {
		return nil, fmt.Errorf("unable to register configz: %s", err)
	}

	return &cc, nil
}

// makeLeaderElectionConfig builds a leader election configuration. It will
// create a new resource lock associated with the configuration.
func makeLeaderElectionConfig(config componentbaseconfig.LeaderElectionConfiguration, client clientset.Interface, recorder record.EventRecorder) (*leaderelection.LeaderElectionConfig, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("unable to get hostname: %v", err)
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := hostname + "_" + string(uuid.NewUUID())

	rl, err := resourcelock.New(config.ResourceLock,
		config.ResourceNamespace,
		config.ResourceName,
		client.CoreV1(),
		client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	return &leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: config.LeaseDuration.Duration,
		RenewDeadline: config.RenewDeadline.Duration,
		RetryPeriod:   config.RetryPeriod.Duration,
		WatchDog:      leaderelection.NewLeaderHealthzAdaptor(time.Second * 20),
		Name:          "kube-scheduler",
	}, nil
}

// Option configures a framework.Registry.
type Option func(runtime.Registry) error

// WithPlugin creates an Option based on plugin name and factory. Please don't remove this function: it is used to register out-of-tree plugins,
// hence there are no references to it from the kubernetes scheduler code base.
func WithPlugin(name string, factory runtime.PluginFactory) Option {
	return func(registry runtime.Registry) error {
		return registry.Register(name, factory)
	}
}
