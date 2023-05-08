/*
Copyright 2019 The Kubernetes Authors.

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
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/version"
	csilog "wujunyi792/oss-csi-lite-plugin/pkg/log"
	"wujunyi792/oss-csi-lite-plugin/pkg/metric"
	"wujunyi792/oss-csi-lite-plugin/pkg/om"
	_ "wujunyi792/oss-csi-lite-plugin/pkg/options"
	"wujunyi792/oss-csi-lite-plugin/pkg/oss"
	"wujunyi792/oss-csi-lite-plugin/pkg/utils"
)

func init() {
	flag.Set("logtostderr", "true")
}

func setPrometheusVersion() {
	version.Version = VERSION
	version.Revision = REVISION
	version.Branch = BRANCH
	version.BuildDate = BUILDTIME
}

const (
	// TypePluginSuffix is the suffix of all storage plugins.
	TypePluginSuffix = "plugin.csi.alibabacloud.com"
	// PluginServicePort default port is 11260.
	PluginServicePort = "11260"
	// ProvisionerServicePort default port is 11270.
	ProvisionerServicePort = "11270"
)

// BRANCH is CSI Driver Branch
var BRANCH = ""

// VERSION is CSI Driver Version
var VERSION = ""

// BUILDTIME is CSI Driver Buildtime
var BUILDTIME = ""

// REVISION is CSI Driver Revision
var REVISION = ""

var (
	endpoint = flag.String("endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	nodeID   = flag.String("nodeid", "114514", "node id")
)

type globalMetricConfig struct {
	enableMetric bool
	serviceType  string
}

// Nas CSI Plugin
func main() {
	flag.Parse()
	serviceType := os.Getenv(utils.ServiceType)

	if len(serviceType) == 0 || serviceType == "" {
		serviceType = utils.PluginService
	}

	var logAttribute string
	if serviceType == utils.ProvisionerService {
		logAttribute = strings.Replace(TypePluginSuffix, utils.PluginService, utils.ProvisionerService, -1)
	} else {
		logAttribute = TypePluginSuffix
	}
	csilog.NewLogger(logAttribute)

	// When serviceType is neither plugin nor provisioner, the program will exits.
	if serviceType != utils.PluginService && serviceType != utils.ProvisionerService {
		csilog.Log.Fatalf("Service type is unknown:%s", serviceType)
	}
	// enable pprof analyse
	pprofPort := os.Getenv("PPROF_PORT")
	if pprofPort != "" {
		if _, err := strconv.Atoi(pprofPort); err == nil {
			csilog.Log.Infof("enable pprof & start port at %v", pprofPort)
			go func() {
				err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%v", pprofPort), nil)
				csilog.Log.Errorf("start server err: %v", err)
			}()
		}
	}

	csilog.Log.Infof("CSI Driver Branch: %s, Version: %s, Build time: %s\n", BRANCH, VERSION, BUILDTIME)

	endPointName := *endpoint
	var wg sync.WaitGroup

	// Storage devops
	go om.StorageOM()

	wg.Add(1)
	driverName := "ossplugin.csi.alibabacloud.com"

	if err := createPersistentStorage(path.Join(utils.KubeletRootDir, "/csi-plugins", driverName, "controller")); err != nil {
		csilog.Log.Errorf("failed to create persistent storage for controller: %v", err)
		os.Exit(1)
	}
	if err := createPersistentStorage(path.Join(utils.KubeletRootDir, "/csi-plugins", driverName, "node")); err != nil {
		csilog.Log.Errorf("failed to create persistent storage for node: %v", err)
		os.Exit(1)
	}
	go func(endPoint string) {
		defer wg.Done()
		driver := oss.NewDriver(*nodeID, endPoint)
		driver.Run()
	}(endPointName)
	servicePort := os.Getenv("SERVICE_PORT")

	if len(servicePort) == 0 || servicePort == "" {
		switch serviceType {
		case utils.PluginService:
			servicePort = PluginServicePort
		case utils.ProvisionerService:
			servicePort = ProvisionerServicePort
		default:
		}
	}

	metricConfig := &globalMetricConfig{
		true,
		"plugin",
	}

	enableMetric := os.Getenv("ENABLE_METRIC")
	setPrometheusVersion()
	if enableMetric == "false" {
		metricConfig.enableMetric = false
	}
	metricConfig.serviceType = serviceType

	csilog.Log.Info("CSI is running status.")
	server := &http.Server{Addr: ":" + servicePort}

	http.HandleFunc("/healthz", healthHandler)
	csilog.Log.Infof("Metric listening on address: /healthz")
	if metricConfig.enableMetric {
		metricHandler := metric.NewMetricHandler(metricConfig.serviceType)
		http.Handle("/metrics", metricHandler)
		csilog.Log.Infof("Metric listening on address: /metrics")
	}

	if err := server.ListenAndServe(); err != nil {
		csilog.Log.Fatalf("Service port listen and serve err:%s", err.Error())
	}

	wg.Wait()
	os.Exit(0)
}

func createPersistentStorage(persistentStoragePath string) error {
	csilog.Log.Infof("Create Stroage Path: %s", persistentStoragePath)
	return os.MkdirAll(persistentStoragePath, os.FileMode(0755))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	time := time.Now()
	message := "Liveness probe is OK, time:" + time.String()
	w.Write([]byte(message))
}
