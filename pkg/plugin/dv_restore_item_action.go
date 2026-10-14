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
	"fmt"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
)

// DVRestoreItemAction is a restore item action plugin for DataVolumes.
//
// It ensures the "cdi.kubevirt.io/storage.prePopulated" annotation is present
// on DataVolumes that are in Succeeded phase. This annotation tells CDI's
// admission webhook to accept the DataVolume creation even when the destination
// PVC already exists.
//
// This is necessary because the backup bundle may not have been produced by a
// standard Velero backup — for example, when using a custom inventory process
// for disaster recovery. In that case, the DVBackupItemAction (which normally
// adds this annotation during backup) never executed, and the annotation is
// missing from the manifest.
//
// If the annotation is already present (e.g., from a standard Velero backup),
// this action is a no-op.
type DVRestoreItemAction struct {
	log logrus.FieldLogger
}

// NewDVRestoreItemAction instantiates a DVRestoreItemAction.
func NewDVRestoreItemAction(log logrus.FieldLogger) *DVRestoreItemAction {
	return &DVRestoreItemAction{log: log}
}

// AppliesTo returns information about which resources this action should be invoked for.
func (p *DVRestoreItemAction) AppliesTo() (velero.ResourceSelector, error) {
	return velero.ResourceSelector{
		IncludedResources: []string{"DataVolume"},
	}, nil
}

// Execute ensures that a Succeeded DataVolume has the prePopulated annotation
// before Velero creates it on the destination cluster.
func (p *DVRestoreItemAction) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.log.Info("Executing DVRestoreItemAction")

	if input == nil {
		return nil, fmt.Errorf("input object nil!")
	}

	var dv cdiv1.DataVolume
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(input.Item.UnstructuredContent(), &dv); err != nil {
		return nil, errors.WithStack(err)
	}

	p.log.Infof("handling DataVolume %v/%v (phase: %v)", dv.GetNamespace(), dv.GetName(), dv.Status.Phase)

	// Skip annotation injection only for DVs that are explicitly in a
	// non-Succeeded phase (e.g., ImportInProgress, Failed), where CDI
	// may need to re-run the import.
	// An empty phase means the status was not captured in the backup bundle
	// (common with custom inventory processes that don't preserve status).
	// In the DR flow, storage is already provisioned by the DR adapter,
	// so it is safe to treat an empty phase the same as Succeeded.
	if dv.Status.Phase != cdiv1.Succeeded && dv.Status.Phase != "" {
		p.log.Infof("DataVolume %v/%v is in phase %v (not Succeeded), skipping annotation injection",
			dv.GetNamespace(), dv.GetName(), dv.Status.Phase)
		return velero.NewRestoreItemActionExecuteOutput(input.Item), nil
	}

	if dv.Status.Phase == "" {
		p.log.Infof("DataVolume %v/%v has empty phase (status not captured in backup), treating as eligible for prePopulated annotation",
			dv.GetNamespace(), dv.GetName())
	}

	annotations := dv.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// If the annotation is already present (from a standard Velero backup where
	// DVBackupItemAction ran), do nothing.
	if _, exists := annotations[AnnPrePopulated]; exists {
		p.log.Infof("DataVolume %v/%v already has %v annotation, no action needed", dv.GetNamespace(), dv.GetName(), AnnPrePopulated)
		return velero.NewRestoreItemActionExecuteOutput(input.Item), nil
	}

	// Add the prePopulated annotation. This tells CDI's admission webhook that
	// the PVC data was already populated and the DataVolume should be accepted
	// even if the destination PVC exists.
	p.log.Infof("DataVolume %v/%v is Succeeded but missing %v annotation, adding it", dv.GetNamespace(), dv.GetName(), AnnPrePopulated)
	annotations[AnnPrePopulated] = dv.GetName()
	dv.SetAnnotations(annotations)

	dvMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dv)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return velero.NewRestoreItemActionExecuteOutput(&unstructured.Unstructured{Object: dvMap}), nil
}
