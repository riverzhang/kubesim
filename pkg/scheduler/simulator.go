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
	"net/http"
	goruntime "runtime"
	"sync"
	"time"

	gouuid "github.com/google/uuid"
	uuid "github.com/satori/go.uuid"

	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/apiserver/pkg/server/routes"
	"k8s.io/client-go/informers"
	kubeinformers "k8s.io/client-go/informers"
	externalclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"k8s.io/component-base/configz"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	"k8s.io/kubernetes/pkg/scheduler"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"k8s.io/kubernetes/pkg/scheduler/metrics/resources"
	"k8s.io/kubernetes/pkg/scheduler/profile"

	nodeconfig "github.com/riverzhang/kubesim/pkg/node"
	"github.com/riverzhang/kubesim/pkg/queue"
	"github.com/riverzhang/kubesim/pkg/strategy"
	"github.com/riverzhang/kubesim/pkg/utils"
)

const (
	podProvisioner = "kubesim.kubernetes.io/provisioned"
	resyncPeriod   = 30 * time.Second
	accessNum      = 45
)

type Kubesim struct {
	// emulation strategy
	strategy        strategy.Strategy
	addNodeStrategy string

	externalkubeclient externalclientset.Interface
	informerFactory    informers.SharedInformerFactory

	// schedulers
	schedulers           map[string]*scheduler.Scheduler
	defaultSchedulerName string
	defaultSchedulerConf *schedconfig.CompletedConfig

	// pod to schedule
	simulatedPod     *v1.Pod
	lastSimulatedPod *v1.Pod
	maxSimulated     int32
	simulated        int32

	// lock for report cache(emulatorResult and predictionNode)
	reportLock     sync.RWMutex
	status         Status
	report         *SchedulerEmulatorReview
	emulatorResult map[string]*SchedulerEmulatorReviewList
	predictionNode map[string]*PredictionNodeReview

	// simulator pod to fifo queue
	fifoQueue queue.PodQueue

	// real cluster client
	KubeClient externalclientset.Interface

	// recorder used to send event to real cluster
	Recorder    record.EventRecorder
	Broadcaster record.EventBroadcaster

	// analysis limitation
	informerStopCh chan struct{}
	// schedulers channel
	schedulerCh chan struct{}

	// stop the analysis
	stop      chan struct{}
	stopMux   sync.RWMutex
	stopped   bool
	closedMux sync.RWMutex
	closed    bool
}

// capture all scheduled pods with reason why the analysis could not continue
type Status struct {
	Pods       []*v1.Pod
	StopReason string
}

func (ks *Kubesim) Report() *SchedulerEmulatorReview {
	// Preparation before pod sequence scheduling is done
	pods := make([]*v1.Pod, 0)
	pods = append(pods, ks.simulatedPod)
	ks.report = GetReport(pods, ks.status, ks.maxSimulated)
	//ks.report.Spec.Replicas = int32(ks.maxSimulated)

	return ks.report
}

// clean status pods
func (ks *Kubesim) CleanStatusPods() {
	ks.status.Pods = []*v1.Pod{}
	ks.report = &SchedulerEmulatorReview{}
}

// Pod request
func (ks *Kubesim) UpdatePodRequest(pod *v1.Pod, replicas int32) *Kubesim {
	ks.closedMux.Lock()
	defer ks.closedMux.Unlock()
	ks.simulatedPod = pod
	ks.maxSimulated = replicas
	ks.simulated = 0
	ks.stop = make(chan struct{})
	ks.informerStopCh = make(chan struct{})
	ks.schedulerCh = make(chan struct{})

	return ks
}

// Scheduler a simulated pod
func (ks *Kubesim) SchedulerPod() error {
	err := ks.nextPod()
	if err != nil {
		ks.Close()
		close(ks.stop)
		return fmt.Errorf("nable to create next pod to schedule: %v", err)
	}
	<-ks.stop
	close(ks.stop)
	return nil
}

func (ks *Kubesim) SyncWithClient(client externalclientset.Interface) error {
	podItems, err := client.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list pods: %v", err)
	}

	for _, item := range podItems.Items {
		if item.Status.Phase != v1.PodPending {
			if _, err := ks.externalkubeclient.CoreV1().Pods(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("unable to copy pod: %v", err)
			}
		}
	}

	nodeItems, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list nodes: %v", err)
	}

	for _, item := range nodeItems.Items {
		if _, err := ks.externalkubeclient.CoreV1().Nodes().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy node: %v", err)
		}
	}

	serviceItems, err := client.CoreV1().Services(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list services: %v", err)
	}

	for _, item := range serviceItems.Items {
		if _, err := ks.externalkubeclient.CoreV1().Services(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy service: %v", err)
		}
	}

	pvcItems, err := client.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list pvcs: %v", err)
	}

	for _, item := range pvcItems.Items {
		if _, err := ks.externalkubeclient.CoreV1().PersistentVolumeClaims(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy pvc: %v", err)
		}
	}

	pvItems, err := client.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list pvs: %v", err)
	}

	for _, item := range pvItems.Items {
		if _, err := ks.externalkubeclient.CoreV1().PersistentVolumes().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy pv: %v", err)
		}
	}

	rcItems, err := client.CoreV1().ReplicationControllers(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list RCs: %v", err)
	}

	for _, item := range rcItems.Items {
		if _, err := ks.externalkubeclient.CoreV1().ReplicationControllers(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy RC: %v", err)
		}
	}

	pdbItems, err := client.PolicyV1beta1().PodDisruptionBudgets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list PDBs: %v", err)
	}

	for _, item := range pdbItems.Items {
		if _, err := ks.externalkubeclient.PolicyV1beta1().PodDisruptionBudgets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy PDB: %v", err)
		}
	}

	replicaSetItems, err := client.AppsV1().ReplicaSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list replicas sets: %v", err)
	}

	for _, item := range replicaSetItems.Items {
		if _, err := ks.externalkubeclient.AppsV1().ReplicaSets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy replica set: %v", err)
		}
	}

	statefulSetItems, err := client.AppsV1().StatefulSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list stateful sets: %v", err)
	}

	for _, item := range statefulSetItems.Items {
		if _, err := ks.externalkubeclient.AppsV1().StatefulSets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy stateful set: %v", err)
		}
	}

	storageClassesItems, err := client.StorageV1().StorageClasses().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list storage classes: %v", err)
	}

	for _, item := range storageClassesItems.Items {
		if _, err := ks.externalkubeclient.StorageV1().StorageClasses().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy storage class: %v", err)
		}
	}

	CSINodeItems, err := client.StorageV1().CSINodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list csinode: %v", err)
	}

	for _, item := range CSINodeItems.Items {
		if _, err := ks.externalkubeclient.StorageV1().CSINodes().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy csinode: %v", err)
		}
	}

	CSIDriverItems, err := client.StorageV1().CSIDrivers().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list csi drivers: %v", err)
	}

	for _, item := range CSIDriverItems.Items {
		if _, err := ks.externalkubeclient.StorageV1().CSIDrivers().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy csi dirvers: %v", err)
		}
	}

	VolumeAttachmentsItems, err := client.StorageV1().VolumeAttachments().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list volumeAttachments: %v", err)
	}

	for _, item := range VolumeAttachmentsItems.Items {
		if _, err := ks.externalkubeclient.StorageV1().VolumeAttachments().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy volumeAttachments: %v", err)
		}
	}

	return nil
}

