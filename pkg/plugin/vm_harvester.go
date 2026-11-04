package plugin

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kvcore "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
)

// Harvester keeps a VM's disks and MAC addresses in annotations of the VM too
// (Harvester pkg/util/constants.go).
const (
	// harvesterVolumeClaimTemplates lists the PVCs of the VM's disks as JSON.
	// Harvester creates a listed PVC that is missing and grows a smaller one.
	harvesterVolumeClaimTemplates = "harvesterhci.io/volumeClaimTemplates"
	// harvesterMacAddresses maps the VM's interfaces to MAC addresses. Harvester
	// gives an interface without a MAC in the VM spec the MAC listed here.
	harvesterMacAddresses = "harvesterhci.io/mac-address"
)

// renameHarvesterDisks names the copies of a renamed VM's disks in Harvester's
// list of them. A listed disk without a copy is not the renamed VM's, so it
// leaves the list: Harvester would create it, or count the original's as the
// renamed VM's.
func renameHarvesterDisks(vm *kvcore.VirtualMachine, rename *util.VMOptions) error {
	value := vm.Annotations[harvesterVolumeClaimTemplates]
	if value == "" {
		return nil
	}
	var disks []map[string]interface{}
	if err := json.Unmarshal([]byte(value), &disks); err != nil {
		return fmt.Errorf("VirtualMachine %s/%s has an invalid annotation %s: %w",
			rename.Namespace, rename.Name, harvesterVolumeClaimTemplates, err)
	}

	var copies []map[string]interface{}
	for _, disk := range disks {
		name, _, _ := unstructured.NestedString(disk, "metadata", "name")
		// The renamed VM is not in the original's storage migration. One that
		// already uses the migration's target PVC lists the target's copy.
		target, _ := disk["targetVolume"].(string)
		delete(disk, "targetVolume")
		newName := rename.NewNameOf(pvcResource, name)
		if newName == "" && target != "" {
			newName = rename.NewNameOf(pvcResource, target)
		}
		if newName == "" {
			continue
		}
		if err := unstructured.SetNestedField(disk, newName, "metadata", "name"); err != nil {
			return err
		}
		copies = append(copies, disk)
	}
	if len(copies) == 0 {
		delete(vm.Annotations, harvesterVolumeClaimTemplates)
		return nil
	}
	data, err := json.Marshal(copies)
	if err != nil {
		return err
	}
	vm.Annotations[harvesterVolumeClaimTemplates] = string(data)
	return nil
}
