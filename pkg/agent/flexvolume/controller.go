/*
Copyright 2017 The Rook Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Some of the code below came from https://github.com/coreos/etcd-operator
which also has the apache 2.0 license.
*/

// Package flexvolume to manage Kubernetes storage attach events.
package flexvolume

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rook/rook/pkg/agent/flexvolume/crd"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/operator/cluster"
	"github.com/rook/rook/pkg/operator/k8sutil"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/util/exec"
	k8smount "k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/util/version"
	"k8s.io/kubernetes/pkg/volume/util"
)

const (
	StorageClassKey   = "storageClass"
	PoolKey           = "pool"
	ImageKey          = "image"
	serverVersionV170 = "v1.7.0"
)

// FlexvolumeController handles all events from the Flexvolume driver
type FlexvolumeController struct {
	clientset                  kubernetes.Interface
	volumeManager              VolumeManager
	volumeAttachmentController crd.VolumeAttachmentController
	mounter                    *k8smount.SafeFormatAndMount
}

func newFlexvolumeController(context *clusterd.Context, volumeAttachmentCRDClient rest.Interface, manager VolumeManager) (*FlexvolumeController, error) {

	var controller crd.VolumeAttachmentController
	// CRD is available on v1.7.0. TPR became deprecated on v1.7.0
	// Remove this code when TPR is not longer supported
	kubeVersion, err := k8sutil.GetK8SVersion(context.Clientset)
	if err != nil {
		return nil, fmt.Errorf("Error getting server version: %v", err)
	}
	if kubeVersion.AtLeast(version.MustParseSemantic(serverVersionV170)) {
		controller = crd.New(volumeAttachmentCRDClient)
	} else {
		controller = crd.NewTPR(context.Clientset)
	}

	mounter := &k8smount.SafeFormatAndMount{
		Interface: k8smount.New("" /* default mount path */),
		Runner:    exec.New(),
	}

	return &FlexvolumeController{
		clientset:                  context.Clientset,
		volumeManager:              manager,
		volumeAttachmentController: controller,
		mounter:                    mounter,
	}, nil
}

// Mount implements the flexvolume Mount(). It attaches and mount a rook volume to the node
func (c *FlexvolumeController) Mount(attachOpts AttachOptions, _ *struct{} /* void reply */) error {

	logger.Debug("Performing Mount operation with args: %+v", attachOpts)

	// Get extra attach information from mountDir
	c.getAttachInfoFromMountDir(&attachOpts)

	// Attach volume to node
	devicePath, err := c.attach(attachOpts)
	if err != nil {
		return err
	}

	// Mount devicePath to global mount path
	err = c.mountDevice(attachOpts, devicePath)
	if err != nil {
		return err
	}

	// Mount global mount path to pod mountDir on host
	err = c.mount(attachOpts)
	if err != nil {
		return err
	}

	return nil
}

// Unmount implements the flexvolume Unmount(). It unmounts and detaches a rook volume from the node
func (c *FlexvolumeController) Unmount(detachOpts AttachOptions, _ *struct{} /* void reply */) error {

	logger.Debug("Performing Unmount operation with args: %+v", detachOpts)

	// Get extra detach information from mountDir
	c.getAttachInfoFromMountDir(&detachOpts)

	// Get list of path of all other references to the device referenced by mountDir
	refs, err := k8smount.GetMountRefs(c.mounter.Interface, detachOpts.MountDir)
	if err != nil {
		return fmt.Errorf("failed to get reference mount %s: %+v", detachOpts.MountDir, err)
	}

	// Unmount volume from mountDir
	err = c.unmount(detachOpts)
	if err != nil {
		return err
	}

	// If len(refs) is 1, then all bind mounts have been removed, and the
	// remaining reference is the global mount. It is safe to detach.
	if len(refs) == 1 {
		// Unmount global mount dir
		if err := c.unmountDevice(detachOpts); err != nil {
			return err
		}
		// Detach volume from node
		if err := c.detach(detachOpts); err != nil {
			return err
		}
	}
	return nil
}