func (ks *Kubesim) PVWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	pvInformer := kubeInformerFactory.Core().V1().PersistentVolumes().Informer()

	pvInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.PersistentVolume:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*v1.PersistentVolume); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if pv, ok := obj.(*v1.PersistentVolume); ok {
					if _, err := ks.externalkubeclient.CoreV1().PersistentVolumes().Get(context.TODO(), pv.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.CoreV1().PersistentVolumes().Create(context.TODO(), pv, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create pv %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if pv, ok := obj.(*v1.PersistentVolume); ok {
					if err := ks.externalkubeclient.CoreV1().PersistentVolumes().Delete(context.TODO(), pv.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete pv: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) PVCWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	pvcInformer := kubeInformerFactory.Core().V1().PersistentVolumeClaims().Informer()

	pvcInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.PersistentVolumeClaim:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*v1.PersistentVolumeClaim); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if pvc, ok := obj.(*v1.PersistentVolumeClaim); ok {
					if _, err := ks.externalkubeclient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(context.TODO(), pvc.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create pvc %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if pvc, ok := obj.(*v1.PersistentVolumeClaim); ok {
					if err := ks.externalkubeclient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(context.TODO(), pvc.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete pvc: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) StorageClassWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	scInformer := kubeInformerFactory.Storage().V1().StorageClasses().Informer()

	scInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *storagev1.StorageClass:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*storagev1.StorageClass); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if sc, ok := obj.(*storagev1.StorageClass); ok {
					if _, err := ks.externalkubeclient.StorageV1().StorageClasses().Get(context.TODO(), sc.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.StorageV1().StorageClasses().Create(context.TODO(), sc, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create stroage class %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if sc, ok := obj.(*storagev1.StorageClass); ok {
					if err := ks.externalkubeclient.StorageV1().StorageClasses().Delete(context.TODO(), sc.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete stroage class: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) CSINodeWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	csinodeInformer := kubeInformerFactory.Storage().V1().CSINodes().Informer()

	csinodeInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *storagev1.CSINode:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*storagev1.CSINode); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if csinode, ok := obj.(*storagev1.CSINode); ok {
					if _, err := ks.externalkubeclient.StorageV1().CSINodes().Get(context.TODO(), csinode.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.StorageV1().CSINodes().Create(context.TODO(), csinode, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create stroage class %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if csinode, ok := obj.(*storagev1.CSINode); ok {
					if err := ks.externalkubeclient.StorageV1().CSINodes().Delete(context.TODO(), csinode.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete stroage class: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) CSIDriverWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	csidriverInformer := kubeInformerFactory.Storage().V1().CSIDrivers().Informer()

	csidriverInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *storagev1.CSIDriver:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*storagev1.CSIDriver); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if csidriver, ok := obj.(*storagev1.CSIDriver); ok {
					if _, err := ks.externalkubeclient.StorageV1().CSIDrivers().Get(context.TODO(), csidriver.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.StorageV1().CSIDrivers().Create(context.TODO(), csidriver, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create stroage driver %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if csidriver, ok := obj.(*storagev1.CSIDriver); ok {
					if err := ks.externalkubeclient.StorageV1().CSIDrivers().Delete(context.TODO(), csidriver.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete stroage driver: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) VolumeAttachmentsWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	volumeAttachmentsInformer := kubeInformerFactory.Storage().V1().VolumeAttachments().Informer()

	volumeAttachmentsInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *storagev1.VolumeAttachment:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*storagev1.VolumeAttachment); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if va, ok := obj.(*storagev1.VolumeAttachment); ok {
					if _, err := ks.externalkubeclient.StorageV1().VolumeAttachments().Get(context.TODO(), va.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.StorageV1().VolumeAttachments().Create(context.TODO(), va, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create stroage driver %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if va, ok := obj.(*storagev1.VolumeAttachment); ok {
					if err := ks.externalkubeclient.StorageV1().VolumeAttachments().Delete(context.TODO(), va.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete stroage driver: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) NodeWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	nodeInformer := kubeInformerFactory.Core().V1().Nodes().Informer()

	nodeInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.Node:
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*v1.Node); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if node, ok := obj.(*v1.Node); ok {
					if _, err := ks.externalkubeclient.CoreV1().Nodes().Get(context.TODO(), node.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create node %v", err)
						}
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if node, ok := obj.(*v1.Node); ok {
					if err := ks.externalkubeclient.CoreV1().Nodes().Delete(context.TODO(), node.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete node: %v", err)
					}
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				if newnode, ok := newObj.(*v1.Node); ok {
					if _, err := ks.externalkubeclient.CoreV1().Nodes().Update(context.TODO(), newnode, metav1.UpdateOptions{}); err != nil {
						klog.Errorf("unable to update node: %v", err)
					}
				}
			},
		},
	})
	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) PodWatch(client externalclientset.Interface) error {

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, resyncPeriod)
	podInformer := kubeInformerFactory.Core().V1().Pods().Informer()

	podInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *v1.Pod:
				if len(t.Spec.NodeName) > 0 || len(t.Status.NominatedNodeName) > 0 {
					return true
				}
				return false
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*v1.Pod); ok {
					return true
				}
				return false
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if pod, ok := obj.(*v1.Pod); ok {
					if _, err := ks.externalkubeclient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.ObjectMeta.Name, metav1.GetOptions{}); err != nil {
						if _, err := ks.externalkubeclient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{}); err != nil {
							klog.Errorf("unable to create pod %v", err)
						}

					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				if pod, ok := obj.(*v1.Pod); ok {
					if err := ks.externalkubeclient.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
						klog.Errorf("unable to delete pod: %v", err)
					}
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				newPod, _ := newObj.(*v1.Pod)
				oldPod, _ := oldObj.(*v1.Pod)
				if &newPod.Spec != &oldPod.Spec {
					if _, err := ks.externalkubeclient.CoreV1().Pods(newPod.Namespace).Update(context.TODO(), newPod, metav1.UpdateOptions{}); err != nil {
						klog.Warning("unable to update pod: %v", err)
					}
				}
			},
		},
	})

	stop := make(chan struct{})
	//defer close(stop)
	go kubeInformerFactory.Start(stop)

	return nil
}

func (ks *Kubesim) Bind(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string, schedulerName string) *framework.Status {
	// run the pod through strategy
	pod, err := ks.externalkubeclient.CoreV1().Pods(p.Namespace).Get(context.TODO(), p.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("unable to bind pod: %v", err)
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to bind: %v", err))
	}
	updatedPod := pod.DeepCopy()
	updatedPod.Spec.NodeName = nodeName
	updatedPod.Status.Phase = v1.PodRunning

	// TODO(riverzhang): rename Add to Update as this actually updates the scheduled pod
	if err := ks.strategy.Add(updatedPod); err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to recompute new cluster state: %v", err))
	}

	ks.status.Pods = append(ks.status.Pods, updatedPod)

	if ks.maxSimulated > 0 && ks.simulated >= ks.maxSimulated {
		ks.status.StopReason = fmt.Sprintf("Schedulable: Schedule the number of simulated pods: %v", ks.maxSimulated)
		ks.Close()
		ks.stop <- struct{}{}
		return nil
	}

	// all good, create another pod
	if err := ks.nextPod(); err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to create next pod to schedule: %v", err))
	}
	return nil
}

func (ks *Kubesim) Close() {
	ks.closedMux.Lock()
	defer ks.closedMux.Unlock()

	if ks.closed {
		return
	}

	close(ks.schedulerCh)
	close(ks.informerStopCh)
	ks.closed = true
}

func (ks *Kubesim) Update(pod *v1.Pod, podCondition *v1.PodCondition, schedulerName string) error {
	stop := podCondition.Type == v1.PodScheduled && podCondition.Status == v1.ConditionFalse && podCondition.Reason == "Unschedulable"

	// Only for pending pods provisioned by kubesim
	if stop && metav1.HasAnnotation(pod.ObjectMeta, podProvisioner) {
		ks.status.StopReason = fmt.Sprintf("%v: %v", podCondition.Reason, podCondition.Message)
		ks.Close()
		// The Update function can be run more than once before any corresponding
		// scheduler is closed. The behaviour is implementation specific
		ks.stopMux.Lock()
		defer ks.stopMux.Unlock()
		ks.stopped = true
		ks.CleanPendingPod(pod)
		ks.stop <- struct{}{}
	}
	return nil
}

func (ks *Kubesim) nextPod() error {
	pod := v1.Pod{}
	pod = *ks.simulatedPod.DeepCopy()
	// reset any node designation set
	pod.Spec.NodeName = ""
	// use simulated pod name with an index to construct the name
	pod.ObjectMeta.Name = fmt.Sprintf("%v-%v", ks.simulatedPod.Name, types.UID(uuid.NewV4().String())[0:7])
	pod.ObjectMeta.UID = types.UID(uuid.NewV4().String())
	pod.Spec.SchedulerName = ks.defaultSchedulerName

	// Add pod provisioner annotation
	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = map[string]string{}
	}

	// Stores the scheduler name
	pod.ObjectMeta.Annotations[podProvisioner] = ks.defaultSchedulerName

	if pod.ObjectMeta.Labels == nil {
		pod.ObjectMeta.Labels = map[string]string{}
	}
	pod.ObjectMeta.Labels["provisioner"] = "kubesim"

	ks.simulated++
	ks.lastSimulatedPod = &pod

	_, err := ks.externalkubeclient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("unable to run emulator pod: %v", err)
	}
	return err
}

func (ks *Kubesim) Run() error {
	// Start all informers.
	ks.informerFactory.Start(ks.informerStopCh)
	ks.informerFactory.WaitForCacheSync(ks.informerStopCh)

	// start recorder
	ks.Broadcaster.StartRecordingToSink((&clientcorev1.EventSinkImpl{Interface: ks.KubeClient.CoreV1().Events("")}))

	ctx, _ := context.WithCancel(context.Background())

	// TODO(riverzhang): remove all pods that are not scheduled yet
	for _, scheduler := range ks.schedulers {
		go func() {
			scheduler.Run(ctx)
		}()
	}

	// wait some time before at least nodes are populated
	// TODO(riverzhang); find a better way how to do this or at least decrease it to <100ms
	time.Sleep(100 * time.Millisecond)

	go func() {
		for {
			// For each pod popped from the front of the queue, ...
			appgroup, key, err := ks.fifoQueue.Pop() // not pop a pod here; it may fail to any node
			if err != nil {
				if err == queue.ErrEmptyQueue {
					continue
				} else {
					klog.Errorf("Unexpected error raised by Queueu.Pop()")
				}
			}

			///var reportList *SchedulerEmulatorReviewList
			switch {
			case appgroup.EmulatorType == "AppsEmulator":
				// run create apps
				_, err = ks.appsEmulator(appgroup, key)
				if err != nil {
					klog.Errorf("apps emulator failed : %v", err)
				}
			case appgroup.EmulatorType == "AppsPodEmulator":
				// run create app
				_, err = ks.appsPodEmulator(appgroup, key)
				if err != nil {
					klog.Errorf("apps emulator failed : %v", err)
				}
			case appgroup.EmulatorType == "HorizontalEmulator":
				// run create apps
				_, err = ks.HorizontalEmulator(appgroup, key)
				if err != nil {
					klog.Errorf("horizontal emulator failed : %v", err)
				}
			case appgroup.EmulatorType == "VerticalEmulator":
				// run create apps
				_, err = ks.VerticalEmulator(appgroup, key)
				if err != nil {
					klog.Errorf("vertical emulator failed : %v", err)
				}
			case appgroup.EmulatorType == "Prediction":
				// run prediction
				_, err = ks.PredictionEmulator(appgroup, key)
				if err != nil {
					klog.Errorf("prediction node  failed : %v", err)
				}
			}
		}
	}()

	return nil
}

func (ks *Kubesim) appsEmulator(appgroup *utils.AppGroup, key string) (*SchedulerEmulatorReviewList, error) {
	var reportList SchedulerEmulatorReviewList
	reportlist := []SchedulerEmulatorReview{}

	// Create apps
	for _, app := range appgroup.Apps {
		ks := ks.UpdatePodRequest(app.Pod, app.Replicas)
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

	ks.reportLock.Lock()
	ks.emulatorResult[key] = &reportList
	ks.reportLock.Unlock()

	return &reportList, nil
}

func (ks *Kubesim) appsPodEmulator(appgroup *utils.AppGroup, key string) (*SchedulerEmulatorReviewList, error) {
	var reportList SchedulerEmulatorReviewList
	reportlist := []SchedulerEmulatorReview{}

	// get old  and new pods
	oldPods, newPods := ks.GetAppsPod(appgroup.PodsConfig)

	// delete old pods
	for _, oldpod := range oldPods {
		err := ks.DeleteAppsPod(oldpod.Namespace, oldpod.Name)
		if err != nil {
			return nil, fmt.Errorf("unable create pod : %v", err)
		}
	}

	// Create apps
	for _, newPod := range newPods {
		v1Pod := v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        newPod.Name,
				Namespace:   newPod.Namespace,
				Annotations: newPod.Annotations,
				Labels:      newPod.Labels,
			},
			Spec: newPod.Spec,
		}

		ks = ks.UpdatePodRequest(&v1Pod, 1)
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

	// Clean new pod
	ks.CleanPod()
	ks.CleanStatusPods()

	// Create old pods
	for _, oldpod := range oldPods {
		err := ks.CreateAppsPod(oldpod)
		if err != nil {
			return nil, fmt.Errorf("unable create pod : %v", err)
		}
	}

	ks.reportLock.Lock()
	ks.emulatorResult[key] = &reportList
	ks.reportLock.Unlock()

	return &reportList, nil
}

func (ks *Kubesim) HorizontalEmulator(appgroup *utils.AppGroup, key string) (*SchedulerEmulatorReviewList, error) {
	var reportList SchedulerEmulatorReviewList
	reportlist := []SchedulerEmulatorReview{}

	// Create apps
	for _, app := range appgroup.Apps {
		ks := ks.UpdatePodRequest(app.Pod, app.Replicas)
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

	ks.reportLock.Lock()
	ks.emulatorResult[key] = &reportList
	ks.reportLock.Unlock()
	return &reportList, nil
}

func (ks *Kubesim) VerticalEmulator(appgroup *utils.AppGroup, key string) (*SchedulerEmulatorReviewList, error) {
	var reportList SchedulerEmulatorReviewList
	reportlist := []SchedulerEmulatorReview{}

	// delete old pods
	for _, pod := range appgroup.OldPods {
		err := ks.DeleteAppsPod(pod.Namespace, pod.Name)
		if err != nil {
			return nil, fmt.Errorf("unable delete old pod")
		}
	}

	// Create apps
	for _, app := range appgroup.Apps {
		ks := ks.UpdatePodRequest(app.Pod, app.Replicas)
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

	// recreate apps
	var status string
	for _, report := range reportList.ReportList {
		if report.Status.Reason.Type == "Unschedulable" {
			status = JobFailed
			break
		} else {
			status = JobSucceed
		}
	}

	if status == JobFailed {
		reportList = SchedulerEmulatorReviewList{}

		// Clean pod
		ks.CleanPod()
		ks.CleanStatusPods()

		for _, app := range appgroup.Apps {
			var newpod *v1.Pod
			newpod = app.Pod.DeepCopy()
			newpod.Spec.NodeName = ""
			newpod.Spec.NodeSelector = map[string]string{}
			ks := ks.UpdatePodRequest(newpod, app.Replicas)
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
	}

	// Clean pod
	ks.CleanPod()
	ks.CleanStatusPods()

	// Create old pod
	for _, pod := range appgroup.OldPods {
		newpod := pod.DeepCopy()
		err := ks.CreateAppsPod(newpod)
		if err != nil {
			return nil, fmt.Errorf("unable create old pod")
		}
	}

	ks.reportLock.Lock()
	ks.emulatorResult[key] = &reportList
	ks.reportLock.Unlock()
	return &reportList, nil
}

func (ks *Kubesim) PredictionEmulator(appgroup *utils.AppGroup, key string) (*SchedulerEmulatorReviewList, error) {
	var runPredictionCount int64 = 1
	var nodeConfig utils.NodeConfig
	var reportList *SchedulerEmulatorReviewList
	var backupEmulator *SchedulerEmulatorReviewList
	var backupPrediction map[string][]string
	for _, node := range appgroup.Nodes {
		nodeConfig = node
	}
	minNode := nodeConfig.NodeNumber
	FminNode := minNode
	FmaxNode := ks.generateMaxNode(minNode)
	maxNode := FmaxNode
	predictionNodeCount := (minNode + FmaxNode) / 2

PLoop:
	for {
		klog.Infof("-------------start prediction run try %v -------------", runPredictionCount)
		access, err := ks.CreateFakeNode(&nodeConfig, predictionNodeCount)
		if err != nil {
			return nil, fmt.Errorf("failed to create nodes")
		}
		klog.Infof("minNode %v , maxNode %v , predictionNode %v , start prediction runtry %v", minNode, maxNode, predictionNodeCount, runPredictionCount)
		schedulerPrediction, _ := ks.CreateApps(appgroup, key)
		if schedulerPrediction.PredictionGo == true {
			maxNode = predictionNodeCount
			backupEmulator = schedulerPrediction.ReportList
			backupPrediction = access
			klog.Infof("cluster apps run fine, prediction node %v, so action delete node from cluster.", predictionNodeCount)
		} else {
			minNode = predictionNodeCount
			klog.Infof("cluster apps run failed, prediction node %v, so action add node to cluster.", predictionNodeCount)
		}
		predictionNodeCount = (minNode + maxNode) / 2

		if maxNode-minNode == 1 && FmaxNode != maxNode && FminNode != minNode {
			predictionNodeCount = maxNode
			ks.DeleteNodes()
			klog.Infof("last run try maxNode and predictionNode %v.", maxNode)
			if schedulerPrediction.PredictionGo == true {
				ks.SavePrediction(access, key, minNode, maxNode, predictionNodeCount, schedulerPrediction.ReportList)
				reportList = schedulerPrediction.ReportList
			} else {
				ks.SavePrediction(backupPrediction, key, minNode, maxNode, predictionNodeCount, backupEmulator)
				reportList = backupEmulator
			}
			break PLoop
		} else if maxNode-minNode == 1 && FmaxNode == maxNode {
			FmaxNode = FmaxNode + 100
			maxNode = FmaxNode
			predictionNodeCount = (minNode + maxNode) / 2
			klog.Infof("try keep going, prediction node %v, so action add node to cluster.", predictionNodeCount)
			runPredictionCount++
			continue
		} else if maxNode-minNode == 1 && FminNode == minNode && (maxNode+minNode)/2 != 0 {
			FminNode = FminNode / 2
			minNode = FminNode
			predictionNodeCount = (minNode + maxNode) / 2
			klog.Infof("try keep going, prediction node %v, so action add node to cluster.", predictionNodeCount)
			runPredictionCount++
			continue
		} else if maxNode-minNode == 1 && FminNode == minNode && (maxNode+minNode)/2 == 0 {
			ks.DeleteNodes()
			schedulerPrediction, _ = ks.CreateApps(appgroup, key)
			if schedulerPrediction.PredictionGo == true {
				var accesssw map[string][]string
				reportList = schedulerPrediction.ReportList
				ks.SavePrediction(accesssw, key, 0, 1, 0, reportList)
			} else {
				reportList = backupEmulator
				ks.SavePrediction(backupPrediction, key, 0, 1, 1, reportList)
			}
			break PLoop
		} else {
			klog.Infof("-------------end prediction run try %v ---------------", runPredictionCount)
			runPredictionCount++
			continue
		}
	}

	return reportList, nil
}

func (ks *Kubesim) generateMaxNode(minNode int) int {
	var maxNode int

	switch {
	case minNode <= 1:
		maxNode = 10
	case minNode > 1 && minNode <= 100:
		maxNode = 2 * minNode
	case minNode > 100 && minNode <= 1000:
		maxNode = int(1.2 * float64(minNode))
	case minNode > 1000 && minNode <= 3000:
		maxNode = int(1.3 * float64(minNode))
	case minNode > 4000:
		maxNode = int(1.4 * float64(minNode))
	}

	return maxNode
}

func (ks *Kubesim) CreateApps(appgroup *utils.AppGroup, key string) (*SchedulerPrediction, error) {
	var predictionReport SchedulerPrediction
	var successAppsName []string
	var successCount int32
	var failedCount int32
	var failedMessage string
	var predictionGo bool = true

	var reportList SchedulerEmulatorReviewList
	reportlist := []SchedulerEmulatorReview{}
Loop:
	for _, apps := range appgroup.Apps {
		ks = ks.UpdatePodRequest(apps.Pod, apps.Replicas)
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
		if report.Status.Reason.Type == "Unschedulable" {
			failedMessage = report.Status.Reason.Message
			predictionGo = false
			break Loop
		}

		successAppsName = append(successAppsName, apps.Pod.Name)
		successCount++
		continue
	}

	failedCount = int32(len(appgroup.Apps)) - successCount
	// Clean pod
	ks.CleanPod()

	predictionReport = SchedulerPrediction{
		SuccessAppsName:      successAppsName,
		SuccessAppsCount:     successCount,
		FailedAppsCount:      failedCount,
		FirstAppfailedReason: failedMessage,
		PredictionGo:         predictionGo,
		ReportList:           &reportList,
	}

	return &predictionReport, nil

}

func (ks *Kubesim) CreateFakeNode(nodeConfig *utils.NodeConfig, predictionNode int) (map[string][]string, error) {
	ks.DeleteNodes()
	nodeConfig.NodeNumber = predictionNode
	access, _, err := ks.AddNodes(nodeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create nodes")
	}
	return access, nil
}

func (ks *Kubesim) SavePrediction(access map[string][]string, key string, minNode, maxNode, predictionNode int, reportList *SchedulerEmulatorReviewList) {
	predictionSpec := AccessSwitchSpec{
		MinNode:        int32(minNode),
		MaxNode:        int32(maxNode),
		PredictionNode: int32(predictionNode),
	}
	var accessswitchlist []*AccessSwitch
	for key, value := range access {
		accessswitch := &AccessSwitch{
			Name:         key,
			NodeNumber:   int32(len(value)),
			NodeNameList: value,
		}
		accessswitchlist = append(accessswitchlist, accessswitch)
	}

	predictionStatus := AccessSwitchStatus{
		PredictionNode: int32(predictionNode),
		Access:         accessswitchlist,
	}
	prediction := &PredictionNodeReview{
		Spec:   predictionSpec,
		Status: predictionStatus,
	}
	ks.reportLock.Lock()
	ks.predictionNode[key] = prediction
	ks.emulatorResult[key] = reportList
	ks.reportLock.Unlock()
}

// get nodeinfo
func (ks *Kubesim) GetNodeInfo() (*[]utils.NodeInfo, error) {
	var nodelist *v1.NodeList
	var err error
	schedulerConfig := ks.schedulers[v1.DefaultSchedulerName]
	snapshot := schedulerConfig.SchedulerCache.Dump()
	if nodelist, err = ks.externalkubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "provisioner=kubesim"}); err != nil {
		//if nodelist, err = se.externalkubeclient.CoreV1().Nodes().List(metav1.ListOptions{}); err != nil {
		klog.Errorf("unable to get fake node list %v", err)
		return nil, fmt.Errorf("unable to get fake node list: %v", err)
	}
	//var nodeInfo utils.NodeInfo
	var nodeInfoList []utils.NodeInfo
	for _, node := range nodelist.Items {
		if nodeinfo, ok := snapshot.Nodes[node.Name]; ok {
			usedNode := nodeinfo.Requested
			allocNode := nodeinfo.Allocatable

			nodeInfo := utils.NodeInfo{
				Name:          node.Name,
				UsedCPU:       usedNode.MilliCPU,
				UsedMemory:    usedNode.Memory,
				CPU:           allocNode.MilliCPU,
				Memory:        allocNode.Memory,
				CPUPercent:    fmt.Sprintf("%v%v", float64(usedNode.MilliCPU)/float64(allocNode.MilliCPU)*100, "%"),
				MemoryPercent: fmt.Sprintf("%v%v", float64(usedNode.Memory)/float64(allocNode.Memory)*100, "%"),
				PodNumber:     int64(len(nodeinfo.Pods)),
				//Pods:           nodeinfo.Pods,
			}

			nodeInfoList = append(nodeInfoList, nodeInfo)
		}
	}

	return &nodeInfoList, nil
}

// Clean simulated pod
func (ks *Kubesim) CleanPod() error {
Loop:
	for {
		podItems, err := ks.externalkubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{LabelSelector: "provisioner=kubesim"})
		if len(podItems.Items) == 0 {
			break Loop
		} else {
			for _, item := range podItems.Items {
				if err = ks.externalkubeclient.CoreV1().Pods(item.Namespace).Delete(context.TODO(), item.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
					klog.Errorf("unable to delete pod: %v", err)
					return fmt.Errorf("unable to delete pod: %v", err)
				}
			}
			continue
		}
	}
	return nil
}

// Clean simulated pending pod
func (ks *Kubesim) CleanPendingPod(pod *v1.Pod) error {
	if err := ks.externalkubeclient.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.ObjectMeta.Name, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("unable to delete pod: %v", err)
		return fmt.Errorf("unable to delete pod: %v", err)
	}
	return nil
}

// get apps pod by label
func (ks *Kubesim) GetAppPod(namespace, label string) ([]v1.Pod, error) {
	podItems, err := ks.externalkubeclient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: label})
	if err != nil {
		klog.Errorf("unable to list app : %v", err)
		return nil, fmt.Errorf("unable create app: %v", err)
	}

	return podItems.Items, nil
}

// delete apps pod by pod name
func (ks *Kubesim) DeleteAppsPod(namespace, name string) error {
	err := ks.externalkubeclient.CoreV1().Pods(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("unable to delete pod: %v", err)
		return fmt.Errorf("unable delete pod : %v", err)
	}

	return nil
}

// get pod by pod name and namespace
func (se *Kubesim) GetPod(name, namespace string) (*v1.Pod, error) {
	pod, err := se.externalkubeclient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("unable to get pod: %v", err)
		return nil, fmt.Errorf("unable get pod : %v", err)
	}

	return pod, nil
}

