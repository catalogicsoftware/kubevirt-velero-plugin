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
 * Copyright 2022 Red Hat, Inc.
 *
 */

package plugin

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"

	corev1api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// PVCRestoreItemAction is a backup item action for restoring DataVolumes
type PVCRestoreItemAction struct {
	log logrus.FieldLogger
}

// NewPVCRestoreItemAction instantiates a PVCRestoreItemAction.
func NewPVCRestoreItemAction(log logrus.FieldLogger) *PVCRestoreItemAction {
	return &PVCRestoreItemAction{log: log}
}

// AppliesTo returns information about which resources this action should be invoked for.
func (p *PVCRestoreItemAction) AppliesTo() (velero.ResourceSelector, error) {
	return velero.ResourceSelector{
			IncludedResources: []string{"PersistentVolumeClaim"},
		},
		nil
}

func (p *PVCRestoreItemAction) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.log.Info("Executing PVCRestoreItemAction")
	if input == nil {
		return nil, fmt.Errorf("input object nil!")
	}

	var pvc corev1api.PersistentVolumeClaim
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(input.Item.UnstructuredContent(), &pvc); err != nil {
		return nil, errors.WithStack(err)
	}
	p.log.Infof("handling PVC %v/%v", pvc.GetNamespace(), pvc.GetName())

	annotations := pvc.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	_, inProgress := annotations[AnnInProgress]
	if inProgress {
		return velero.NewRestoreItemActionExecuteOutput(input.Item).WithoutRestore(), nil
	}

	// If the PVC is owned by a DataVolume and the populatedFor annotation is
	// missing, add it. This is needed when the backup bundle was not produced
	// by a standard Velero backup (e.g., custom inventory for DR), so the
	// DVBackupItemAction never ran and the annotation is absent from the
	// manifest. Without this annotation, CDI's admission webhook will reject
	// the DataVolume restore because the destination PVC already exists.
	if _, hasPopulatedFor := annotations[AnnPopulatedFor]; !hasPopulatedFor {
		dvName := getOwnerDataVolumeName(&pvc)
		if dvName != "" {
			p.log.Infof("PVC %v/%v is owned by DataVolume %v but missing %v annotation, adding it",
				pvc.GetNamespace(), pvc.GetName(), dvName, AnnPopulatedFor)
			annotations[AnnPopulatedFor] = dvName
			pvc.SetAnnotations(annotations)

			pvcMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pvc)
			if err != nil {
				return nil, errors.WithStack(err)
			}
			return velero.NewRestoreItemActionExecuteOutput(&unstructured.Unstructured{Object: pvcMap}), nil
		}
	}

	return velero.NewRestoreItemActionExecuteOutput(input.Item), nil
}

// getOwnerDataVolumeName returns the name of the DataVolume that owns this PVC,
// using multiple detection methods:
//  1. ownerReferences with Kind=DataVolume
//  2. CDI label cdi.kubevirt.io/storage.dataVolumeName
func getOwnerDataVolumeName(pvc *corev1api.PersistentVolumeClaim) string {
	for _, ownerRef := range pvc.GetOwnerReferences() {
		if ownerRef.Kind == "DataVolume" {
			return ownerRef.Name
		}
	}

	if labels := pvc.GetLabels(); labels != nil {
		if dvName, ok := labels["cdi.kubevirt.io/storage.dataVolumeName"]; ok && dvName != "" {
			return dvName
		}
	}

	return ""
}
