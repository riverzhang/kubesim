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

package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	_ "k8s.io/kubernetes/pkg/scheduler/algorithmprovider"
	//"k8s.io/kubernetes/pkg/scheduler/framework"
	fruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
)

const DefaultSchedulerName = "default-scheduler"

func PrintPod(pod *v1.Pod, format string) error {
	var contentType string
	switch format {
	case "json":
		contentType = runtime.ContentTypeJSON
	case "yaml":
		contentType = "application/yaml"
	default:
		contentType = "application/yaml"
	}

	info, ok := runtime.SerializerInfoForMediaType(legacyscheme.Codecs.SupportedMediaTypes(), contentType)
	if !ok {
		return fmt.Errorf("serializer for %s not registered", contentType)
	}
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	encoder := legacyscheme.Codecs.EncoderForVersion(info.Serializer, gvr.GroupVersion())
	stream, err := runtime.Encode(encoder, pod)

	if err != nil {
		return fmt.Errorf("Failed to create pod: %v", err)
	}
	fmt.Print(string(stream))
	return nil
}

func GetMasterFromKubeConfig(filename string) (string, error) {
	config, err := clientcmd.LoadFromFile(filename)
	if err != nil {
		return "", fmt.Errorf("can not load kubeconfig file: %v", err)
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return "", fmt.Errorf("Failed to get master address from kubeconfig")
	}

	if val, ok := config.Clusters[context.Cluster]; ok {
		return val.Server, nil
	}
	return "", fmt.Errorf("Failed to get master address from kubeconfig")
}

// Option configures a framework.Registry.
type Option func(fruntime.Registry) error

type SimulatorJob struct {
	Id string `json:"id"`
}

type NodeInfo struct {
	Name          string `json:"name"`
	UsedCPU       int64  `json:"usedCPU"`
	UsedMemory    int64  `json:"usedMemory"`
	CPU           int64  `json:"cpu"`
	Memory        int64  `json:"memory"`
	CPUPercent    string `json:"cpuPercent"`
	MemoryPercent string `json:"memoryPercent"`
	PodNumber     int64  `json:"podNumber"`
	//Pods          []*v1.Pod `json:pods`
}

type NodeInfoList struct {
	NodeNumber        int            `json:"nodeNumber"`
	PodNumber         int            `json:"podNumber"`
	RunningPod        int            `json:"runingPod"`
	Status            v1.PodPhase    `json:"status"`
	NotRunningPod     int            `json:"notRuningPod"`
	NodeInfoPodNumber int            `json:"nodeInfoPodNumber"`
	NodeInfoNumber    int            `json:"nodeInfoNumber"`
	NotNodeInfoNumber int            `json:"notNodeInfoNumber"`
	BindFailed        int            `json:"bindFailed"`
	BindSuccess       int            `json:"bindSuccess"`
	NodePods          map[string]int `json:"nodePods"`
	NodeInfoPods      map[string]int `json:"nodeInfoPods"`
	NodeList          *v1.NodeList   `json:"nodeList"`
}

type NodeCapacity struct {
	GPUNumber int64 `json:"gpunumber"`
	CPU       int64 `json:"cpu"`
	Memory    int64 `json:"memory"`
	Storage   int64 `json:"storage"`
}

type NodeConfig struct {
	NodeNumber   int               `json:"nodeNumber"`
	NodeCapacity *NodeCapacity     `json:"nodeCapacity"`
	NodeLabels   map[string]string `json:"nodeLabels"`
}

type PodsConfig struct {
	Op   string     `json:"op"`
	Pods []PodsInfo `json:"pods"`
}

type PodsInfo struct {
	Name           string `json:"name"`
	NameSpace      string `json:"namespace"`
	TargetNodeName string `json:"targetnodename"`
}

type PodRequest struct {
	Pod      *v1.Pod
	Replicas int32  `json:"Replicas"`
	Kind     string `json:"kind"`
}

type MutiAppsRequest struct {
	PodRequestList []PodRequest
}

type Apps struct {
	Nodes           []NodeConfig          `json:"nodes"`
	DeployApps      []*appsv1.Deployment  `json:"deployApps"`
	StatefulsetApps []*appsv1.StatefulSet `json:"statefulsetApps"`
}

type AppGroup struct {
	EmulatorType string       `json:"emulatorType"`
	Nodes        []NodeConfig `json:"nodes"`
	Apps         []PodRequest `json:"apps"`
	OldPods      []v1.Pod     `json:"odlPods"`
	PodsConfig   *PodsConfig  `json:"podsConfig"`
}

func GetUID(uid string) types.UID {
	if len(uid) == 0 {
		klog.Warningf("the uuid is none, will be auto generate for it.")
		uid = uuid.New().String()
	}

	return types.UID(uid)
}

func getDefaultSchedulerName(dsn string) string {
	if len(dsn) == 0 {
		dsn = DefaultSchedulerName
	}

	return dsn
}

func PodsConfigParse(data io.Reader) (*PodsConfig, error) {
	var podsConfig PodsConfig
	if err := json.NewDecoder(data).Decode(&podsConfig); err != nil {
		return nil, fmt.Errorf("Parse to PodsConfig, json unmarshal err: %v", err)
	}

	return &podsConfig, nil
}

func NodeConfigParse(data io.Reader) (*NodeConfig, error) {
	var node NodeConfig
	if err := json.NewDecoder(data).Decode(&node); err != nil {
		return nil, fmt.Errorf("Parse to NodeConfig, json unmarshal err: %v", err)
	}

	return &node, nil
}