// get apps newpod and oldpods
func (se *Kubesim) GetAppsPod(podsConfig *utils.PodsConfig) ([]*v1.Pod, []*v1.Pod) {
	var oldpods []*v1.Pod
	var newpods []*v1.Pod
	var newpod *v1.Pod
	for _, pod := range podsConfig.Pods {
		oldpod, err := se.GetPod(pod.Name, pod.NameSpace)
		if err == nil {
			newpod = oldpod.DeepCopy()
			oldpods = append(oldpods, oldpod)
			newpods = append(newpods, newpod)
		}
	}
	return oldpods, newpods
}

// create apps pod
func (ks *Kubesim) CreateAppsPod(pod *v1.Pod) error {
	_, err := ks.externalkubeclient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("unable to create pod: %v", err)
		return fmt.Errorf("unable create pod : %v", err)
	}

	return nil
}

// addNodes
func (ks *Kubesim) addNodes(nodeConfig *utils.NodeConfig) (map[string][]string, error) {
	var v1Node *v1.Node
	var err error
	var swList []int

	if ks.addNodeStrategy == "prd" {
		swList = ks.buildRulesNode(nodeConfig)
	} else {
		swList = ks.buildNode(nodeConfig)
	}

	access := make(map[string][]string, len(swList))
	var fakeNameList []string
	for _, swNumber := range swList {
		accessSwitchName := "access" + fmt.Sprintf("%v", types.UID(uuid.NewV4().String())[0:7])
		nodeConfig.NodeLabels["accessSwitch"] = accessSwitchName
		fakeNameList = []string{}
		for i := 0; i < swNumber; i++ {
			nodeName := "fake-" + gouuid.New().String()
			fakeNameList = append(fakeNameList, nodeName)
			if v1Node, err = nodeconfig.BuildNode(nodeConfig, nodeName); err != nil {
				return nil, fmt.Errorf("Unable to build node config: %v", err)
			}
			if _, err = ks.externalkubeclient.CoreV1().Nodes().Create(context.TODO(), v1Node, metav1.CreateOptions{}); err != nil {
				return nil, fmt.Errorf("unable to create fake node: %v", err)
			}
		}
		access[accessSwitchName] = fakeNameList
	}

	return access, nil
}

