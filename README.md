# KubeSim analysis framework

## Intro
kubesim provides a virtual kubernetes cluster interface for evaluating your scheduler. As new pods get scheduled on nodes
in a cluster, more resources get consumed.Monitoring available resources and scheduler rules in the cluster is very important
as operators can increase the current resources in time before all of them get exhausted.Or, carry different steps that lead 
to increase of available resources.

## Build and Run

Build the framework:

```sh
$ make build
```

and run the analysis:

```sh
$ make run
```

For more information about available options run:
```
$ ./kubesim --help
```
## Running KubeSim

Follow these example steps to run kubesim on kubernetes cluster:

### 1.  Build kubesim docker images
In this example we create a simple Docker image utilizing the Dockerfile found in the root directory and tag it with `kubesim-image`:
```
$ make build-image .
```

### 2. Setup an create kubesim server

```
$ kubectl create -f manifests/kubesim.yaml
$ kubectl create -f manifests/kubesim-cm.yaml
```

## Key Features

### 1.  Support kubernetes cluster simulator
### 2.  Support vpa emulator of apps
### 3.  Support hpa emulator of apps
### 4.  Support cluster Prediction
### 5.  Support debug online kubernetes scheduler

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).