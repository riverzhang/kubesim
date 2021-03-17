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
	"errors"
	"fmt"

	"github.com/riverzhang/kubesim/pkg/utils"
)

var (
	// ErrEmptyQueue is returned from Pop.
	ErrEmptyQueue = errors.New("No pod queued")
	// ErrDifferentNames is returned from Update.
	ErrDifferentNames = errors.New("Original and new pods have different names")
)

// ErrNoMatchingPod is returned from Update.
type ErrNoMatchingPod struct {
	key string
}

func (e *ErrNoMatchingPod) Error() string {
	return fmt.Sprintf("No pod with key %q", e.key)
}

// PodQueue defines the interface of pod queues.
type PodQueue interface {
	// Push pushes the pod to the "end" of this PodQueue.
	Push(key string, pod *utils.AppGroup) error

	// Pop pops the pod on the "front" of this PodQueue.
	// This method never blocks; Immediately returns ErrEmptyQueue if the queue is empty.
	Pop() (*utils.AppGroup, string, error)

	// Front refers (not pops) the pod on the "front" of this PodQueue.
	// This method never bocks; Immediately returns ErrEmptyQueue if the queue is empty.
	Front() (*utils.AppGroup, string, error)

	// Get fifoQueue key
	Get() map[string]*utils.AppGroup

	// Delete deletes the pod from this PodQueue.
	// Returns true if the pod is found, or false otherwise.
	//Delete(*utils.AppGroup, string) bool

	// Update updates the pod to the newPod.
	// Returns ErrNoMatchingPod if an original pod is not found.
	// The original and new pods must have the same namespace/name; Otherwise ErrDifferentNames is
	// returned in the second field.
	//Update(*utils.AppGroup, string) error
}