func (ks *Kubesim) buildRulesNode(nodeConfig *utils.NodeConfig) []int {
	var SW []int
	switchNumber := nodeConfig.NodeNumber / accessNum
	switchNumberOut := nodeConfig.NodeNumber % accessNum
	if nodeConfig.NodeNumber <= 2*accessNum {
		sw1 := nodeConfig.NodeNumber / 2
		sw2 := nodeConfig.NodeNumber % 2
		SW = []int{sw1, sw1 + sw2}
	} else {
		for i := 0; i < switchNumber; i++ {
			SW = append(SW, accessNum)
		}
		if switchNumberOut != 0 {
			SW = append(SW, switchNumberOut)
		}
	}

	return SW
}

func (ks *Kubesim) buildNode(nodeConfig *utils.NodeConfig) []int {
	var SW []int
	switchNumber := nodeConfig.NodeNumber / accessNum
	switchNumberOut := nodeConfig.NodeNumber % accessNum
	for i := 0; i < switchNumber; i++ {
		SW = append(SW, accessNum)
	}
	if switchNumberOut != 0 {
		SW = append(SW, switchNumberOut)
	}

	return SW
}

// Create simulator node
func (ks *Kubesim) AddNodes(nodeConfig *utils.NodeConfig) (map[string][]string, *utils.NodeInfoList, error) {
	nodeCount := nodeConfig.NodeNumber
	var nodeinfolist *utils.NodeInfoList
	var err error
	var access map[string][]string
Loop:
	for {
		nodeinfolist, err = ks.ListNodes()
		if err != nil {
			return nil, nil, fmt.Errorf("Unable to list simulator node: %v", err)
		}

		if nodeinfolist.NodeNumber < nodeCount {
			failedNode := nodeCount - nodeinfolist.NodeNumber
			nodeConfig.NodeNumber = failedNode
			access, err = ks.addNodes(nodeConfig)
			if err != nil {
				return nil, nil, fmt.Errorf("Unable to create simulator node: %v", err)
			}
			continue
		} else if nodeinfolist.NodeNumber > nodeCount {
			deleteNodeCount := nodeinfolist.NodeNumber - nodeCount
			deleteNodeList := nodeinfolist.NodeList.Items[0:deleteNodeCount]
			for _, item := range deleteNodeList {
				if err := ks.externalkubeclient.CoreV1().Nodes().Delete(context.TODO(), item.Name, metav1.DeleteOptions{}); err != nil {
					klog.Errorf("Unable to delete simulator node %v: %v", item.ObjectMeta.Name, err)
				}
			}
			continue
		} else {
			break Loop
			klog.Infof("Add node is work fine.")
		}
	}
	return access, nodeinfolist, nil
}

