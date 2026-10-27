package plugin

import (
	"fmt"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	kvcore "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
	"kubevirt.io/kubevirt-velero-plugin/pkg/util/kvgraph"
)

// persistentStateLabel names the VM whose persistent state (TPM, EFI) a PVC
// holds; KubeVirt finds the PVC by it.
const persistentStateLabel = "persistent-state-for"

var (
	pvcResource                = kvgraph.KVObjectGraph["persistentvolumeclaims"].String()
	dataVolumeResource         = kvgraph.KVObjectGraph["datavolumes"].String()
	secretResource             = kvgraph.KVObjectGraph["secrets"].String()
	configMapResource          = kvgraph.KVObjectGraph["configmaps"].String()
	controllerRevisionResource = kvgraph.KVObjectGraph["controllerrevisions"].String()
)

// renameVM points a VM restored under a new name at the restore's copies of
// the disks, secrets, config maps and revisions it uses, so that it shares
// none of them with the original. Labels and the hostname that carry the old
// name get the new one. Service accounts and networks stay shared.
func renameVM(vm *kvcore.VirtualMachine, rename *util.VMOptions) error {
	var missing []string
	copyOf := func(resource, name string) string {
		if newName := rename.NewNameOf(resource, name); newName != "" {
			return newName
		}
		if !slices.Contains(missing, resource+" "+name) {
			missing = append(missing, resource+" "+name)
		}
		return name
	}
	// A DataVolume and its PVC share a name. CDI may have deleted the DataVolume
	// after its import, so only the PVC is in the backup.
	dataVolumeCopyOf := func(name string) string {
		if newName := rename.NewNameOf(dataVolumeResource, name); newName != "" {
			return newName
		}
		return copyOf(pvcResource, name)
	}

	for i := range vm.Spec.DataVolumeTemplates {
		vm.Spec.DataVolumeTemplates[i].Name = dataVolumeCopyOf(vm.Spec.DataVolumeTemplates[i].Name)
	}
	if vm.Spec.Instancetype != nil && vm.Spec.Instancetype.RevisionName != "" {
		vm.Spec.Instancetype.RevisionName = copyOf(controllerRevisionResource, vm.Spec.Instancetype.RevisionName)
	}
	if vm.Spec.Preference != nil && vm.Spec.Preference.RevisionName != "" {
		vm.Spec.Preference.RevisionName = copyOf(controllerRevisionResource, vm.Spec.Preference.RevisionName)
	}

	spec := &vm.Spec.Template.Spec
	for i := range spec.Volumes {
		volume := &spec.Volumes[i]
		switch {
		case volume.DataVolume != nil:
			volume.DataVolume.Name = dataVolumeCopyOf(volume.DataVolume.Name)
		case volume.PersistentVolumeClaim != nil:
			volume.PersistentVolumeClaim.ClaimName = copyOf(pvcResource, volume.PersistentVolumeClaim.ClaimName)
		case volume.MemoryDump != nil:
			volume.MemoryDump.ClaimName = copyOf(pvcResource, volume.MemoryDump.ClaimName)
		case volume.ConfigMap != nil:
			volume.ConfigMap.Name = copyOf(configMapResource, volume.ConfigMap.Name)
		case volume.Secret != nil:
			volume.Secret.SecretName = copyOf(secretResource, volume.Secret.SecretName)
		case volume.CloudInitNoCloud != nil:
			renameSecretRefs(copyOf, volume.CloudInitNoCloud.UserDataSecretRef, volume.CloudInitNoCloud.NetworkDataSecretRef)
		case volume.CloudInitConfigDrive != nil:
			renameSecretRefs(copyOf, volume.CloudInitConfigDrive.UserDataSecretRef, volume.CloudInitConfigDrive.NetworkDataSecretRef)
		case volume.Sysprep != nil:
			// The backup does not follow sysprep sources, so they are copied only when listed.
			if ref := volume.Sysprep.Secret; ref != nil && rename.NewNameOf(secretResource, ref.Name) != "" {
				ref.Name = rename.NewNameOf(secretResource, ref.Name)
			}
			if ref := volume.Sysprep.ConfigMap; ref != nil && rename.NewNameOf(configMapResource, ref.Name) != "" {
				ref.Name = rename.NewNameOf(configMapResource, ref.Name)
			}
		}
	}
	for i := range spec.AccessCredentials {
		credential := &spec.AccessCredentials[i]
		if credential.SSHPublicKey != nil && credential.SSHPublicKey.Source.Secret != nil {
			credential.SSHPublicKey.Source.Secret.SecretName = copyOf(secretResource, credential.SSHPublicKey.Source.Secret.SecretName)
		}
		if credential.UserPassword != nil && credential.UserPassword.Source.Secret != nil {
			credential.UserPassword.Source.Secret.SecretName = copyOf(secretResource, credential.UserPassword.Source.Secret.SecretName)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("VirtualMachine %s/%s cannot be restored as %s: the restore has no copy of %s, which it uses",
			rename.Namespace, rename.Name, rename.NewName, strings.Join(missing, ", "))
	}

	if spec.Hostname == rename.Name {
		spec.Hostname = rename.NewName
	}
	renameLabelValues(vm.Labels, rename.Name, rename.NewName)
	renameLabelValues(vm.Spec.Template.ObjectMeta.Labels, rename.Name, rename.NewName)
	return nil
}

func renameSecretRefs(copyOf func(resource, name string) string, refs ...*corev1.LocalObjectReference) {
	for _, ref := range refs {
		if ref != nil {
			ref.Name = copyOf(secretResource, ref.Name)
		}
	}
}

func renameLabelValues(labels map[string]string, oldValue, newValue string) {
	for key, value := range labels {
		if value == oldValue {
			labels[key] = newValue
		}
	}
}

// warnAboutCopiedCloudInit tells the user that a VM restored under a new name
// boots with a copy of its cloud-init configuration. KAS shows the message in
// the job's activity by its fixed phrase "with a copy of its cloud-init configuration".
func warnAboutCopiedCloudInit(log logrus.FieldLogger, vm *kvcore.VirtualMachine, rename *util.VMOptions, targetNamespace string) {
	var secrets []string
	hasCloudInit := false
	for _, volume := range vm.Spec.Template.Spec.Volumes {
		var refs []*corev1.LocalObjectReference
		switch {
		case volume.CloudInitNoCloud != nil:
			refs = []*corev1.LocalObjectReference{volume.CloudInitNoCloud.UserDataSecretRef, volume.CloudInitNoCloud.NetworkDataSecretRef}
		case volume.CloudInitConfigDrive != nil:
			refs = []*corev1.LocalObjectReference{volume.CloudInitConfigDrive.UserDataSecretRef, volume.CloudInitConfigDrive.NetworkDataSecretRef}
		default:
			continue
		}
		hasCloudInit = true
		for _, ref := range refs {
			if ref != nil && !slices.Contains(secrets, ref.Name) {
				secrets = append(secrets, ref.Name)
			}
		}
	}
	if !hasCloudInit {
		return
	}
	where := "the VirtualMachine's cloud-init volume"
	if len(secrets) > 0 {
		where = "secret " + strings.Join(secrets, ", ")
	}
	log.Warnf("VirtualMachine %s/%s is restored as %s/%s with a copy of its cloud-init configuration. "+
		"If it sets static IP addresses, change them in %s so that they do not conflict with the original VM",
		rename.Namespace, rename.Name, targetNamespace, rename.NewName, where)
}

// renamedFrom returns the backed up name of an item the engine restores under
// a new name, or "" when the item keeps its name.
func renamedFrom(itemFromBackup runtime.Unstructured, name string) string {
	if itemFromBackup == nil {
		return ""
	}
	backedUp, err := meta.Accessor(itemFromBackup)
	if err != nil || backedUp.GetName() == name {
		return ""
	}
	return backedUp.GetName()
}
