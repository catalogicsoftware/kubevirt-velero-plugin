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
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
)

func TestDVRestoreItemAction_Execute_NilInput(t *testing.T) {
	action := NewDVRestoreItemAction(logrus.StandardLogger())
	_, err := action.Execute(nil)
	assert.Error(t, err)
}

func TestDVRestoreItemAction_Execute_SucceededDV_NoAnnotation(t *testing.T) {
	// A Succeeded DV without the prePopulated annotation should get it added.
	dv := &cdiv1.DataVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dv",
			Namespace: "test-ns",
		},
		Status: cdiv1.DataVolumeStatus{
			Phase: cdiv1.Succeeded,
		},
	}

	dvMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dv)
	require.NoError(t, err)

	input := &velero.RestoreItemActionExecuteInput{
		Item: &unstructured.Unstructured{Object: dvMap},
	}

	action := NewDVRestoreItemAction(logrus.StandardLogger())
	output, err := action.Execute(input)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify the annotation was added.
	annotations, found, err := unstructured.NestedStringMap(output.UpdatedItem.UnstructuredContent(), "metadata", "annotations")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "test-dv", annotations[AnnPrePopulated])
}

func TestDVRestoreItemAction_Execute_SucceededDV_AlreadyAnnotated(t *testing.T) {
	// A Succeeded DV that already has the prePopulated annotation should not be modified.
	dv := &cdiv1.DataVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dv",
			Namespace: "test-ns",
			Annotations: map[string]string{
				AnnPrePopulated: "test-dv",
			},
		},
		Status: cdiv1.DataVolumeStatus{
			Phase: cdiv1.Succeeded,
		},
	}

	dvMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dv)
	require.NoError(t, err)

	input := &velero.RestoreItemActionExecuteInput{
		Item: &unstructured.Unstructured{Object: dvMap},
	}

	action := NewDVRestoreItemAction(logrus.StandardLogger())
	output, err := action.Execute(input)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify the annotation is still present and unchanged.
	annotations, found, err := unstructured.NestedStringMap(output.UpdatedItem.UnstructuredContent(), "metadata", "annotations")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "test-dv", annotations[AnnPrePopulated])
}

func TestDVRestoreItemAction_Execute_SucceededDV_NilAnnotations(t *testing.T) {
	// A Succeeded DV with nil annotations map should get the annotation added.
	dv := &cdiv1.DataVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dv",
			Namespace: "test-ns",
			// Annotations intentionally nil
		},
		Status: cdiv1.DataVolumeStatus{
			Phase: cdiv1.Succeeded,
		},
	}

	dvMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dv)
	require.NoError(t, err)

	input := &velero.RestoreItemActionExecuteInput{
		Item: &unstructured.Unstructured{Object: dvMap},
	}

	action := NewDVRestoreItemAction(logrus.StandardLogger())
	output, err := action.Execute(input)
	require.NoError(t, err)
	require.NotNil(t, output)

	annotations, found, err := unstructured.NestedStringMap(output.UpdatedItem.UnstructuredContent(), "metadata", "annotations")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "test-dv", annotations[AnnPrePopulated])
}

func TestDVRestoreItemAction_AppliesTo(t *testing.T) {
	action := NewDVRestoreItemAction(logrus.StandardLogger())
	selector, err := action.AppliesTo()
	require.NoError(t, err)
	assert.Equal(t, []string{"DataVolume"}, selector.IncludedResources)
}

func TestDVRestoreItemAction_Execute_NonSucceededDV(t *testing.T) {
	// A DV explicitly in a non-Succeeded, non-empty phase should not get the annotation.
	phases := []cdiv1.DataVolumePhase{
		cdiv1.ImportInProgress,
		cdiv1.CloneInProgress,
		cdiv1.Pending,
		cdiv1.Failed,
	}

	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			dv := &cdiv1.DataVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dv",
					Namespace: "test-ns",
				},
				Status: cdiv1.DataVolumeStatus{
					Phase: phase,
				},
			}

			dvMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dv)
			require.NoError(t, err)

			input := &velero.RestoreItemActionExecuteInput{
				Item: &unstructured.Unstructured{Object: dvMap},
			}

			action := NewDVRestoreItemAction(logrus.StandardLogger())
			output, err := action.Execute(input)
			require.NoError(t, err)
			require.NotNil(t, output)

			annotations, _, _ := unstructured.NestedStringMap(output.UpdatedItem.UnstructuredContent(), "metadata", "annotations")
			_, hasAnnotation := annotations[AnnPrePopulated]
			assert.False(t, hasAnnotation, "prePopulated annotation should not be set for phase %v", phase)
		})
	}
}

func TestDVRestoreItemAction_Execute_EmptyPhaseDV(t *testing.T) {
	// A DV with empty phase (status not captured) should get the annotation.
	dv := &cdiv1.DataVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dv",
			Namespace: "test-ns",
		},
		Status: cdiv1.DataVolumeStatus{
			Phase: "",
		},
	}

	dvMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dv)
	require.NoError(t, err)

	input := &velero.RestoreItemActionExecuteInput{
		Item: &unstructured.Unstructured{Object: dvMap},
	}

	action := NewDVRestoreItemAction(logrus.StandardLogger())
	output, err := action.Execute(input)
	require.NoError(t, err)
	require.NotNil(t, output)

	annotations, found, err := unstructured.NestedStringMap(output.UpdatedItem.UnstructuredContent(), "metadata", "annotations")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "test-dv", annotations[AnnPrePopulated])
}