// Delete simulator node
func (ks *Kubesim) DeleteNodes() error {
	// check simulator pod, if exist, delete all, than delete node
	ks.CleanPod()

	nodelist, err := ks.ListNodes()
	if err != nil {
		return fmt.Errorf("Unable to list simulator node: %v", err)
	}
	if len(nodelist.NodeList.Items) > 0 {
		for _, item := range nodelist.NodeList.Items {
			if err := ks.externalkubeclient.CoreV1().Nodes().Delete(context.TODO(), item.Name, metav1.DeleteOptions{}); err != nil {
				klog.Errorf("Unable to delete simulator node %v: %v", item.ObjectMeta.Name, err)
			}
		}
	}

	return nil
}

// List simulator node
func (ks *Kubesim) ListNodes() (*utils.NodeInfoList, error) {
	var nodelist *v1.NodeList
	var podlist *v1.PodList
	var err error
	//if nodelist, err = ks.externalkubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "provisioner=kubesim"}); err != nil {
	if nodelist, err = ks.externalkubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{}); err != nil {
		klog.Errorf("unable to get fake node list %v", err)
		return nil, fmt.Errorf("unable to get fake node list: %v", err)
	}

	if podlist, err = ks.externalkubeclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{LabelSelector: "provisioner=kubesim"}); err != nil {
		klog.Errorf("unable to get pod list %v", err)
		return nil, fmt.Errorf("unable to get pod list: %v", err)
	}

	var runingPod, notRununingPod, bindfailed, bindsuccess int
	nodepod := make(map[string]int, len(nodelist.Items))
	var status v1.PodPhase
	for _, pod := range podlist.Items {
		if nodepod[pod.Spec.NodeName] != 0 {
			nodepod[pod.Spec.NodeName]++
		} else {
			nodepod[pod.Spec.NodeName] = 1
		}

		if pod.Status.Phase == v1.PodRunning {
			runingPod++
		} else {
			notRununingPod++
			status = pod.Status.Phase
		}

		if len(pod.Spec.NodeName) == 0 {
			bindfailed++
		} else {
			bindsuccess++
		}

	}

	schedulerConfig := ks.schedulers[v1.DefaultSchedulerName]
	snapshot := schedulerConfig.SchedulerCache.Dump()
	nodeinfopod := make(map[string]int, len(snapshot.Nodes))
	var podnumber int
	var nodeinfonumber, notnodeinfonumber int
	for _, node := range nodelist.Items {
		if nodeinfo, ok := snapshot.Nodes[node.Name]; ok {
			nodeinfonumber++
			podnumber = podnumber + len(nodeinfo.Pods)
		} else {
			notnodeinfonumber++
		}
	}

	for key, nodeinfo := range snapshot.Nodes {
		nodeinfopod[key] = len(nodeinfo.Pods)
	}

	nodeInfoList := utils.NodeInfoList{
		NodeNumber:        len(nodelist.Items),
		PodNumber:         len(podlist.Items),
		RunningPod:        runingPod,
		NotRunningPod:     notRununingPod,
		NodeInfoPodNumber: podnumber,
		NodeInfoNumber:    nodeinfonumber,
		NotNodeInfoNumber: notnodeinfonumber,
		BindFailed:        bindfailed,
		BindSuccess:       bindsuccess,
		Status:            status,
		NodePods:          nodepod,
		NodeInfoPods:      nodeinfopod,
		NodeList:          nodelist,
	}

	return &nodeInfoList, nil
}

