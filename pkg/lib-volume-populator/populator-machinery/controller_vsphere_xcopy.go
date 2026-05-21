package populator_machinery

import (
	"context"
	"time"

	api "github.com/kubev2v/forklift/pkg/apis/forklift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// vSphere copy-offload controller behavior (VSphereXcopyVolumePopulator CR only).
//
// Scope — what this file changes:
//   - Applies to every vSphere copy-offload migration (all storage vendors configured
//     in the StorageMap: Primera, Pure, PowerMax, etc.), not to oVirt/OpenStack populators.
//   - Does not affect classic VDDK/streaming copy (no VSphereXcopyVolumePopulator CR).
//
// Inside the vsphere-copy-offload-populator binary, disk type selects the clone path:
//   - VVol / RDM on Primera → WSAPI CopyVolume in par3client.go (unmap-before-delete there).
//   - Regular VMDK disks → VIB/SSH remote esxcli xcopy (never calls Primera CopyVolume).
//
// The populate pod must not reference or mount the prime PVC for any VSphereXcopy
// populator: kubelet attachment triggers CSI NodePublish while array-side work runs,
// which caused promote 403 "Volume is exported" and stale multipath on virt-v2v.
const vsphereXcopyPrimeRequeueDelay = 2 * time.Second

func (c *controller) isVSphereXcopyPopulator() bool {
	return c.gk.Kind == api.VSphereXcopyVolumePopulatorKind
}

// populatorPodReferencesPrimeVolume reports whether the populate pod should list the
// prime PVC in spec.volumes. oVirt/OpenStack need it for data transfer; vSphere XCOPY does not.
func (c *controller) populatorPodReferencesPrimeVolume() bool {
	return !c.isVSphereXcopyPopulator()
}

// populatorMountsPrimeVolume reports whether the populator container should mount the
// prime volume at /dev/block or a filesystem path. vSphere XCOPY only needs API access
// to the array and reads volumeHandle from the prime PV via the Kubernetes API.
func (c *controller) populatorMountsPrimeVolume() bool {
	return !c.isVSphereXcopyPopulator()
}

// ensureVSphereXcopyPrimeReady creates the prime PVC if missing and waits until it is
// Bound before the populate pod is created. Returns ready=false when the caller should
// requeue sync (prime just created or still provisioning).
func (c *controller) ensureVSphereXcopyPrimeReady(
	ctx context.Context,
	key string,
	pvc *corev1.PersistentVolumeClaim,
	pvcPrime *corev1.PersistentVolumeClaim,
	pvcPrimeName, populatorNamespace string,
	waitForFirstConsumer bool,
	nodeName string,
) (ready bool, err error) {
	if !c.isVSphereXcopyPopulator() {
		return true, nil
	}

	if pvcPrime == nil {
		if err = c.createPrimePVC(ctx, pvc, pvcPrimeName, populatorNamespace, waitForFirstConsumer, nodeName); err != nil {
			return false, err
		}
		// syncPvc returns without error and the workqueue entry is Forgotten; re-add explicitly.
		c.requeuePVC(key, vsphereXcopyPrimeRequeueDelay)
		return false, nil
	}

	if pvcPrime.Status.Phase != corev1.ClaimBound || pvcPrime.Spec.VolumeName == "" {
		// Immediate SC binds on create; WFFC binds after selected-node — both without prime in populate pod spec.
		c.requeuePVC(key, vsphereXcopyPrimeRequeueDelay)
		return false, nil
	}

	return true, nil
}

func (c *controller) createPrimePVC(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
	pvcPrimeName, populatorNamespace string,
	waitForFirstConsumer bool,
	nodeName string,
) error {
	pvcPrime := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcPrimeName,
			Namespace: populatorNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
					Name:       pvc.Name,
					UID:        pvc.UID,
				},
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      pvc.Spec.AccessModes,
			Resources:        pvc.Spec.Resources,
			StorageClassName: pvc.Spec.StorageClassName,
			VolumeMode:       pvc.Spec.VolumeMode,
		},
	}
	if waitForFirstConsumer {
		pvcPrime.Annotations = map[string]string{
			annSelectedNode: nodeName,
		}
	}
	_, err := c.kubeClient.CoreV1().PersistentVolumeClaims(populatorNamespace).Create(ctx, pvcPrime, metav1.CreateOptions{})
	if err != nil {
		c.recorder.Eventf(pvc, corev1.EventTypeWarning, reasonPVCCreationError, "Failed to create populator PVC: %s", err)
		return err
	}
	return nil
}

// makePopulatePodSpecNoPrimeVolume builds a populate pod that only mounts the populator
// secret (credentials). Used for vSphere XCOPY so CSI never publishes the prime LUN on the populate node.
func makePopulatePodSpecNoPrimeVolume(secretName string) corev1.PodSpec {
	spec := makePopulatePodSpec("", secretName)
	spec.Volumes = nil
	return spec
}
