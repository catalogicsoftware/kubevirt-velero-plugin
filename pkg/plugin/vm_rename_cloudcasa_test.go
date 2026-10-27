package plugin

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kvcore "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
)

// backedUpVMToRename is vm1 as backed up: two disks, cloud-init, an SSH key,
// a secret and a config map volume, a service account and an instance type.
func backedUpVMToRename() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]interface{}{
			"name":      "vm1",
			"namespace": "ns1",
			"labels":    map[string]interface{}{"app": "vm1", "tier": "db"},
		},
		"spec": map[string]interface{}{
			"runStrategy":  "Always",
			"instancetype": map[string]interface{}{"name": "u1.medium", "revisionName": "vm1-u1.medium-1"},
			"dataVolumeTemplates": []interface{}{
				map[string]interface{}{"metadata": map[string]interface{}{"name": "vm1-rootdisk"}},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"kubevirt.io/domain": "vm1", "app": "vm1", "tier": "db"},
				},
				"spec": map[string]interface{}{
					"hostname": "vm1",
					"domain": map[string]interface{}{
						"firmware": map[string]interface{}{"uuid": "original-uuid"},
						"devices": map[string]interface{}{
							"interfaces": []interface{}{map[string]interface{}{"name": "default", "macAddress": "02:00:00:00:00:01"}},
						},
					},
					"accessCredentials": []interface{}{map[string]interface{}{
						"sshPublicKey": map[string]interface{}{
							"source":            map[string]interface{}{"secret": map[string]interface{}{"secretName": "ssh-key"}},
							"propagationMethod": map[string]interface{}{"noCloud": map[string]interface{}{}},
						},
					}},
					"volumes": []interface{}{
						map[string]interface{}{"name": "rootdisk", "dataVolume": map[string]interface{}{"name": "vm1-rootdisk"}},
						map[string]interface{}{"name": "data", "persistentVolumeClaim": map[string]interface{}{"claimName": "data-disk"}},
						map[string]interface{}{"name": "cloudinit", "cloudInitNoCloud": map[string]interface{}{
							"secretRef":            map[string]interface{}{"name": "vm1-cloudinit"},
							"networkDataSecretRef": map[string]interface{}{"name": "vm1-cloudinit"},
						}},
						map[string]interface{}{"name": "certs", "secret": map[string]interface{}{"secretName": "shared-certs"}},
						map[string]interface{}{"name": "config", "configMap": map[string]interface{}{"name": "app-config"}},
						map[string]interface{}{"name": "sa", "serviceAccount": map[string]interface{}{"serviceAccountName": "vm-sa"}},
					},
				},
			},
		},
	}}
}

func restoreWithVMOptions(t *testing.T, vms ...util.VMOptions) *velerov1.Restore {
	t.Helper()
	value, err := json.Marshal(vms)
	require.NoError(t, err)
	return &velerov1.Restore{ObjectMeta: metav1.ObjectMeta{
		Name:        "test-restore",
		Namespace:   "velero",
		Annotations: map[string]string{util.VMOptionsAnnotation: string(value)},
	}}
}

func vm1Clone(items ...util.RenamedItem) util.VMOptions {
	return util.VMOptions{Namespace: "ns1", Name: "vm1", NewName: "vm1-clone", ClearMacAddresses: true, NewFirmwareUUID: true, Items: items}
}

var vm1CloneItems = []util.RenamedItem{
	{Resource: "datavolumes.cdi.kubevirt.io", Name: "vm1-rootdisk", NewName: "vm1-clone-rootdisk"},
	{Resource: "persistentvolumeclaims", Name: "vm1-rootdisk", NewName: "vm1-clone-rootdisk"},
	{Resource: "persistentvolumeclaims", Name: "data-disk", NewName: "vm1-clone-data-disk"},
	{Resource: "secrets", Name: "vm1-cloudinit", NewName: "vm1-clone-cloudinit"},
	{Resource: "secrets", Name: "ssh-key", NewName: "vm1-clone-ssh-key"},
	{Resource: "secrets", Name: "shared-certs", NewName: "vm1-clone-shared-certs", KeepOriginal: true},
	{Resource: "configmaps", Name: "app-config", NewName: "vm1-clone-app-config"},
	{Resource: "controllerrevisions.apps", Name: "vm1-u1.medium-1", NewName: "vm1-clone-u1.medium-1"},
}