func (ks *Kubesim) createScheduler(schedulerName string, cc *schedconfig.CompletedConfig, outOfTreeRegistryOptions ...utils.Option) (*scheduler.Scheduler, error) {
	outOfTreeRegistry := make(frameworkruntime.Registry)
	for _, option := range outOfTreeRegistryOptions {
		if err := option(outOfTreeRegistry); err != nil {
			return nil, err
		}
	}
	outOfTreeRegistry["KubesimBinder"] = func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
		return ks.NewBindPlugin(schedulerName, configuration, f)
	}

	ks.informerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				if pod, ok := obj.(*v1.Pod); ok && pod.Spec.SchedulerName == schedulerName {
					return true
				}
				return false
			},
			Handler: cache.ResourceEventHandlerFuncs{
				UpdateFunc: func(oldObj, newObj interface{}) {
					if pod, ok := newObj.(*v1.Pod); ok {
						for _, podCondition := range pod.Status.Conditions {
							if podCondition.Type == v1.PodScheduled {
								ks.Update(pod, &podCondition, schedulerName)
							}
						}
					}
				},
			},
		},
	)

	// Create the scheduler.
	return scheduler.New(
		ks.externalkubeclient,
		ks.informerFactory,
		//ks.informerFactory.Core().V1().Pods(),
		getRecorderFactory(cc),
		ks.schedulerCh,
		scheduler.WithProfiles(cc.ComponentConfig.Profiles...),
		scheduler.WithAlgorithmSource(cc.ComponentConfig.AlgorithmSource),
		scheduler.WithPercentageOfNodesToScore(cc.ComponentConfig.PercentageOfNodesToScore),
		scheduler.WithFrameworkOutOfTreeRegistry(outOfTreeRegistry),
		scheduler.WithPodMaxBackoffSeconds(cc.ComponentConfig.PodMaxBackoffSeconds),
		scheduler.WithPodInitialBackoffSeconds(cc.ComponentConfig.PodInitialBackoffSeconds),
		scheduler.WithExtenders(cc.ComponentConfig.Extenders...),
	)
}

