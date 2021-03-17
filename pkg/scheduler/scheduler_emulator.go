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

package scheduler

import (
	"context"
	"fmt"
	"k8s.io/klog"

	gouuid "github.com/google/uuid"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/riverzhang/kubesim/pkg/utils"
)

func NewAppsEmulator(clientset clientset.Interface, ks *Kubesim) *AppsEmulator {
	return &AppsEmulator{
		Name: "AppsEmulator",
		Func: func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) *utils.SimulatorJob {
			Apps := &utils.AppGroup{
				EmulatorType: "AppsEmulator",
				Apps:         apps.PodRequestList,
			}
			job_id := gouuid.New().String()

			ks.fifoQueue.Push(job_id, Apps)

			job := &utils.SimulatorJob{
				Id: job_id,
			}
			return job
		},
		cache: ks,
	}
}

func NewHorizontalEmulator(clientset clientset.Interface, ks *Kubesim) *HorizontalEmulator {
	return &HorizontalEmulator{
		Name: "HorizontalEmulator",
		Func: func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, s *Kubesim) (*utils.SimulatorJob, error) {
			var newApps utils.MutiAppsRequest
			var usList []utils.PodRequest
			var newPodRequest utils.PodRequest
			for _, app := range apps.PodRequestList {
				if app.Kind == "deployment" {
					deployment, err := clientset.AppsV1().Deployments(app.Pod.Namespace).Get(context.TODO(), app.Pod.Name, metav1.GetOptions{})
					if err != nil {
						klog.Errorf("unable to get apps name %s: %v", app.Pod.Name, err)
						return nil, fmt.Errorf("unable to get apps name : %v", app.Pod.Name, err)
					}

					if app.Replicas-*deployment.Spec.Replicas <= 0 {
						return nil, fmt.Errorf("horizontal apps is stop,scale replicas is too samll.")
					}

					newPodRequest = utils.PodRequest{
						Pod:      app.Pod,
						Replicas: app.Replicas - *deployment.Spec.Replicas,
					}

				} else if app.Kind == "statefulsets" {
					statefulsets, err := clientset.AppsV1().StatefulSets(app.Pod.Namespace).Get(context.TODO(), app.Pod.Name, metav1.GetOptions{})
					if err != nil {
						klog.Errorf("unable to get apps name %s: %v", app.Pod.Name, err)
						return nil, fmt.Errorf("unable to get apps name : %v", app.Pod.Name, err)
					}

					if app.Replicas-*statefulsets.Spec.Replicas <= 0 {
						return nil, fmt.Errorf("horizontal apps is stop,scale replicas is too samll.")
					}

					newPodRequest = utils.PodRequest{
						Pod:      app.Pod,
						Replicas: app.Replicas - *statefulsets.Spec.Replicas,
					}
				}

				usList = append(usList, newPodRequest)
				newApps = utils.MutiAppsRequest{
					PodRequestList: usList,
				}

			}

			Apps := &utils.AppGroup{
				EmulatorType: "HorizontalEmulator",
				Apps:         newApps.PodRequestList,
			}

			job_id := gouuid.New().String()
			ks.fifoQueue.Push(job_id, Apps)

			job := &utils.SimulatorJob{
				Id: job_id,
			}
			return job, nil
		},
		cache: ks,
	}
}

