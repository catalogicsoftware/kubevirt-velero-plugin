package plugin

import (
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kvcore "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
)

const harvesterMacs = `{"default":"02:00:00:00:00:01"}`

// harvesterDisks lists vm1's disks the way Harvester does, with a disk on its
// way to another PVC and a disk vm1 does not use.
const harvesterDisks = `[` +
	`{"metadata":{"name":"vm1-rootdisk","creationTimestamp":null,"annotations":{"harvesterhci.io/imageId":"default/image-1"}},` +
	`"spec":{"accessModes":["ReadWriteMany"],"resources":{"requests":{"storage":"20Gi"}},"storageClassName":"longhorn-image-1","volumeMode":"Block"},"status":{}},` +
	`{"metadata":{"name":"data-disk","creationTimestamp":null},"targetVolume":"data-disk-2",` +
	`"spec":{"resources":{"requests":{"storage":"5Gi"}},"storageClassName":"longhorn"},"status":{}},` +
	`{"metadata":{"name":"unused-disk","creationTimestamp":null},"spec":{"resources":{"requests":{"storage":"1Gi"}}},"status":{}}]`

// backedUpHarvesterVM is vm1 as Harvester keeps it, with its disks and MAC
// addresses in annotations.
func backedUpHarvesterVM(t *testing.T, disks string) *unstructured.Unstructured {
	t.Helper()
	vm := backedUpVMToRename()
	require.NoError(t, unstructured.SetNestedStringMap(vm.Object, map[string]string{
		harvesterMacAddresses:         harvesterMacs,
		harvesterVolumeClaimTemplates: disks,
	}, "metadata", "annotations"))
	return vm
}

func restoreHarvesterVM(t *testing.T, restore *velerov1.Restore, backedUp *unstructured.Unstructured) *kvcore.VirtualMachine {
	t.Helper()
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

func renameHarvesterVM(t *testing.T, disks string, items ...util.RenamedItem) (*kvcore.VirtualMachine, error) {
	t.Helper()
	output, _, err := executeRenamed(t, restoreWithVMOptions(t, vm1Clone(items...)), backedUpHarvesterVM(t, disks))
	if err != nil {
		return nil, err
	}
	vm := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), vm))
	return vm, nil
}

func disksOf(t *testing.T, vm *kvcore.VirtualMachine) []map[string]interface{} {
	t.Helper()
	var disks []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(vm.Annotations[harvesterVolumeClaimTemplates]), &disks))
	return disks
}

// Harvester creates a listed disk that is missing and grows a smaller one, so
// a renamed VM lists its copies and never the original's disks.
func TestARenamedHarvesterVMListsTheCopiesOfItsDisks(t *testing.T) {
	vm, err := renameHarvesterVM(t, harvesterDisks, vm1CloneItems...)
	require.NoError(t, err)

	disks := disksOf(t, vm)
	require.Len(t, disks, 2, "unused-disk has no copy, so it is not the renamed VM's")
	assert.Equal(t, map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "vm1-clone-rootdisk", "creationTimestamp": nil,
			"annotations": map[string]interface{}{"harvesterhci.io/imageId": "default/image-1"},
		},
		"spec": map[string]interface{}{
			"accessModes": []interface{}{"ReadWriteMany"}, "resources": map[string]interface{}{"requests": map[string]interface{}{"storage": "20Gi"}},
			"storageClassName": "longhorn-image-1", "volumeMode": "Block",
		},
		"status": map[string]interface{}{},
	}, disks[0], "only the name changes")
	assert.Equal(t, "vm1-clone-data-disk", disks[1]["metadata"].(map[string]interface{})["name"])
	assert.NotContains(t, disks[1], "targetVolume", "the original's migration target is not the renamed VM's")
}

