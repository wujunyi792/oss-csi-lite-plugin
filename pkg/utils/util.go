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

package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	utilexec "k8s.io/utils/exec"
	"k8s.io/utils/mount"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	k8svol "k8s.io/kubernetes/pkg/volume"
	k8sfs "k8s.io/kubernetes/pkg/volume/util/fs"

	"wujunyi792/oss-csi-lite-plugin/pkg/options"
)

// DefaultOptions used for global ak
type DefaultOptions struct {
	Global struct {
		KubernetesClusterTag string
		AccessKeyID          string `json:"accessKeyID"`
		AccessKeySecret      string `json:"accessKeySecret"`
		Region               string `json:"region"`
	}
}

const (
	// UserAKID is user AK ID
	UserAKID = "/etc/.volumeak/akId"
	// UserAKSecret is user AK Secret
	UserAKSecret = "/etc/.volumeak/akSecret"
	// MetadataURL is metadata url
	MetadataURL = "http://100.100.100.200/latest/meta-data/"
	// RegionIDTag is region id
	RegionIDTag = "region-id"
	// InstanceIDTag is instance id
	InstanceIDTag = "instance-id"
	// DefaultRegion is default region
	DefaultRegion = "cn-hangzhou"
	// CsiPluginRunTimeFlagFile tag
	CsiPluginRunTimeFlagFile = "../alibabacloudcsiplugin.json"
	// RuncRunTimeTag tag
	RuncRunTimeTag = "runc"
	// RunvRunTimeTag tag
	RunvRunTimeTag = "runv"
	// ServiceType tag
	ServiceType = "SERVICE_TYPE"
	// PluginService represents the csi-plugin type.
	PluginService = "plugin"
	// ProvisionerService represents the csi-provisioner type.
	ProvisionerService = "provisioner"
	// InstallSnapshotCRD tag
	InstallSnapshotCRD = "INSTALL_SNAPSHOT_CRD"
	// MetadataMaxRetrycount ...
	MetadataMaxRetrycount = 4
	// VolDataFileName file
	VolDataFileName = "vol_data.json"
	// fsckErrorsCorrected tag
	fsckErrorsCorrected = 1
	// fsckErrorsUncorrected tag
	fsckErrorsUncorrected = 4

	// NsenterCmd is the nsenter command
	NsenterCmd = "nsenter --mount=/proc/1/ns/mnt --ipc=/proc/1/ns/ipc --net=/proc/1/ns/net --uts=/proc/1/ns/uts"

	// socketPath is path of connector sock
	socketPath = "/host/etc/csi-tool/connector.sock"
)

// KubernetesAlicloudIdentity set a identity label
var KubernetesAlicloudIdentity = fmt.Sprintf("Kubernetes.Alicloud/CsiPlugin")

var (
	// NodeAddrMap map for NodeID and its Address
	NodeAddrMap = map[string]string{}
	// NodeAddrMutex Mutex for NodeAddr map
	NodeAddrMutex sync.RWMutex
)

// RoleAuth define STS Token Response
type RoleAuth struct {
	AccessKeyID     string
	AccessKeySecret string
	Expiration      time.Time
	SecurityToken   string
	LastUpdated     time.Time
	Code            string
}

// CreateEvent is create events
func CreateEvent(recorder record.EventRecorder, objectRef *v1.ObjectReference, eventType string, reason string, err string) {
	recorder.Event(objectRef, eventType, reason, err)
}

// NewEventRecorder is create snapshots event recorder
func NewEventRecorder() record.EventRecorder {
	cfg, err := clientcmd.BuildConfigFromFlags(options.MasterURL, options.Kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("NewControllerServer: Failed to create client: %v", err)
	}
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(log.Infof)
	source := v1.EventSource{Component: "csi-controller-server"}
	if broadcaster != nil {
		sink := &v1core.EventSinkImpl{
			Interface: v1core.New(clientset.CoreV1().RESTClient()).Events(""),
		}
		broadcaster.StartRecordingToSink(sink)
	}
	return broadcaster.NewRecorder(scheme.Scheme, source)
}

// Result struct definition
type Result struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Device  string `json:"device,omitempty"`
}

// CommandRunFunc define the run function in utils for ut
type CommandRunFunc func(cmd string) (string, error)