func ParseDeployToPod(deploy *appsv1.Deployment) (*v1.Pod, int32, error) {
	replicas := int32(0)
	if strings.ToLower(deploy.Kind) != "deployment" {
		klog.Errorf("Invalid Deployment resource %q", deploy.Name)
		return nil, 0, fmt.Errorf("invalid Deployment resource")
	}

	// if the default scheduler name is empty, use "default-scheduler"
	if len(deploy.Spec.Template.Spec.SchedulerName) == 0 {
		deploy.Spec.Template.Spec.SchedulerName = getDefaultSchedulerName(DefaultSchedulerName)
	}

	if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas == 0 {
		klog.Warningf("warn: found rc %v, replicas count is %v, USE the default value 1.",
			deploy.Name, *deploy.Spec.Replicas)
		replicas = int32(1)
	} else {
		replicas = *deploy.Spec.Replicas
	}

	v1Pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        deploy.Name,
			UID:         GetUID(""),
			Namespace:   deploy.Namespace,
			Annotations: deploy.Annotations,
			Labels:      deploy.Spec.Template.Labels,
		},
		Spec: deploy.Spec.Template.Spec,
	}

	return &v1Pod, replicas, nil
}

// ParseToPod, convert deployment or statefulset to v1.pod
func ParseStatefulSetToPod(statefulset *appsv1.StatefulSet) (*v1.Pod, int32, error) {
	replicas := int32(0)

	if strings.ToLower(statefulset.Kind) != "statefulset" {
		klog.Errorf("Invalid statefuleset resource %q", statefulset.Name)
		return nil, 0, fmt.Errorf("invalid StatefulSet resource")
	}

	// if the default scheduler name is empty, use "default-scheduler"
	if len(statefulset.Spec.Template.Spec.SchedulerName) == 0 {
		statefulset.Spec.Template.Spec.SchedulerName = getDefaultSchedulerName(DefaultSchedulerName)
	}

	if statefulset.Spec.Replicas == nil || *statefulset.Spec.Replicas == 0 {
		klog.Warningf("warn: found rc %v, replicas count is %v, USE the default value 1.",
			statefulset.Name, *statefulset.Spec.Replicas)
		replicas = int32(1)
	} else {
		replicas = *statefulset.Spec.Replicas
	}

	v1Pod := v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        statefulset.Name,
			UID:         GetUID(""),
			Namespace:   statefulset.Namespace,
			Annotations: statefulset.Annotations,
			Labels:      statefulset.Spec.Template.Labels,
		},
		Spec: statefulset.Spec.Template.Spec,
	}

	return &v1Pod, replicas, nil
}

// ParseToPod, convert deployment or statefulset to v1.pod
func ParseToPod(rc string, data io.Reader) (*v1.Pod, int32, error) {
	var v1Pod *v1.Pod
	var replicas int32
	var err error

	rc = strings.ToLower(rc)
	if rc == "deployment" {
		var dm appsv1.Deployment
		if err := json.NewDecoder(data).Decode(&dm); err != nil {
			v1Pod = nil
			replicas = 0
			err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
		}
		v1Pod, replicas, err = ParseDeployToPod(&dm)
		if err != nil {
			v1Pod = nil
			replicas = 0
			err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
		}
	} else if rc == "statefulset" {
		var dm appsv1.StatefulSet
		if err := json.NewDecoder(data).Decode(&dm); err != nil {
			v1Pod = nil
			replicas = 0
			err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
		}
		v1Pod, replicas, err = ParseStatefulSetToPod(&dm)
		if err != nil {
			v1Pod = nil
			replicas = 0
			err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
		}
	} else {
		v1Pod = nil
		replicas = 0
		err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
	}

	return v1Pod, replicas, nil
}

// ParseToPod, convert deployment or statefulset to v1.pod
func AppsParseToPod(rc string, data io.Reader) ([]NodeConfig, MutiAppsRequest, error) {
	var v1Pod *v1.Pod
	var replicas int32
	var err error
	var appsListRequest MutiAppsRequest
	var nodes []NodeConfig

	rc = strings.ToLower(rc)
	if rc == "deployment" {
		var dm Apps
		if err = json.NewDecoder(data).Decode(&dm); err != nil {
			v1Pod = nil
			replicas = 0
			err = fmt.Errorf("ParseToPod: json unmarshal err:%v", err)
			return nil, appsListRequest, err
		}
		nodes = dm.Nodes
		for _, deploy := range dm.DeployApps {
			v1Pod, replicas, err = ParseDeployToPod(deploy)
			if err != nil {
				err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
				return nil, appsListRequest, err
			}
			podSpecResponse := PodRequest{
				Pod:      v1Pod,
				Replicas: replicas,
				Kind:     rc,
			}
			usList := appsListRequest.PodRequestList
			usList = append(usList, podSpecResponse)
			appsListRequest = MutiAppsRequest{
				PodRequestList: usList,
			}
		}
	} else if rc == "statefulset" {
		var dm Apps
		if err = json.NewDecoder(data).Decode(&dm); err != nil {
			v1Pod = nil
			replicas = 0
			err = fmt.Errorf("ParseToPod: json unmarshal err:%v", err)
			return nil, appsListRequest, err
		}
		nodes = dm.Nodes
		for _, stateful := range dm.StatefulsetApps {
			v1Pod, replicas, err = ParseStatefulSetToPod(stateful)
			if err != nil {
				err = fmt.Errorf("ParseToPod, json unmarshal err: %v", err)
				return nil, appsListRequest, err
			}
			podSpecResponse := PodRequest{
				Pod:      v1Pod,
				Replicas: replicas,
				Kind:     rc,
			}
			statefulList := appsListRequest.PodRequestList
			statefulList = append(statefulList, podSpecResponse)
			appsListRequest = MutiAppsRequest{
				PodRequestList: statefulList,
			}
		}
	}
	return nodes, appsListRequest, err
}