// Attach attaches rook volume to the node, creates a VolumeAttachment CRD and returns the device path
func (c *FlexvolumeController) attach(attachOpts AttachOptions) (string, error) {

	logger.Infof("Attaching volume %s/%s", attachOpts.Pool, attachOpts.Image)
	namespace := os.Getenv(k8sutil.PodNamespaceEnvVar)
	node := os.Getenv(k8sutil.NodeNameEnvVar)

	// Name of CRD is the PV name. This is done so that the CRD can be use for fencing
	crdName := attachOpts.VolumeName

	// Check if this volume has been attached
	volumeattachObj, err := c.volumeAttachmentController.Get(namespace, crdName)
	if err != nil {
		if !errors.IsNotFound(err) {
			return "", fmt.Errorf("failed to get volume CRD %s. %+v", crdName, err)
		}
		// No volumeattach CRD for this volume found. Create one
		volumeattachObj = crd.NewVolumeAttachment(crdName, namespace, node, attachOpts.PodNamespace, attachOpts.Pod,
			attachOpts.MountDir, strings.ToLower(attachOpts.RW) == ReadOnly)
		logger.Infof("Creating Volume attach Resource %s/%s: %+v", volumeattachObj.Namespace, volumeattachObj.Name, attachOpts)
		err = c.volumeAttachmentController.Create(volumeattachObj)
		if err != nil {
			if !errors.IsAlreadyExists(err) {
				return "", fmt.Errorf("failed to create volume CRD %s. %+v", crdName, err)
			}
			// Some other attacher beat us in this race. Kubernetes will retry again.
			return "", fmt.Errorf("failed to attach volume %s. Volume is already attached by a different pod", crdName)
		}
	} else {
		// Volume has already been attached.
		// Check if there is already an attachment with RW.
		index := getPodRWAttachmentObject(volumeattachObj)
		if index != -1 {
			// check if the RW attachment is orphaned.
			attachment := &volumeattachObj.Attachments[index]
			pod, err := c.clientset.Core().Pods(attachment.PodNamespace).Get(attachment.PodName, metav1.GetOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					return "", fmt.Errorf("failed to get pod CRD %s/%s. %+v", attachment.PodNamespace, attachment.PodName, err)
				}

				// Attachment is orphaned. Update attachment record and proceed with attaching
				attachment.Node = node
				attachment.MountDir = attachOpts.MountDir
				attachment.PodNamespace = attachOpts.PodNamespace
				attachment.PodName = attachOpts.Pod
				attachment.ReadOnly = attachOpts.RW == ReadOnly
				err = c.volumeAttachmentController.Update(volumeattachObj)
				if err != nil {
					return "", fmt.Errorf("failed to update volume CRD %s. %+v", crdName, err)
				}
			} else {
				// Attachment is not orphaned. Original pod still exists. Dont attach.
				return "", fmt.Errorf("failed to attach volume %s. Volume is already attached by pod %s/%s. Status %+v", crdName, attachment.PodNamespace, attachment.PodName, pod.Status.Phase)
			}
		} else {
			// No RW attachment found. Check if this is a RW attachment request.
			// We only support RW once attachment. No mixing either with RO
			if attachOpts.RW == "rw" && len(volumeattachObj.Attachments) > 0 {
				return "", fmt.Errorf("failed to attach volume %s. Volume is already attached by one or more pods", crdName)
			}

		}

		// find if the attachment object has been previously created
		found := false
		for _, a := range volumeattachObj.Attachments {
			if a.MountDir == attachOpts.MountDir {
				found = true
			}
		}

		if !found {
			// Create a new attachment record and proceed with attaching
			newAttach := crd.Attachment{
				Node:         node,
				PodNamespace: attachOpts.PodNamespace,
				PodName:      attachOpts.Pod,
				MountDir:     attachOpts.MountDir,
				ReadOnly:     attachOpts.RW == ReadOnly,
			}
			volumeattachObj.Attachments = append(volumeattachObj.Attachments, newAttach)
			err = c.volumeAttachmentController.Update(volumeattachObj)
			if err != nil {
				return "", fmt.Errorf("failed to update volume CRD %s. %+v", crdName, err)
			}
		}
	}
	devicePath, err := c.volumeManager.Attach(attachOpts.Image, attachOpts.Pool, attachOpts.ClusterName)
	if err != nil {
		return "", fmt.Errorf("failed to attach volume %s/%s: %+v", attachOpts.Pool, attachOpts.Image, err)
	}

	return devicePath, nil
}