// Run command
func ValidateRun(cmd string) (string, error) {
	arr := strings.Split(cmd, " ")
	withArgs := false
	if len(arr) >= 2 {
		withArgs = true
	}

	name := arr[0]
	var args []string
	err := CheckCmd(cmd, name)
	if err != nil {
		return "", err
	}
	if withArgs {
		args = arr[1:]
		err = CheckCmdArgs(cmd, args...)
		if err != nil {
			return "", err
		}
	}
	safeMount := mount.SafeFormatAndMount{
		Interface: mount.New(""),
		Exec:      utilexec.New(),
	}
	var command utilexec.Cmd
	if withArgs {
		command = safeMount.Exec.Command(name, args...)
	} else {
		command = safeMount.Exec.Command(name)
	}

	stdout, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed to exec command:%s, name:%s, args:%+v, stdout:%s, stderr:%s", cmd, name, args, string(stdout), err.Error())
	}
	log.Infof("Exec command %s is successfully, name:%s, args:%+v", cmd, name, args)
	return string(stdout), nil
}

// run shell command
func Run(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed to run cmd: " + cmd + ", with out: " + string(out) + ", with error: " + err.Error())
	}
	return string(out), nil
}

func RunWithFilter(cmd string, filter ...string) ([]string, error) {
	ans := make([]string, 0)
	stdout, err := Run(cmd)
	if err != nil {
		return nil, err
	}
	stdoutArr := strings.Split(string(stdout), "\n")
	for _, stdout := range stdoutArr {
		find := true
		for _, f := range filter {
			if !strings.Contains(stdout, f) {
				find = false
			}
		}
		if find {
			ans = append(ans, stdout)
		}
	}
	return ans, nil
}

// CreateDest create de destination dir
func CreateDest(dest string) error {
	fi, err := os.Lstat(dest)

	if os.IsNotExist(err) {
		if err := os.MkdirAll(dest, 0777); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if fi != nil && !fi.IsDir() {
		return fmt.Errorf("%v already exist but it's not a directory", dest)
	}
	return nil
}

// IsLikelyNotMountPoint return status of mount point,this function fix IsMounted return 0 bug
// IsLikelyNotMountPoint determines if a directory is not a mountpoint.
// It is fast but not necessarily ALWAYS correct. If the path is in fact
// a bind mount from one part of a mount to another it will not be detected.
// It also can not distinguish between mountpoints and symbolic links.
// mkdir /tmp/a /tmp/b; mount --bind /tmp/a /tmp/b; IsLikelyNotMountPoint("/tmp/b")
// will return true. When in fact /tmp/b is a mount point. If this situation
// is of interest to you, don't use this function...
func IsLikelyNotMountPoint(file string) (bool, error) {
	stat, err := os.Stat(file)
	if err != nil {
		return true, err
	}
	rootStat, err := os.Stat(filepath.Dir(strings.TrimSuffix(file, "/")))
	if err != nil {
		return true, err
	}
	// If the directory has a different device as parent, then it is a mountpoint.
	if stat.Sys().(*syscall.Stat_t).Dev != rootStat.Sys().(*syscall.Stat_t).Dev {
		return false, nil
	}

	return true, nil
}

// IsFileExisting check file exist in volume driver or not
func IsFileExisting(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}

// ReadJSONFile return a json object
func ReadJSONFile(file string) (map[string]string, error) {
	jsonObj := map[string]string{}
	raw, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &jsonObj)
	if err != nil {
		return nil, err
	}
	return jsonObj, nil
}