// executeRenamed runs the VM restore action the way the engine does for a
// renamed VM: the item carries the new name, the item from the backup the old.
func executeRenamed(t *testing.T, restore *velerov1.Restore, backedUp *unstructured.Unstructured) (*velero.RestoreItemActionExecuteOutput, string, error) {
	t.Helper()
	item := backedUp.DeepCopy()
	item.SetName("vm1-clone")
	var logs bytes.Buffer
	logger := logrus.New()
	logger.Out = &logs
	logger.Formatter = &logrus.JSONFormatter{}

	output, err := NewVMRestoreItemAction(logger).Execute(&velero.RestoreItemActionExecuteInput{
		Item:           item,
		ItemFromBackup: backedUp,
		Restore:        restore,
	})
	return output, logs.String(), err
}

func TestARenamedVMUsesTheCopiesOfWhatItUses(t *testing.T) {
	output, logs, err := executeRenamed(t, restoreWithVMOptions(t, vm1Clone(vm1CloneItems...)), backedUpVMToRename())
	require.NoError(t, err)

	vm := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), vm))
	assert.Equal(t, "vm1-clone", vm.Name)
	assert.Equal(t, "vm1-clone-rootdisk", vm.Spec.DataVolumeTemplates[0].Name)
	assert.Equal(t, "vm1-clone-u1.medium-1", vm.Spec.Instancetype.RevisionName)

	spec := vm.Spec.Template.Spec
	volumes := map[string]kvcore.Volume{}
	for _, volume := range spec.Volumes {
		volumes[volume.Name] = volume
	}
	assert.Equal(t, "vm1-clone-rootdisk", volumes["rootdisk"].DataVolume.Name)
	assert.Equal(t, "vm1-clone-data-disk", volumes["data"].PersistentVolumeClaim.ClaimName)
	assert.Equal(t, "vm1-clone-cloudinit", volumes["cloudinit"].CloudInitNoCloud.UserDataSecretRef.Name)
	assert.Equal(t, "vm1-clone-cloudinit", volumes["cloudinit"].CloudInitNoCloud.NetworkDataSecretRef.Name)
	assert.Equal(t, "vm1-clone-shared-certs", volumes["certs"].Secret.SecretName)
	assert.Equal(t, "vm1-clone-app-config", volumes["config"].ConfigMap.Name)
	assert.Equal(t, "vm-sa", volumes["sa"].ServiceAccount.ServiceAccountName, "service accounts are shared")
	assert.Equal(t, "vm1-clone-ssh-key", spec.AccessCredentials[0].SSHPublicKey.Source.Secret.SecretName)

	assert.Equal(t, "vm1-clone", spec.Hostname)
	assert.Equal(t, map[string]string{"kubevirt.io/domain": "vm1-clone", "app": "vm1-clone", "tier": "db"}, vm.Spec.Template.ObjectMeta.Labels,
		"a Service that selects the original does not select the copy")
	assert.Equal(t, map[string]string{"app": "vm1-clone", "tier": "db"}, vm.Labels)
	assert.Empty(t, spec.Domain.Devices.Interfaces[0].MacAddress)
	assert.NotEqual(t, "original-uuid", string(spec.Domain.Firmware.UUID))

	var additional []string
	for _, item := range output.AdditionalItems {
		additional = append(additional, item.GroupResource.String()+" "+item.Namespace+"/"+item.Name)
	}
	assert.ElementsMatch(t, []string{
		"controllerrevisions.apps ns1/vm1-u1.medium-1",
		"datavolumes.cdi.kubevirt.io ns1/vm1-rootdisk",
		"persistentvolumeclaims ns1/vm1-rootdisk",
		"persistentvolumeclaims ns1/data-disk",
		"secrets ns1/vm1-cloudinit",
		"secrets ns1/shared-certs",
		"secrets ns1/ssh-key",
		"configmaps ns1/app-config",
		"serviceaccounts ns1/vm-sa",
	}, additional, "additional items are named as in the backup; the engine restores their copies")

	assert.Contains(t, logs, `"level":"warning"`)
	assert.Contains(t, logs, "with a copy of its cloud-init configuration")
	assert.Contains(t, logs, "vm1-clone-cloudinit")
}

// A DataVolume that CDI garbage collected after its import is not in the
// backup; its PVC, which has the same name, gives the template its new name.
func TestARenamedVMTakesItsDataVolumeNameFromThePVC(t *testing.T) {
	var items []util.RenamedItem
	for _, item := range vm1CloneItems {
		if item.Resource != "datavolumes.cdi.kubevirt.io" {
			items = append(items, item)
		}
	}
	output, _, err := executeRenamed(t, restoreWithVMOptions(t, vm1Clone(items...)), backedUpVMToRename())
	require.NoError(t, err)

	vm := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), vm))
	assert.Equal(t, "vm1-clone-rootdisk", vm.Spec.DataVolumeTemplates[0].Name)
	assert.Equal(t, "vm1-clone-rootdisk", vm.Spec.Template.Spec.Volumes[0].DataVolume.Name)
}

