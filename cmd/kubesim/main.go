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

package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"time"

	"k8s.io/klog"

	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"

	"github.com/riverzhang/kubesim/cmd/kubesim/app"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	rand.Seed(time.Now().UnixNano())

	// add klog flags
	local := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	klog.InitFlags(local)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(local)

	defer klog.Flush()

	cmd := app.NewKubesimCommand(
	//add plugin
	)
	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
