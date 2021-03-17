package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	//"io/ioutil"
	"net/http"

	"k8s.io/klog"

	"github.com/julienschmidt/httprouter"

	"github.com/riverzhang/kubesim/pkg/scheduler"
	"github.com/riverzhang/kubesim/pkg/utils"
)

const (
	versionPath = "/version"
	apiPrefix   = "/v1/scheduler"

	// emulator apps
	appsEmulatorPrefix       = apiPrefix + "/emulator/:kind/apps"
	horizontalEmulatorPrefix = apiPrefix + "/emulator/:kind/horizontal/apps"
	verticalEmulatorPrefix   = apiPrefix + "/emulator/:kind/vertical/apps"

	// prediction nodes
	predictionPrefix = apiPrefix + "/prediction/:kind/nodes"

	// emulator report
	reportPrefix = apiPrefix + "/report/:id"

	// simulator nodes
	addNodesPrefix      = apiPrefix + "/simulator/node"
	deleteALLNodePrefix = apiPrefix + "/simulator/node"
	listAllNodePrefix   = apiPrefix + "/simulator/node"
	listNodeInfoPrefix  = apiPrefix + "/simulator/nodeinfo"

	// deprecated prefix
	deprecatedPredictionPrefix   = apiPrefix + "/deprecated/prediction/:kind/nodes"
	deprecatedAppsEmulatorPrefix = apiPrefix + "/deprecated/emulator/:kind/apps"
)

var (
	version = "0.0.1"
)

func checkBody(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
}

func VersionRoute(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	fmt.Fprint(w, fmt.Sprint(version))
}

func AppsEmulatorRoute(emulator *scheduler.AppsEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)
		kind := params.ByName("kind")
		klog.Infof("received multiple apps emulate for %s.", kind)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		// parse to data
		nodes, appList, err := utils.AppsParseToPod(kind, body)
		if err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			report := emulator.AppsHandler(nodes, appList)
			if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func HorizontalEmulatorRoute(emulator *scheduler.HorizontalEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)

		kind := params.ByName("kind")
		klog.Infof("received multiple apps emulate for %s.", kind)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		// parse to data
		nodes, appList, err := utils.AppsParseToPod(kind, body)
		if err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			report, err := emulator.HorizontalHandler(nodes, appList)
			if err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func VerticalEmulatorRoute(emulator *scheduler.VerticalEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)

		kind := params.ByName("kind")
		klog.Infof("received multiple apps emulate for %s.", kind)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		// parse to data
		nodes, appList, err := utils.AppsParseToPod(kind, body)
		if err != nil {
			klog.Errorf("failed reason is %v")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			report, err := emulator.VerticalHandler(nodes, appList)
			if err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func PredictionEmulatorRoute(emulator *scheduler.PredictionEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)

		kind := params.ByName("kind")
		klog.Infof("received multiple apps emulate for %s.", kind)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		// parse to data
		nodes, appList, err := utils.AppsParseToPod(kind, body)
		if err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			report := emulator.PredictionHandler(nodes, appList)
			if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func EmulatorReportRoute(emulator *scheduler.EmulatorReport) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)
		id := params.ByName("id")

		report, err := emulator.EmulatorReportHandler(id)
		if err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func AddNodesEmulatorRoute(emulator *scheduler.AddNodesEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		checkBody(w, r)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)
		// parse to data
		var nodelist *utils.NodeInfoList
		nodeConfig, err := utils.NodeConfigParse(body)
		if err != nil {
			klog.Errorf("parse to nodeConfig err: %v", err)
		} else {
			nodelist, err = emulator.AddNodesHandler(nodeConfig)
			if err != nil {
				klog.Errorf("failed to create node: %v", err)
			}
		}

		if resultBody, err := json.Marshal(nodelist); err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			//klog.Infof("parse result: ", string(resultBody))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(resultBody)
		}
	}
}