// A renamed VM never shares a disk, secret, config map or revision with the
// original: deleting the original would take them away. One the restore does
// not copy fails the VM.
func TestARenamedVMIsNotRestoredWithoutACopyOfWhatItUses(t *testing.T) {
	for _, missing := range []string{"vm1-rootdisk", "data-disk", "vm1-cloudinit", "ssh-key", "shared-certs", "app-config", "vm1-u1.medium-1"} {
		t.Run(missing, func(t *testing.T) {
			var items []util.RenamedItem
			for _, item := range vm1CloneItems {
				if item.Name != missing {
					items = append(items, item)
				}
			}

			_, _, err := executeRenamed(t, restoreWithVMOptions(t, vm1Clone(items...)), backedUpVMToRename())

			require.Error(t, err)
			assert.Contains(t, err.Error(), missing)
			assert.Contains(t, err.Error(), "vm1-clone")
		})
	}
}

// A VM the restore does not rename keeps its names, MAC and firmware UUID.
func TestAVMThatIsNotRenamedKeepsItsNames(t *testing.T) {
	backedUp := backedUpVMToRename()
	restore := restoreWithVMOptions(t, util.VMOptions{Namespace: "ns1", Name: "other", NewName: "other-clone"})

	output, err := NewVMRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
		Item:           backedUp.DeepCopy(),
		ItemFromBackup: backedUp,
		Restore:        restore,
	})
	require.NoError(t, err)

	vm := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), vm))
	assert.Equal(t, "vm1-rootdisk", vm.Spec.DataVolumeTemplates[0].Name)
	assert.Equal(t, "vm1", vm.Spec.Template.Spec.Hostname)
	assert.Equal(t, "02:00:00:00:00:01", vm.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress)
	assert.Equal(t, "original-uuid", string(vm.Spec.Template.Spec.Domain.Firmware.UUID))
}

// executeKept runs the VM restore action for a VM restored under its own name.
func executeKept(t *testing.T, restore *velerov1.Restore) *kvcore.VirtualMachine {
	t.Helper()
	backedUp := backedUpVMToRename()
	output, err := NewVMRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
		Item:           backedUp.DeepCopy(),
		ItemFromBackup: backedUp,
		Restore:        restore,
	})
	require.NoError(t, err)
	vm := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), vm))
	return vm
}

func withRestoreWideOptions(restore *velerov1.Restore) *velerov1.Restore {
	restore.Labels = map[string]string{util.ClearMacAddressLabel: "true", util.GenerateNewFirmwareUUIDLabel: "true"}
	return restore
}

// A VM with options but no new name keeps its names, and gets its own options.
func TestAVMWithOptionsButNoNewNameKeepsItsNames(t *testing.T) {
	vm := executeKept(t, restoreWithVMOptions(t, util.VMOptions{Namespace: "ns1", Name: "vm1", ClearMacAddresses: true}))

	assert.Equal(t, "vm1", vm.Name)
	assert.Equal(t, "vm1-rootdisk", vm.Spec.DataVolumeTemplates[0].Name)
	assert.Equal(t, "vm1", vm.Spec.Template.Spec.Hostname)
	assert.Empty(t, vm.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress, "its own option clears the MAC")
	assert.Equal(t, "original-uuid", string(vm.Spec.Template.Spec.Domain.Firmware.UUID), "its own option keeps the UUID")
}

// A VM's own options take the place of the restore-wide ones, also when they
// say to keep the MAC addresses and the firmware UUID.
func TestAVMsOwnOptionsTakeThePlaceOfTheRestoreWideOnes(t *testing.T) {
	kept := executeKept(t, withRestoreWideOptions(restoreWithVMOptions(t, util.VMOptions{Namespace: "ns1", Name: "vm1"})))
	assert.Equal(t, "02:00:00:00:00:01", kept.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress)
	assert.Equal(t, "original-uuid", string(kept.Spec.Template.Spec.Domain.Firmware.UUID))

	clone := vm1Clone(vm1CloneItems...)
	clone.ClearMacAddresses, clone.NewFirmwareUUID = false, false
	output, _, err := executeRenamed(t, withRestoreWideOptions(restoreWithVMOptions(t, clone)), backedUpVMToRename())
	require.NoError(t, err)
	renamed := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), renamed))
	assert.Equal(t, "vm1-clone", renamed.Name)
	assert.Equal(t, "02:00:00:00:00:01", renamed.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress)
	assert.Equal(t, "original-uuid", string(renamed.Spec.Template.Spec.Domain.Firmware.UUID))
}