// mountDevice mounts the volume device path to a global mount in the host
func (c *FlexvolumeController) mountDevice(opts AttachOptions, devicePath string) error {

	globalVolumeMountPath := getGlobalMountPath(opts.VolumeName)
	logger.Infof("Mounting volume %s/%s at global mount path %s to %s", opts.Pool, opts.Image, globalVolumeMountPath, opts.MountDir)

	notMnt, err := c.mounter.Interface.IsLikelyNotMountPoint(globalVolumeMountPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(globalVolumeMountPath, 0750); err != nil {
				return fmt.Errorf("cannot create global volume mount path dir %s: %v", globalVolumeMountPath, err)
			}
			notMnt = true
		} else {
			return fmt.Errorf("error checking if %s is a mount point: %v", globalVolumeMountPath, err)
		}
	}
	options := []string{opts.RW}
	if notMnt {
		if err = c.mounter.FormatAndMount(devicePath, globalVolumeMountPath, opts.FsType, options); err != nil {
			os.Remove(globalVolumeMountPath)
			return fmt.Errorf("failed to mount volume at device path %s [%s] to %s, error %v", devicePath, opts.FsType, globalVolumeMountPath, err)
		}
		logger.Info("Ignore error about Mount failed: exit status 32. Kubernetes does this to check whether the volume has been formatted. It will format and retry again. https://github.com/kubernetes/kubernetes/blob/release-1.7/pkg/util/mount/mount_linux.go#L360")
		logger.Infof("formatting volume %v devicePath %v deviceMountPath %v fs %v with options %+v", opts.VolumeName, devicePath, globalVolumeMountPath, opts.FsType, options)
	}
	return nil
}

// mount mounts the global mount to the expected mountDir in the host
func (c *FlexvolumeController) mount(opts AttachOptions) error {

	globalVolumeMountPath := getGlobalMountPath(opts.VolumeName)
	logger.Infof("Mounting volume %s/%s at global mount path %s to %s", opts.Pool, opts.Image, globalVolumeMountPath, opts.MountDir)

	// Perform a bind mount to the full path to allow duplicate mounts of the same volume. This is only supported for RO attachments.
	options := append([]string{opts.RW}, "bind")
	err := c.mounter.Interface.Mount(globalVolumeMountPath, opts.MountDir, "", options)
	if err != nil {
		notMnt, mntErr := c.mounter.Interface.IsLikelyNotMountPoint(opts.MountDir)
		if mntErr != nil {
			return fmt.Errorf("IsLikelyNotMountPoint check failed on %s: %v", opts.MountDir, mntErr)
		}
		if !notMnt {
			if mntErr = c.mounter.Interface.Unmount(opts.MountDir); mntErr != nil {
				return fmt.Errorf("failed to unmount %s: %v", opts.MountDir, mntErr)
			}
			notMnt, mntErr := c.mounter.Interface.IsLikelyNotMountPoint(opts.MountDir)
			if mntErr != nil {
				return fmt.Errorf("IsLikelyNotMountPoint check failed on %s: %v", opts.MountDir, mntErr)
			}
			if !notMnt {
				// This is very odd, we don't expect it.  We'll try again next sync loop.
				return fmt.Errorf("%s is still mounted, despite call to unmount().  Will try again next sync loop", opts.MountDir)
			}
		}
		os.Remove(opts.MountDir)
		return fmt.Errorf("failed to mount volume %s/%s at global mount path %s to %s, error %v", opts.Pool, opts.Image, globalVolumeMountPath, opts.MountDir, err)
	}

	logger.Infof("volume %s/%s has been attached and mounted to %s", opts.Pool, opts.Image, opts.MountDir)
	return nil
}

