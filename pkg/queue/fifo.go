// Copyright 2020 Preferred Networks, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package queue

import (
	"sync"

	"k8s.io/klog"

	"github.com/riverzhang/kubesim/pkg/utils"
)

// FIFOQueue stores pods in a FIFO queue.
type FIFOQueue struct {
	// Push adds a pod to both the map and the slice; Pop deletes a pod from both.
	// OTOH, Delete deletes a pod only from the map, so a pod associated with a key popped from the
	// slice may have been deleted.
	// Pop and Front check whether the pod actually exists in the slice.

	appGroup map[string]*utils.AppGroup

	queue []string

	lock *sync.Mutex
}

// NewFIFOQueue creates a new FIFOQueue.
func NewFIFOQueue() *FIFOQueue {
	return &FIFOQueue{
		appGroup: map[string]*utils.AppGroup{},
		queue:    []string{},
		lock:     &sync.Mutex{},
	}
}

func (fifo *FIFOQueue) Push(key string, appGroup *utils.AppGroup) error {
	fifo.lock.Lock()
	defer fifo.lock.Unlock()
	klog.Infof("Push appGroup to fifiqueue, key: %v", key)
	fifo.appGroup[key] = appGroup
	fifo.queue = append(fifo.queue, key)
	klog.Infof("The current scheduling fifo queue: %v", fifo.queue)
	return nil
}

func (fifo *FIFOQueue) Pop() (*utils.AppGroup, string, error) {
	fifo.lock.Lock()
	defer fifo.lock.Unlock()
	var key string
	for len(fifo.queue) > 0 {
		key, fifo.queue = fifo.queue[0], fifo.queue[1:]
		if appgroup, ok := fifo.appGroup[key]; ok {
			delete(fifo.appGroup, key)
			klog.Infof("The current scheduling fifo queue: %v\n", fifo.queue)
			return appgroup, key, nil
		}
	}

	return nil, key, ErrEmptyQueue
}

func (fifo *FIFOQueue) Front() (*utils.AppGroup, string, error) {
	fifo.lock.Lock()
	defer fifo.lock.Unlock()
	var key string
	for len(fifo.queue) > 0 {
		key = fifo.queue[0]
		if appgroup, ok := fifo.appGroup[key]; ok {
			return appgroup, key, nil
		}
		fifo.queue = fifo.queue[1:]
	}

	return nil, key, ErrEmptyQueue
}

func (fifo *FIFOQueue) Get() map[string]*utils.AppGroup {
	fifo.lock.Lock()
	defer fifo.lock.Unlock()
	return fifo.appGroup
}

/*func (fifo *FIFOQueue) Delete(*utils.AppGroup, key string) bool {
	key = utils.PodKeyFromNames(podNamespace, podName)
	_, ok := fifo.pods[key]
	delete(fifo.pods, key)

	return ok
}

func (fifo *FIFOQueue) Update(*utils.AppGroup, string,) error {
	keyOrig := util.PodKeyFromNames(podNamespace, podName)
	keyNew, err := util.PodKey(newPod)
	if err != nil {
		return err
	}
	if keyOrig != keyNew {
		return ErrDifferentNames
	}

	if _, ok := fifo.pods[keyOrig]; !ok {
		return &ErrNoMatchingPod{key: keyOrig}
	}

	fifo.pods[keyOrig] = newPod
	return nil
}*/

var _ = PodQueue(&FIFOQueue{})
