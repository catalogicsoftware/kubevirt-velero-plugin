/*
 * This file is part of the Kubevirt Velero Plugin project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package util

import (
	"encoding/json"
	"fmt"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

// VMOptionsAnnotation on a Restore lists the per-VM options of the restore.
// The engine restores each VM with a NewName, and the objects it uses, under
// the new names given here; the restore item actions point the VM and its disks
// at the copies. The engine reads the same annotation (CloudCasa's Velero,
// pkg/util/kubevirt).
const VMOptionsAnnotation = "cloudcasa.io/vm-options"

// VMOptions are the restore options of a VM of the backup, in place of the
// restore-wide ones. A VM with a NewName is restored as NewName.
type VMOptions struct {
	Namespace         string        `json:"namespace"`
	Name              string        `json:"name"`
	NewName           string        `json:"newName,omitempty"`
	ClearMacAddresses bool          `json:"clearMacAddresses"`
	NewFirmwareUUID   bool          `json:"newFirmwareUUID"`
	Items             []RenamedItem `json:"items,omitempty"`
}

// RenamedItem is an object in the VM's namespace that the VM uses, restored as
// NewName for this VM. Resource is a group resource, e.g. "secrets" or
// "datavolumes.cdi.kubevirt.io".
type RenamedItem struct {
	Resource     string `json:"resource"`
	Name         string `json:"name"`
	NewName      string `json:"newName"`
	KeepOriginal bool   `json:"keepOriginal,omitempty"`
}

// VMOptionsOf returns the options a restore has for a VM of the backup, or nil
// when it has none for it.
func VMOptionsOf(restore *velerov1.Restore, namespace, name string) (*VMOptions, error) {
	if restore == nil {
		return nil, nil
	}
	value, ok := restore.Annotations[VMOptionsAnnotation]
	if !ok {
		return nil, nil
	}
	var vms []VMOptions
	if err := json.Unmarshal([]byte(value), &vms); err != nil {
		return nil, fmt.Errorf("invalid annotation %s: %w", VMOptionsAnnotation, err)
	}
	for i := range vms {
		if vms[i].Namespace == namespace && vms[i].Name == name {
			return &vms[i], nil
		}
	}
	return nil, nil
}

// NewNameOf returns the name of this VM's copy of an item it uses, or "" when
// the restore does not copy that item for the VM.
func (r *VMOptions) NewNameOf(resource, name string) string {
	for _, item := range r.Items {
		if item.Resource == resource && item.Name == name {
			return item.NewName
		}
	}
	return ""
}