func NewVerticalEmulator(clientset clientset.Interface, ks *Kubesim) *VerticalEmulator {
	return &VerticalEmulator{
		Name: "VerticalEmulator",
		Func: func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*utils.SimulatorJob, error) {
			var newApps utils.MutiAppsRequest
			var usList []utils.PodRequest
			var oldpods []v1.Pod
			for _, app := range apps.PodRequestList {
				us, err := clientset.AppsV1().Deployments(app.Pod.Namespace).Get(context.TODO(), app.Pod.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("unable to get apps name %s: %v", app.Pod.Name, err)
					return nil, fmt.Errorf("unable to get apps name : %v", app.Pod.Name, err)
				}

				labels := us.Spec.Selector.MatchLabels
				var label string
				if value, ok := labels["app"]; ok {
					label = "app=" + value
				}

				pods, err := ks.GetAppPod(app.Pod.Namespace, label)
				if err != nil {
					return nil, fmt.Errorf("unable to get pod: %v", err)
				}

				containers := app.Pod.Spec.Containers
				for _, pod := range pods {
					oldpods = append(oldpods, pod)
					var newpod *v1.Pod
					newpod = pod.DeepCopy()
					newpod.Spec.NodeName = ""
					newpod.Spec.Containers = containers
					Labels := map[string]string{}
					Labels["kubernetes.io/hostname"] = pod.Spec.NodeName
					newpod.Spec.NodeSelector = Labels
					newPodRequest := utils.PodRequest{
						Pod:      newpod,
						Replicas: 1,
					}
					usList = append(usList, newPodRequest)
					newApps = utils.MutiAppsRequest{
						PodRequestList: usList,
					}
				}
			}

			Apps := &utils.AppGroup{
				EmulatorType: "VerticalEmulator",
				Apps:         newApps.PodRequestList,
				OldPods:      oldpods,
			}
			job_id := gouuid.New().String()
			ks.fifoQueue.Push(job_id, Apps)

			job := &utils.SimulatorJob{
				Id: job_id,
			}
			return job, nil
		},
		cache: ks,
	}
}

func NewPredictionEmulator(clientset clientset.Interface, ks *Kubesim) *PredictionEmulator {
	return &PredictionEmulator{
		Name: "PredictionEmulator",
		Func: func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, s *Kubesim) *utils.SimulatorJob {
			Apps := &utils.AppGroup{
				EmulatorType: "Prediction",
				Nodes:        nodes,
				Apps:         apps.PodRequestList,
			}
			job_id := gouuid.New().String()
			ks.fifoQueue.Push(job_id, Apps)

			job := &utils.SimulatorJob{
				Id: job_id,
			}
			return job
		},
		cache: ks,
	}
}

func NewEmulatorReport(clientset clientset.Interface, ks *Kubesim) *EmulatorReport {
	return &EmulatorReport{
		Name: "EmulatorReport",
		Func: func(id string, ks *Kubesim) (*JobReport, error) {
			var status string
			var predictionnode *PredictionNodeReview
			var reportList []SchedulerEmulatorReview
			queue := ks.fifoQueue.Get()
			ks.reportLock.RLock()
			defer ks.reportLock.RUnlock()
			if reportlist, ok := ks.emulatorResult[id]; ok {
				if reportlist != nil {
					reportList = reportlist.ReportList
					for _, report := range reportList {
						if &report != nil {
							if report.Status.Reason.Type == "Unschedulable" {
								status = JobFailed
								break
							} else {
								status = JobSucceed
							}
						}
					}
				} else {
					status = JobStopped
				}
			} else if _, ok = queue[id]; ok {
				status = JobQueuing
			} else {
				status = JobProcessing
			}

			if prediction, ok := ks.predictionNode[id]; ok {
				predictionnode = prediction
			} else {
				predictionnode = nil
			}

			jobReport := JobReport{
				JobID:          id,
				Status:         status,
				Report:         reportList,
				PredictionNode: predictionnode,
			}

			return &jobReport, nil

		},
		cache: ks,
	}
}

func NewAddNodesEmulator(clientset clientset.Interface, ks *Kubesim) *AddNodesEmulator {
	return &AddNodesEmulator{
		Name: "nodesimulator",
		Func: func(nodeConfig *utils.NodeConfig, s *Kubesim) (*utils.NodeInfoList, error) {
			// Run create node simlator process
			_, nodeinfolist, err := ks.AddNodes(nodeConfig)
			if err != nil {
				return nil, fmt.Errorf("Unable to create simulator node: %v", err)
			}
			return nodeinfolist, nil
		},
		cache: ks,
	}
}