func (ks *Kubesim) NewBindPlugin(schedulerName string, configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
	return &localBinderPodConditionUpdater{
		schedulerName: schedulerName,
		kubesim:       ks,
	}, nil
}

type localBinderPodConditionUpdater struct {
	schedulerName string
	kubesim       *Kubesim
}

func (b *localBinderPodConditionUpdater) Name() string {
	return "KubesimBinder"
}

// TODO(riverzhang): Needs to be locked since the scheduler runs the binding phase in a go routine
func (b *localBinderPodConditionUpdater) Bind(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) *framework.Status {
	return b.kubesim.Bind(ctx, state, p, nodeName, b.schedulerName)
}

// Create new cluster capacity analysis
// The analysis is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func New(addStrategy string, cc *schedconfig.CompletedConfig, client, kubeClient externalclientset.Interface, outOfTreeRegistryOptions ...utils.Option) (*Kubesim, error) {
	sharedInformerFactory := informers.NewSharedInformerFactory(client, 0)

	// generate new recorder for kubesim
	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(clientgoscheme.Scheme, v1.EventSource{Component: "KubeSim"})

	ks := &Kubesim{
		strategy:           strategy.NewPredictiveStrategy(client),
		externalkubeclient: client,
		informerFactory:    sharedInformerFactory,
		KubeClient:         kubeClient,
		Recorder:           recorder,
		Broadcaster:        eventBroadcaster,
		simulated:          0,
		fifoQueue:          queue.NewFIFOQueue(),
		emulatorResult:     map[string]*SchedulerEmulatorReviewList{},
		predictionNode:     map[string]*PredictionNodeReview{},
		addNodeStrategy:    addStrategy,
	}

	ks.schedulers = make(map[string]*scheduler.Scheduler)

	scheduler, err := ks.createScheduler(v1.DefaultSchedulerName, cc, outOfTreeRegistryOptions...)
	if err != nil {
		return nil, err
	}

	ks.schedulers[v1.DefaultSchedulerName] = scheduler
	ks.defaultSchedulerName = v1.DefaultSchedulerName

	ctx, _ := context.WithCancel(context.Background())

	// Setup healthz checks.
	var checks []healthz.HealthChecker
	if cc.ComponentConfig.LeaderElection.LeaderElect {
		checks = append(checks, cc.LeaderElection.WatchDog)
	}

	waitingForLeader := make(chan struct{})
	isLeader := func() bool {
		select {
		case _, ok := <-waitingForLeader:
			// if channel is closed, we are leading
			return !ok
		default:
			// channel is open, we are waiting for a leader
			return false
		}
	}

	// Start up the healthz server.
	if cc.InsecureServing != nil {
		separateMetrics := cc.InsecureMetricsServing != nil
		handler := buildHandlerChain(newHealthzHandler(&cc.ComponentConfig, cc.InformerFactory, isLeader, separateMetrics, checks...), nil, nil)
		if err := cc.InsecureServing.Serve(handler, 0, ctx.Done()); err != nil {
			return nil, fmt.Errorf("failed to start healthz server: %v", err)
		}
	}
	if cc.InsecureMetricsServing != nil {
		handler := buildHandlerChain(newMetricsHandler(&cc.ComponentConfig, cc.InformerFactory, isLeader), nil, nil)
		if err := cc.InsecureMetricsServing.Serve(handler, 0, ctx.Done()); err != nil {
			return nil, fmt.Errorf("failed to start metrics server: %v", err)
		}
	}
	/*if cc.SecureServing != nil {
		handler := buildHandlerChain(newHealthzHandler(&cc.ComponentConfig, false, checks...), cc.Authentication.Authenticator, cc.Authorization.Authorizer)
		// TODO: handle stoppedCh returned by c.SecureServing.Serve
		if _, err := cc.SecureServing.Serve(handler, 0, ctx.Done()); err != nil {
			// fail early for secure handlers, removing the old error loop from above
			return fmt.Errorf("failed to start secure server: %v", err)
		}
	}*/

	return ks, nil
}