// unmount the mountDir
func (c *FlexvolumeController) unmount(opts AttachOptions) error {

	logger.Infof("Unmounting volume %s/%s at %s", opts.Pool, opts.Image, opts.MountDir)

	// Unmount pod mount dir
	if err := util.UnmountPath(opts.MountDir, c.mounter.Interface); err != nil {
		return fmt.Errorf("failed to unmount volume at %s: %+v", opts.MountDir, err)
	}

	// Remove attachment item from the CRD
	if err := c.removeAttachmentObject(opts); err != nil {
		return fmt.Errorf("failed to remove attachment item from CRD for mount dir %s: %+v", opts.MountDir, err)
	}

	return nil
}

// unmountDevice unmounts the global mount path from the host
func (c *FlexvolumeController) unmountDevice(opts AttachOptions) error {

	globalVolumeMountPath := getGlobalMountPath(opts.VolumeName)
	logger.Infof("Unmounting volume %s/%s at global mount path %s", opts.Pool, opts.Image, globalVolumeMountPath)

	if err := util.UnmountPath(globalVolumeMountPath, c.mounter.Interface); err != nil {
		return fmt.Errorf("failed to unmount volume at %s: %+v", globalVolumeMountPath, err)
	}

	return nil
}

// detach detaches the volume from the node
func (c *FlexvolumeController) detach(opts AttachOptions) error {

	err := c.volumeManager.Detach(opts.Image, opts.Pool, opts.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to detach volume %s/%s: %+v", opts.Pool, opts.Image, err)
	}

	// Volume detached. Delete CRD is volume is no longer attached to any node.
	namespace := os.Getenv(k8sutil.PodNamespaceEnvVar)
	crdName := opts.VolumeName
	volumeAttach, err := c.volumeAttachmentController.Get(namespace, crdName)
	if len(volumeAttach.Attachments) == 0 {
		logger.Infof("Deleting VolumeAttachment CRD %s/%s", namespace, crdName)
		return c.volumeAttachmentController.Delete(namespace, crdName)
	}
	return nil
}

// removeAttachmentObject removes the attachment from the VolumeAttachment CRD
func (c *FlexvolumeController) removeAttachmentObject(detachOpts AttachOptions) error {
	namespace := os.Getenv(k8sutil.PodNamespaceEnvVar)
	crdName := detachOpts.VolumeName
	logger.Infof("Deleting attachment for mountDir %s from Volume attach CRD %s/%s", detachOpts.MountDir, namespace, crdName)
	volumeAttach, err := c.volumeAttachmentController.Get(namespace, crdName)
	if err != nil {
		return fmt.Errorf("failed to get Volume attach CRD %s/%s: %+v", namespace, crdName, err)
	}
	for i, v := range volumeAttach.Attachments {
		if v.MountDir == detachOpts.MountDir {
			// Deleting slice
			volumeAttach.Attachments = append(volumeAttach.Attachments[:i], volumeAttach.Attachments[i+1:]...)

			// Update CRD.
			return c.volumeAttachmentController.Update(volumeAttach)
		}
	}
	return fmt.Errorf("VolumeAttachment CRD %s found but attachment to the mountDir %s was not found", crdName, detachOpts.MountDir)
}