func NewListNodesEmulator(clientset clientset.Interface, ks *Kubesim) *ListNodesEmulator {
	return &ListNodesEmulator{
		Name: "nodesimulator",
		Func: func(ks *Kubesim) (*utils.NodeInfoList, error) {
			nodelist, err := ks.ListNodes()
			if err != nil {
				return nil, fmt.Errorf("Unable to list simulator node: %v", err)
			}
			return nodelist, nil
		},
		cache: ks,
	}
}

func NewListNodeInfoEmulator(clientset clientset.Interface, ks *Kubesim) *ListNodeInfoEmulator {
	return &ListNodeInfoEmulator{
		Name: "nodeinfosimulator",
		Func: func(ks *Kubesim) (*[]utils.NodeInfo, error) {
			nodeinfolist, err := ks.GetNodeInfo()
			if err != nil {
				return nil, fmt.Errorf("Unable to list simulator node: %v", err)
			}
			return nodeinfolist, nil
		},
		cache: ks,
	}
}

func NewDeleteNodesEmulator(clientset clientset.Interface, ks *Kubesim) *DeleteNodesEmulator {
	return &DeleteNodesEmulator{
		Name: "nodesimulator",
		Func: func(ks *Kubesim) error {
			err := ks.DeleteNodes()
			if err != nil {
				return fmt.Errorf("Unable to delete simulator node")
			}

			return nil
		},
		cache: ks,
	}
}

func NewDeprecatedPredictionEmulator(clientset clientset.Interface, ks *Kubesim) *DeprecatedPredictionEmulator {
	return &DeprecatedPredictionEmulator{
		Name: "deprecatedschedulerprediction",
		Func: func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*SchedulerPrediction, error) {
			var predictionReport SchedulerPrediction
			var successAppsName []string
			var successCount int32
			var failedCount int32
			var failedMessage string
			var predictionGo bool = true

		Loop:
			for _, apps := range apps.PodRequestList {
				ks = ks.UpdatePodRequest(apps.Pod, apps.Replicas)
				//Run scheduler pod  emulator process
				err := ks.SchedulerPod()
				if err != nil {
					return nil, fmt.Errorf("Unable to create next pod to schedule: %v", err)
				}
				report := ks.Report()
				ks.CleanStatusPods()
				if report.Status.Reason.Type == "Unschedulable" {
					failedMessage = report.Status.Reason.Message
					predictionGo = false
					break Loop
				}
				successAppsName = append(successAppsName, apps.Pod.Name)
				successCount++
				continue
			}

			failedCount = int32(len(apps.PodRequestList)) - successCount

			// Clean pod
			ks.CleanPod()

			predictionReport = SchedulerPrediction{
				SuccessAppsName:      successAppsName,
				SuccessAppsCount:     successCount,
				FailedAppsCount:      failedCount,
				FirstAppfailedReason: failedMessage,
				PredictionGo:         predictionGo,
			}

			return &predictionReport, nil
		},
		cache: ks,
	}
}

func NewDeprecatedAppsEmulator(clientset clientset.Interface, ks *Kubesim) *DeprecatedAppsEmulator {
	return &DeprecatedAppsEmulator{
		Name: "scheduleremulator",
		Func: func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*SchedulerEmulatorReviewList, error) {
			var reportList SchedulerEmulatorReviewList
			reportlist := []SchedulerEmulatorReview{}

			// Create apps
			for _, app := range apps.PodRequestList {
				ks = ks.UpdatePodRequest(app.Pod, app.Replicas)
				//Run scheduler pod  emulator process
				err := ks.SchedulerPod()
				if err != nil {
					return nil, fmt.Errorf("Unable to create next pod to schedule: %v", err)
				}
				report := ks.Report()
				ks.CleanStatusPods()
				reportlist = append(reportlist, *report)
				reportList = SchedulerEmulatorReviewList{
					ReportList: reportlist,
				}
			}

			// Clean pod
			ks.CleanPod()
			ks.CleanStatusPods()

			return &reportList, nil
		},
		cache: ks,
	}
}