func getRecorderFactory(cc *schedconfig.CompletedConfig) profile.RecorderFactory {
	return func(name string) events.EventRecorder {
		return cc.EventBroadcaster.NewRecorder(name)
	}
}

// buildHandlerChain wraps the given handler with the standard filters.
func buildHandlerChain(handler http.Handler, authn authenticator.Request, authz authorizer.Authorizer) http.Handler {
	requestInfoResolver := &apirequest.RequestInfoFactory{}
	failedHandler := genericapifilters.Unauthorized(scheme.Codecs)

	handler = genericapifilters.WithAuthorization(handler, authz, scheme.Codecs)
	handler = genericapifilters.WithAuthentication(handler, authn, failedHandler, nil)
	handler = genericapifilters.WithRequestInfo(handler, requestInfoResolver)
	handler = genericapifilters.WithCacheControl(handler)
	handler = genericfilters.WithPanicRecovery(handler, requestInfoResolver)

	return handler
}

func installMetricHandler(pathRecorderMux *mux.PathRecorderMux, informers informers.SharedInformerFactory, isLeader func() bool) {
	configz.InstallHandler(pathRecorderMux)
	pathRecorderMux.Handle("/metrics", legacyregistry.HandlerWithReset())

	resourceMetricsHandler := resources.Handler(informers.Core().V1().Pods().Lister())
	pathRecorderMux.HandleFunc("/metrics/resources", func(w http.ResponseWriter, req *http.Request) {
		if !isLeader() {
			return
		}
		resourceMetricsHandler.ServeHTTP(w, req)
	})
}

// newMetricsHandler builds a metrics server from the config.
func newMetricsHandler(config *kubeschedulerconfig.KubeSchedulerConfiguration, informers informers.SharedInformerFactory, isLeader func() bool) http.Handler {
	pathRecorderMux := mux.NewPathRecorderMux("kube-scheduler")
	installMetricHandler(pathRecorderMux, informers, isLeader)
	if config.EnableProfiling {
		routes.Profiling{}.Install(pathRecorderMux)
		if config.EnableContentionProfiling {
			goruntime.SetBlockProfileRate(1)
		}
		routes.DebugFlags{}.Install(pathRecorderMux, "v", routes.StringFlagPutHandler(logs.GlogSetter))
	}
	return pathRecorderMux
}

// newHealthzHandler creates a healthz server from the config, and will also
// embed the metrics handler if the healthz and metrics address configurations
// are the same.
func newHealthzHandler(config *kubeschedulerconfig.KubeSchedulerConfiguration, informers informers.SharedInformerFactory, isLeader func() bool, separateMetrics bool, checks ...healthz.HealthChecker) http.Handler {
	pathRecorderMux := mux.NewPathRecorderMux("kube-scheduler")
	healthz.InstallHandler(pathRecorderMux, checks...)
	if !separateMetrics {
		installMetricHandler(pathRecorderMux, informers, isLeader)
	}
	if config.EnableProfiling {
		routes.Profiling{}.Install(pathRecorderMux)
		if config.EnableContentionProfiling {
			goruntime.SetBlockProfileRate(1)
		}
		routes.DebugFlags{}.Install(pathRecorderMux, "v", routes.StringFlagPutHandler(logs.GlogSetter))
	}
	return pathRecorderMux
}
