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
 * Copyright 2021 Red Hat, Inc.
 *
 */

package plugin

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// backedUpOwnerReferences returns the owner references of an item being restored.
//
// Velero resets the metadata of the item before restore item actions run, and
// that removes the owner references along with the uid, the resourceVersion and
// the other non core fields, see resetMetadata in
// vmware-tanzu/velero v1.14.0 pkg/restore/restore.go:2058.
//
// The pristine copy Velero takes from the backup before the reset still carries
// them, so it is the reliable source. The item being restored is only used as a
// fallback, for callers that do not provide the backed up one.
func backedUpOwnerReferences(itemFromBackup runtime.Unstructured, restored metav1.Object) []metav1.OwnerReference {
	if itemFromBackup != nil {
		if metadata, err := meta.Accessor(itemFromBackup); err == nil {
			if ownerReferences := metadata.GetOwnerReferences(); len(ownerReferences) > 0 {
				return ownerReferences
			}
		}
	}

	if restored == nil {
		return nil
	}

	return restored.GetOwnerReferences()
}
