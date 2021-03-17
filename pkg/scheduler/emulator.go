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
	"github.com/riverzhang/kubesim/pkg/utils"
)

type AppsEmulator struct {
	Name  string
	Func  func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) *utils.SimulatorJob
	cache *Kubesim
}

func (e AppsEmulator) AppsHandler(nodes []utils.NodeConfig, apps utils.MutiAppsRequest) *utils.SimulatorJob {
	job_id := e.Func(nodes, apps, e.cache)
	return job_id
}

type HorizontalEmulator struct {
	Name  string
	Func  func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*utils.SimulatorJob, error)
	cache *Kubesim
}

func (e HorizontalEmulator) HorizontalHandler(nodes []utils.NodeConfig, apps utils.MutiAppsRequest) (*utils.SimulatorJob, error) {
	job_id, err := e.Func(nodes, apps, e.cache)
	if err != nil {
		return nil, err
	}
	return job_id, nil
}

type VerticalEmulator struct {
	Name  string
	Func  func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*utils.SimulatorJob, error)
	cache *Kubesim
}

func (e VerticalEmulator) VerticalHandler(nodes []utils.NodeConfig, apps utils.MutiAppsRequest) (*utils.SimulatorJob, error) {
	job_id, err := e.Func(nodes, apps, e.cache)
	if err != nil {
		return nil, err
	}
	return job_id, nil
}

type PredictionEmulator struct {
	Name  string
	Func  func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) *utils.SimulatorJob
	cache *Kubesim
}

func (e PredictionEmulator) PredictionHandler(nodes []utils.NodeConfig, apps utils.MutiAppsRequest) *utils.SimulatorJob {
	job_id := e.Func(nodes, apps, e.cache)
	return job_id
}

type EmulatorReport struct {
	Name  string
	Func  func(id string, ks *Kubesim) (*JobReport, error)
	cache *Kubesim
}

func (e EmulatorReport) EmulatorReportHandler(id string) (*JobReport, error) {
	report, err := e.Func(id, e.cache)
	if err != nil {
		return nil, err
	}

	return report, err
}

type AddNodesEmulator struct {
	Name  string
	Func  func(nodeConfig *utils.NodeConfig, ks *Kubesim) (*utils.NodeInfoList, error)
	cache *Kubesim
}

func (e AddNodesEmulator) AddNodesHandler(nodeConfig *utils.NodeConfig) (*utils.NodeInfoList, error) {
	nodelist, err := e.Func(nodeConfig, e.cache)
	if err != nil {
		return nil, err
	}

	return nodelist, nil
}

type ListNodesEmulator struct {
	Name  string
	Func  func(ks *Kubesim) (*utils.NodeInfoList, error)
	cache *Kubesim
}

func (e ListNodesEmulator) ListNodesHandler() (*utils.NodeInfoList, error) {

	nodelist, err := e.Func(e.cache)
	if err != nil {
		return nil, err
	}

	return nodelist, err
}

type ListNodeInfoEmulator struct {
	Name  string
	Func  func(ks *Kubesim) (*[]utils.NodeInfo, error)
	cache *Kubesim
}

func (e ListNodeInfoEmulator) ListNodeInfoHandler() (*[]utils.NodeInfo, error) {

	nodelistinfo, err := e.Func(e.cache)
	if err != nil {
		return nil, err
	}

	return nodelistinfo, nil
}

type DeleteNodesEmulator struct {
	Name  string
	Func  func(S *Kubesim) error
	cache *Kubesim
}

func (e DeleteNodesEmulator) DeleteNodesHandler() error {
	err := e.Func(e.cache)
	if err != nil {
		return err
	}

	return nil
}

type DeprecatedPredictionEmulator struct {
	Name  string
	Func  func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*SchedulerPrediction, error)
	cache *Kubesim
}

func (e DeprecatedPredictionEmulator) DeprecatedPredictionHandler(nodes []utils.NodeConfig, apps utils.MutiAppsRequest) (*SchedulerPrediction, error) {
	report, err := e.Func(nodes, apps, e.cache)
	if err != nil {
		return nil, err
	}

	return report, err
}

type DeprecatedAppsEmulator struct {
	Name  string
	Func  func(nodes []utils.NodeConfig, apps utils.MutiAppsRequest, ks *Kubesim) (*SchedulerEmulatorReviewList, error)
	cache *Kubesim
}

func (e DeprecatedAppsEmulator) DeprecatedAppsHandler(nodes []utils.NodeConfig, apps utils.MutiAppsRequest) (*SchedulerEmulatorReviewList, error) {
	report, err := e.Func(nodes, apps, e.cache)
	if err != nil {
		return nil, err
	}

	return report, nil
}