func ListNodesEmulatorRoute(emulator *scheduler.ListNodesEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		checkBody(w, r)

		nodelist, err := emulator.ListNodesHandler()
		if err != nil {
			klog.Errorf("failed to due to %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			if resultBody, err := json.Marshal(nodelist); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func ListNodeInfoEmulatorRoute(emulator *scheduler.ListNodeInfoEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		checkBody(w, r)

		nodelistinfo, err := emulator.ListNodeInfoHandler()
		if err != nil {
			klog.Errorf("failed to due to %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			if resultBody, err := json.Marshal(nodelistinfo); err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func DeleteNodesEmulatorRoute(emulator *scheduler.DeleteNodesEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		checkBody(w, r)

		err := emulator.DeleteNodesHandler()
		if err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			//klog.Infof("Delete simulator node success.")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
		}
	}
}

func DeprecatedPredictionEmulatorRoute(emulator *scheduler.DeprecatedPredictionEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)

		kind := params.ByName("kind")
		klog.Infof("received multiple apps emulate for %s.", kind)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		nodes, appList, err := utils.AppsParseToPod(kind, body)
		if err != nil {
			klog.Errorf("failed reason is %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			report, err := emulator.DeprecatedPredictionHandler(nodes, appList)
			if err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed to due to %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func DeprecatedAppsEmulatorRoute(emulator *scheduler.DeprecatedAppsEmulator) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		checkBody(w, r)

		kind := params.ByName("kind")
		klog.Infof("received multiple apps emulate for %s.", kind)

		var buf bytes.Buffer
		body := io.TeeReader(r.Body, &buf)

		// parse to data
		nodes, appList, err := utils.AppsParseToPod(kind, body)
		if err != nil {
			klog.Errorf("failed to due to %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
			w.Write([]byte(errMsg))
		} else {
			report, err := emulator.DeprecatedAppsHandler(nodes, appList)
			if err != nil {
				klog.Errorf("failed reason is %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else if resultBody, err := json.Marshal(report); err != nil {
				klog.Errorf("failed to due to %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
				w.Write([]byte(errMsg))
			} else {
				//klog.Infof("parse result: ", string(resultBody))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(resultBody)
			}
		}
	}
}

func reportError(w http.ResponseWriter, err error) {
	klog.Errorf("failed to emulate due to %v", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	errMsg := fmt.Sprintf("{'error':'%s'}", err.Error())
	w.Write([]byte(errMsg))
}

func AddVersion(router *httprouter.Router) {
	router.GET(versionPath, DebugLogging(VersionRoute, versionPath))
}

func AddAppsEmulator(router *httprouter.Router, appsemulator *scheduler.AppsEmulator) {
	router.POST(appsEmulatorPrefix, DebugLogging(AppsEmulatorRoute(appsemulator), appsEmulatorPrefix))
}

func AddHorizontalEmulator(router *httprouter.Router, emulator *scheduler.HorizontalEmulator) {
	router.POST(horizontalEmulatorPrefix, DebugLogging(HorizontalEmulatorRoute(emulator), horizontalEmulatorPrefix))
}

func AddVerticalEmulator(router *httprouter.Router, emulator *scheduler.VerticalEmulator) {
	router.POST(verticalEmulatorPrefix, DebugLogging(VerticalEmulatorRoute(emulator), verticalEmulatorPrefix))
}

func AddPredictionEmulator(router *httprouter.Router, emulator *scheduler.PredictionEmulator) {
	router.POST(predictionPrefix, DebugLogging(PredictionEmulatorRoute(emulator), predictionPrefix))
}

func AddEmulatorReport(router *httprouter.Router, emulator *scheduler.EmulatorReport) {
	router.GET(reportPrefix, DebugLogging(EmulatorReportRoute(emulator), reportPrefix))
}

func AddNodesEmulator(router *httprouter.Router, addnodes *scheduler.AddNodesEmulator) {
	router.POST(addNodesPrefix, DebugLogging(AddNodesEmulatorRoute(addnodes), addNodesPrefix))
}

func DeleteNodesEmulator(router *httprouter.Router, deletenodes *scheduler.DeleteNodesEmulator) {
	router.DELETE(deleteALLNodePrefix, DebugLogging(DeleteNodesEmulatorRoute(deletenodes), deleteALLNodePrefix))
}

func ListNodesEmulator(router *httprouter.Router, listnodes *scheduler.ListNodesEmulator) {
	router.GET(listAllNodePrefix, DebugLogging(ListNodesEmulatorRoute(listnodes), listAllNodePrefix))
}

func ListNodeInfoEmulator(router *httprouter.Router, listnodes *scheduler.ListNodeInfoEmulator) {
	router.GET(listNodeInfoPrefix, DebugLogging(ListNodeInfoEmulatorRoute(listnodes), listNodeInfoPrefix))
}

func AddDeprecatedPredictionEmulator(router *httprouter.Router, predictionemulator *scheduler.DeprecatedPredictionEmulator) {
	router.POST(deprecatedPredictionPrefix, DebugLogging(DeprecatedPredictionEmulatorRoute(predictionemulator), deprecatedPredictionPrefix))
}

func AddDeprecatedAppsEmulator(router *httprouter.Router, appsrulesemulator *scheduler.DeprecatedAppsEmulator) {
	router.POST(deprecatedAppsEmulatorPrefix, DebugLogging(DeprecatedAppsEmulatorRoute(appsrulesemulator), deprecatedAppsEmulatorPrefix))
}

func DebugLogging(h httprouter.Handle, path string) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		//klog.Infof("debug start: ", path, " request body = ", r.Body)
		h(w, r, p)
		//klog.Infof("debug end: ", path, " response=", w)
	}
}