func (c *FlexvolumeController) parseClusterName(storageClassName string) (string, error) {
	sc, err := c.clientset.Storage().StorageClasses().Get(storageClassName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	clusterName, ok := sc.Parameters["clusterName"]
	if !ok {
		// Defaults to rook if not found
		logger.Infof("clusterName not specified in the storage class %s. Defaulting to '%s'", storageClassName, cluster.DefaultClusterName)
		return cluster.DefaultClusterName, nil
	}
	return clusterName, nil
}

// getAttachInfoFromMountDir obtain pod and volume information from the mountDir. K8s does not provide
// all necessary information to detach a volume (https://github.com/kubernetes/kubernetes/issues/52590).
// So we are hacking a bit and by parsing it from mountDir
func (c *FlexvolumeController) getAttachInfoFromMountDir(attachOptions *AttachOptions) error {

	if attachOptions.PodID == "" {
		podID, pvName, err := getPodAndPVNameFromMountDir(attachOptions.MountDir)
		if err != nil {
			return err
		}
		attachOptions.PodID = podID
		attachOptions.VolumeName = pvName
	}

	pv, err := c.clientset.CoreV1().PersistentVolumes().Get(attachOptions.VolumeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get persistent volume %s: %+v", attachOptions.VolumeName, err)
	}

	if attachOptions.PodNamespace == "" {
		// pod namespace should be the same as the PVC namespace
		attachOptions.PodNamespace = pv.Spec.ClaimRef.Namespace
	}

	node := os.Getenv(k8sutil.NodeNameEnvVar)
	if attachOptions.Pod == "" {
		// Find all pods scheduled to this node
		opts := metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String(),
		}
		pods, err := c.clientset.Core().Pods(attachOptions.PodNamespace).List(opts)
		if err != nil {
			return fmt.Errorf("failed to get pods in namespace %s: %+v", attachOptions.PodNamespace, err)
		}

		pod := findPodByID(pods, types.UID(attachOptions.PodID))
		if pod != nil {
			attachOptions.Pod = pod.GetName()
		}
	}

	if attachOptions.Image == "" {
		attachOptions.Image = pv.Spec.PersistentVolumeSource.FlexVolume.Options[ImageKey]
	}
	if attachOptions.Pool == "" {
		attachOptions.Pool = pv.Spec.PersistentVolumeSource.FlexVolume.Options[PoolKey]
	}
	if attachOptions.StorageClass == "" {
		attachOptions.StorageClass = pv.Spec.PersistentVolumeSource.FlexVolume.Options[StorageClassKey]
	}
	attachOptions.ClusterName, err = c.parseClusterName(attachOptions.StorageClass)
	if err != nil {
		return fmt.Errorf("Failed to parse clusterName from storageClass %s: %+v", attachOptions.StorageClass, err)
	}
	return nil
}

// getGlobalMountPath generate the global mount path where the device path is mounted.
// It is based on the kubelet root dir, which defaults to /var/lib/kubelet
func getGlobalMountPath(volumeName string) string {
	kubeletRootDir := os.Getenv(k8sutil.KubeletRootDirPathDirEnv)
	return path.Join(kubeletRootDir, "plugins", FlexvolumeVendor, FlexvolumeDriver, "mounts", volumeName)
}

// getPodAndPVNameFromMountDir parses pod information from the mountDir
func getPodAndPVNameFromMountDir(mountDir string) (string, string, error) {
	// mountDir is in the form of <rootDir>/pods/<podID>/volumes/rook.io~rook/<pv name>
	filepath.Clean(mountDir)
	token := strings.Split(mountDir, string(filepath.Separator))
	// token lenght should at least size 5
	length := len(token)
	if length < 5 {
		return "", "", fmt.Errorf("failed to parse mountDir %s for CRD name and podID", mountDir)
	}
	return token[length-4], token[length-1], nil
}

func findPodByID(pods *v1.PodList, podUID types.UID) *v1.Pod {
	for i := range pods.Items {
		if pods.Items[i].GetUID() == podUID {
			return &(pods.Items[i])
		}
	}
	return nil
}

// getPodRWAttachmentObject loops through the list of attachments of the VolumeAttachment
// resource and returns the index of the first RW attachment object
func getPodRWAttachmentObject(volumeAttachmentObject crd.VolumeAttachment) int {
	for i, a := range volumeAttachmentObject.Attachments {
		if !a.ReadOnly {
			return i
		}
	}
	return -1
}