// GetMetrics get path metric
func GetMetrics(path string) (*csi.NodeGetVolumeStatsResponse, error) {
	if path == "" {
		return nil, fmt.Errorf("getMetrics No path given")
	}
	available, capacity, usage, inodes, inodesFree, inodesUsed, err := k8sfs.FsInfo(path)
	if err != nil {
		return nil, err
	}

	metrics := &k8svol.Metrics{Time: metav1.Now()}
	metrics.Available = resource.NewQuantity(available, resource.BinarySI)
	metrics.Capacity = resource.NewQuantity(capacity, resource.BinarySI)
	metrics.Used = resource.NewQuantity(usage, resource.BinarySI)
	metrics.Inodes = resource.NewQuantity(inodes, resource.BinarySI)
	metrics.InodesFree = resource.NewQuantity(inodesFree, resource.BinarySI)
	metrics.InodesUsed = resource.NewQuantity(inodesUsed, resource.BinarySI)

	metricAvailable, ok := (*(metrics.Available)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch available bytes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch available bytes")
	}
	metricCapacity, ok := (*(metrics.Capacity)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch capacity bytes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch capacity bytes")
	}
	metricUsed, ok := (*(metrics.Used)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch used bytes for target %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch used bytes")
	}
	metricInodes, ok := (*(metrics.Inodes)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch available inodes for target %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch available inodes")
	}
	metricInodesFree, ok := (*(metrics.InodesFree)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch free inodes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch free inodes")
	}
	metricInodesUsed, ok := (*(metrics.InodesUsed)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch used inodes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch used inodes")
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: metricAvailable,
				Total:     metricCapacity,
				Used:      metricUsed,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: metricInodesFree,
				Total:     metricInodes,
				Used:      metricInodesUsed,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

// WriteAndSyncFile behaves just like ioutil.WriteFile in the standard library,
// but calls Sync before closing the file. WriteAndSyncFile guarantees the data
// is synced if there is no error returned.
func WriteAndSyncFile(filename string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = Fsync(f)
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

// Fsync is a wrapper around file.Sync(). Special handling is needed on darwin platform.
func Fsync(f *os.File) error {
	return f.Sync()
}

// IsHostFileExist is check host file is existing in lvm
func IsHostFileExist(path string) bool {
	args := []string{NsenterCmd, "stat", path}
	cmd := strings.Join(args, " ")
	out, err := Run(cmd)
	if err != nil && strings.Contains(out, "No such file or directory") {
		return false
	}

	return true
}

func DoMountInHost(mntCmd string) error {
	out, err := ConnectorRun(mntCmd)
	if err != nil {
		msg := fmt.Sprintf("Mount is failed in host, mntCmd:%s, err: %s, out: %s", mntCmd, err.Error(), out)
		log.Errorf(msg)
		return errors.New(msg)
	}
	return nil
}

// ConnectorRun Run shell command with host connector
// host connector is daemon running in host.
func ConnectorRun(cmd string) (string, error) {
	c, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Errorf("Oss connector Dial error: %s", err.Error())
		return err.Error(), err
	}
	defer c.Close()

	_, err = c.Write([]byte(cmd))
	if err != nil {
		log.Errorf("Oss connector write error: %s", err.Error())
		return err.Error(), err
	}

	buf := make([]byte, 2048)
	n, err := c.Read(buf[:])
	response := string(buf[0:n])
	if strings.HasPrefix(response, "Success") {
		respstr := response[8:]
		return respstr, nil
	}
	return response, errors.New("Exec command error:" + response)
}

func MkdirAll(path string, mode fs.FileMode) error {
	return os.MkdirAll(path, mode)
}

func WriteMetricsInfo(metricsPathPrefix string, req *csi.NodePublishVolumeRequest, metricsTop string, clientName string, storageBackendName string, fsName string) {
	podUIDPath := metricsPathPrefix + req.VolumeContext["csi.storage.k8s.io/pod.uid"] + "/"
	mountPointPath := podUIDPath + req.GetVolumeId() + "/"
	podInfoName := "pod_info"
	mountPointName := "mount_point_info"
	if !IsFileExisting(mountPointPath) {
		_ = MkdirAll(mountPointPath, os.FileMode(0755))
	}
	if !IsFileExisting(podUIDPath + podInfoName) {
		info := req.VolumeContext["csi.storage.k8s.io/pod.namespace"] + " " +
			req.VolumeContext["csi.storage.k8s.io/pod.name"] + " " +
			req.VolumeContext["csi.storage.k8s.io/pod.uid"] + " " +
			metricsTop
		_ = WriteAndSyncFile(podUIDPath+podInfoName, []byte(info), os.FileMode(0644))
	}

	if !IsFileExisting(mountPointPath + mountPointName) {
		info := clientName + " " +
			storageBackendName + " " +
			fsName + " " +
			req.GetVolumeId() + " " +
			req.TargetPath
		_ = WriteAndSyncFile(mountPointPath+mountPointName, []byte(info), os.FileMode(0644))
	}
}

func ParseProviderID(providerID string) string {
	providers := strings.Split(providerID, ".")
	if len(providers) != 2 {
		return ""
	}
	return providers[1]
}