// Midway through a storage migration Harvester has moved the VM to the target
// PVC but still lists the disk by its source PVC.
func TestARenamedHarvesterVMMidMigrationListsTheCopyOfTheTarget(t *testing.T) {
	backedUp := backedUpHarvesterVM(t, harvesterDisks)
	volumes, _, _ := unstructured.NestedSlice(backedUp.Object, "spec", "template", "spec", "volumes")
	require.NoError(t, unstructured.SetNestedField(volumes[1].(map[string]interface{}), "data-disk-2", "persistentVolumeClaim", "claimName"))
	require.NoError(t, unstructured.SetNestedSlice(backedUp.Object, volumes, "spec", "template", "spec", "volumes"))
	items := []util.RenamedItem{{Resource: "persistentvolumeclaims", Name: "data-disk-2", NewName: "vm1-clone-data-disk-2"}}
	for _, item := range vm1CloneItems {
		if item.Name != "data-disk" {
			items = append(items, item)
		}
	}

	output, _, err := executeRenamed(t, restoreWithVMOptions(t, vm1Clone(items...)), backedUp)
	require.NoError(t, err)
	vm := new(kvcore.VirtualMachine)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(output.UpdatedItem.UnstructuredContent(), vm))

	assert.Equal(t, "vm1-clone-data-disk-2", vm.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim.ClaimName)
	disks := disksOf(t, vm)
	require.Len(t, disks, 2)
	assert.Equal(t, "vm1-clone-data-disk-2", disks[1]["metadata"].(map[string]interface{})["name"])
	assert.NotContains(t, disks[1], "targetVolume")
}

func TestARenamedHarvesterVMWithoutACopyOfAnyListedDiskHasNoList(t *testing.T) {
	vm, err := renameHarvesterVM(t, `[{"metadata":{"name":"unused-disk"},"spec":{}}]`, vm1CloneItems...)
	require.NoError(t, err)

	assert.NotContains(t, vm.Annotations, harvesterVolumeClaimTemplates)
}

func TestARenamedHarvesterVMWithAnInvalidListOfDisksIsNotRestored(t *testing.T) {
	_, err := renameHarvesterVM(t, `{"metadata":`, vm1CloneItems...)

	require.Error(t, err)
	assert.Contains(t, err.Error(), harvesterVolumeClaimTemplates)
}

func TestAHarvesterVMThatIsNotRenamedKeepsItsListOfDisks(t *testing.T) {
	vm := restoreHarvesterVM(t, restoreWithVMOptions(t), backedUpHarvesterVM(t, harvesterDisks))

	assert.Equal(t, harvesterDisks, vm.Annotations[harvesterVolumeClaimTemplates])
}

// Harvester gives an interface without a MAC in the spec the MAC in its
// annotation, so clearing the MAC addresses removes the annotation too.
func TestClearingMacAddressesClearsHarvestersMacAddresses(t *testing.T) {
	for name, test := range map[string]struct {
		restore *velerov1.Restore
		cleared bool
	}{
		"kept":                     {restore: restoreWithVMOptions(t), cleared: false},
		"restore-wide option":      {restore: withRestoreWideOptions(restoreWithVMOptions(t)), cleared: true},
		"the VM's own option":      {restore: restoreWithVMOptions(t, util.VMOptions{Namespace: "ns1", Name: "vm1", ClearMacAddresses: true}), cleared: true},
		"the VM's own option, off": {restore: withRestoreWideOptions(restoreWithVMOptions(t, util.VMOptions{Namespace: "ns1", Name: "vm1"})), cleared: false},
	} {
		t.Run(name, func(t *testing.T) {
			vm := restoreHarvesterVM(t, test.restore, backedUpHarvesterVM(t, harvesterDisks))

			if test.cleared {
				assert.Empty(t, vm.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress)
				assert.NotContains(t, vm.Annotations, harvesterMacAddresses)
			} else {
				assert.Equal(t, "02:00:00:00:00:01", vm.Spec.Template.Spec.Domain.Devices.Interfaces[0].MacAddress)
				assert.Equal(t, harvesterMacs, vm.Annotations[harvesterMacAddresses])
			}
			assert.Contains(t, vm.Annotations, harvesterVolumeClaimTemplates, "only the MAC addresses are cleared")
		})
	}

	renamed, err := renameHarvesterVM(t, harvesterDisks, vm1CloneItems...)
	require.NoError(t, err)
	assert.NotContains(t, renamed.Annotations, harvesterMacAddresses, "vm1-clone clears its MAC addresses")
}