// A VM without options of its own follows the restore-wide options.
func TestAVMWithoutOptionsFollowsTheRestoreWideOnes(t *testing.T) {
	vm := executeKept(t, withRestoreWideOptions(restoreWithVMOptions(t, util.VMOptions{Namespace: "ns1", Name: "other"})))

	assert.Empty(t, vm.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress)
	assert.NotEqual(t, "original-uuid", string(vm.Spec.Template.Spec.Domain.Firmware.UUID))
}

func renamedItem(kind, oldName, newName string, annotations, labels map[string]interface{}) (*unstructured.Unstructured, *unstructured.Unstructured) {
	metadata := map[string]interface{}{"name": oldName, "namespace": "ns1"}
	if annotations != nil {
		metadata["annotations"] = annotations
	}
	if labels != nil {
		metadata["labels"] = labels
	}
	apiVersion := "v1"
	if kind == "DataVolume" {
		apiVersion = "cdi.kubevirt.io/v1beta1"
	}
	backedUp := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": apiVersion, "kind": kind, "metadata": metadata,
	}}
	item := backedUp.DeepCopy()
	item.SetName(newName)
	return item, backedUp
}

// The copy of a renamed VM's disk is populated for the copy of its DataVolume,
// so CDI hands it to that DataVolume and not to the original's.
func TestTheCopyOfADiskIsPopulatedForTheCopyOfItsDataVolume(t *testing.T) {
	restore := restoreWithVMOptions(t, vm1Clone(vm1CloneItems...))
	for name, annotations := range map[string]map[string]interface{}{
		"annotated in the backup": {AnnPopulatedFor: "vm1-rootdisk"},
		"owned by the DataVolume": nil,
	} {
		t.Run(name, func(t *testing.T) {
			item, backedUp := renamedItem("PersistentVolumeClaim", "vm1-rootdisk", "vm1-clone-rootdisk", annotations, nil)
			backedUp.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "cdi.kubevirt.io/v1beta1", Kind: "DataVolume", Name: "vm1-rootdisk", UID: "uid-1"}})

			output, err := NewPVCRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
				Item: item, ItemFromBackup: backedUp, Restore: restore,
			})
			require.NoError(t, err)

			restored := output.UpdatedItem.(*unstructured.Unstructured)
			assert.Equal(t, "vm1-clone-rootdisk", restored.GetName())
			assert.Equal(t, "vm1-clone-rootdisk", restored.GetAnnotations()[AnnPopulatedFor])
		})
	}
}

// The copy of a renamed VM's persistent state (TPM, EFI) is found by KubeVirt
// through its label, which names the VM.
func TestTheCopyOfAPersistentStatePVCBelongsToTheRenamedVM(t *testing.T) {
	restore := restoreWithVMOptions(t, vm1Clone(util.RenamedItem{
		Resource: "persistentvolumeclaims", Name: "persistent-state-for-vm1-abcde", NewName: "persistent-state-for-vm1-clone-abcde",
	}))
	item, backedUp := renamedItem("PersistentVolumeClaim", "persistent-state-for-vm1-abcde", "persistent-state-for-vm1-clone-abcde",
		nil, map[string]interface{}{"persistent-state-for": "vm1"})

	output, err := NewPVCRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
		Item: item, ItemFromBackup: backedUp, Restore: restore,
	})
	require.NoError(t, err)

	assert.Equal(t, "vm1-clone", output.UpdatedItem.(*unstructured.Unstructured).GetLabels()["persistent-state-for"])
}

// The copy of a renamed VM's DataVolume is marked as populated under its own name.
func TestTheCopyOfADataVolumeIsPrePopulatedUnderItsNewName(t *testing.T) {
	restore := restoreWithVMOptions(t, vm1Clone(vm1CloneItems...))
	for name, annotations := range map[string]map[string]interface{}{
		"annotated in the backup": {AnnPrePopulated: "vm1-rootdisk"},
		"not annotated":           nil,
	} {
		t.Run(name, func(t *testing.T) {
			item, backedUp := renamedItem("DataVolume", "vm1-rootdisk", "vm1-clone-rootdisk", annotations, nil)

			output, err := NewDVRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
				Item: item, ItemFromBackup: backedUp, Restore: restore,
			})
			require.NoError(t, err)

			assert.Equal(t, "vm1-clone-rootdisk", output.UpdatedItem.(*unstructured.Unstructured).GetAnnotations()[AnnPrePopulated])
		})
	}
}
