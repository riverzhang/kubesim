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

package node

import (
	goruntime "runtime"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/riverzhang/kubesim/pkg/utils"
)

const (
	ResourceNvidiaGPU = "nvidia.com/gpu"
)

// BuildNode builds a *v1.Node. Returns error if failed to parse.
func BuildNode(nodeConfig *utils.NodeConfig, nodeName string) (*v1.Node, error) {
	clock := time.Now()

	labels := nodeConfig.NodeLabels
	labels["kubernetes.io/arch"] = "amd64"
	labels["kubernetes.io/hostname"] = nodeName
	labels["kubernetes.io/os"] = "linux"
	labels["node-role.kubernetes.io/node"] = ""
	labels["provisioner"] = "kubesim"

	node := v1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: labels},
		Spec:       v1.NodeSpec{},
		Status: v1.NodeStatus{
			Capacity:    buildNodeCapacity(nodeConfig),
			Allocatable: buildNodeAllocatable(nodeConfig),
			NodeInfo:    buildNodeSystemInfo(),
			Conditions:  buildNodeCondition(metav1.NewTime(clock)),
			Addresses: []v1.NodeAddress{
				{Type: v1.NodeExternalIP, Address: "127.0.0.1"},
				{Type: v1.NodeInternalIP, Address: "127.0.0.1"},
			},
			Images: []v1.ContainerImage{},
		},
	}

	return &node, nil
}

func buildNodeCondition(clock metav1.Time) []v1.NodeCondition {
	return []v1.NodeCondition{
		{
			Type:               v1.NodeReady,
			Status:             v1.ConditionTrue,
			LastHeartbeatTime:  clock,
			LastTransitionTime: clock,
			Reason:             "KubeletReady",
			Message:            "kubelet is posting ready status",
		},
		{
			Type:               v1.NodeNetworkUnavailable,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  clock,
			LastTransitionTime: clock,
			Reason:             "NetworkIsNotCorrectly",
			Message:            "Node network is not correctl configured",
		},
		{
			Type:               v1.NodeMemoryPressure,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  clock,
			LastTransitionTime: clock,
			Reason:             "KubeletHasSufficientMemory",
			Message:            "kubelet has sufficient memory available",
		},
		{
			Type:               v1.NodeDiskPressure,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  clock,
			LastTransitionTime: clock,
			Reason:             "KubeletHasNoDiskPressure",
			Message:            "kubelet has no disk pressure",
		},
		{
			Type:               v1.NodePIDPressure,
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  clock,
			LastTransitionTime: clock,
			Reason:             "KubeletHasSufficientPID",
			Message:            "kubelet has sufficient PID available",
		},
	}
}

func buildNodeSystemInfo() v1.NodeSystemInfo {
	return v1.NodeSystemInfo{
		MachineID:               "123ff",
		SystemUUID:              "abcff",
		BootID:                  "1bff3",
		KernelVersion:           "4.18.0-0.bpo.4-amd64",
		OSImage:                 "Centos GNU/Linux 7",
		OperatingSystem:         goruntime.GOOS,
		Architecture:            goruntime.GOARCH,
		ContainerRuntimeVersion: "docker://1.19.0",
		KubeletVersion:          "v1.20.4",
		KubeProxyVersion:        "v1.20.4",
	}
}

func buildNodeCapacity(nodeConfig *utils.NodeConfig) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:              *resource.NewMilliQuantity(nodeConfig.NodeCapacity.CPU, resource.DecimalSI),
		v1.ResourceMemory:           *resource.NewQuantity(nodeConfig.NodeCapacity.Memory*1024*1024, resource.BinarySI),
		v1.ResourceEphemeralStorage: *resource.NewQuantity(nodeConfig.NodeCapacity.Storage*1024*1024*1024, resource.BinarySI),
		v1.ResourcePods:             *resource.NewQuantity(100, resource.DecimalSI),
		ResourceNvidiaGPU:           *resource.NewQuantity(nodeConfig.NodeCapacity.GPUNumber, resource.DecimalSI),
	}
}

func buildNodeAllocatable(nodeConfig *utils.NodeConfig) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:              *resource.NewMilliQuantity(nodeConfig.NodeCapacity.CPU, resource.DecimalSI),
		v1.ResourceMemory:           *resource.NewQuantity(nodeConfig.NodeCapacity.Memory*1024*1024, resource.BinarySI),
		v1.ResourceEphemeralStorage: *resource.NewQuantity(nodeConfig.NodeCapacity.Storage*1024*1024*1024, resource.BinarySI),
		v1.ResourcePods:             *resource.NewQuantity(100, resource.DecimalSI),
		ResourceNvidiaGPU:           *resource.NewQuantity(nodeConfig.NodeCapacity.GPUNumber, resource.DecimalSI),
	}
}
