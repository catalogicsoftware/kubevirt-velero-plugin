package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newTestPVC(annotations, labels map[string]interface{}, ownerReferences []interface{}) *unstructured.Unstructured {
	metadata := map[string]interface{}{
		"name":      "test-pvc",
		"namespace": "test-namespace",
	}
	if annotations != nil {
		metadata["annotations"] = annotations
	}
	if labels != nil {
		metadata["labels"] = labels
	}
	if ownerReferences != nil {
		metadata["ownerReferences"] = ownerReferences
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata":   metadata,
			"spec":       map[string]interface{}{},
		},
	}
}

func TestPvcRestoreExecute(t *testing.T) {
	dataVolumeOwner := []interface{}{
		map[string]interface{}{
			"apiVersion": "cdi.kubevirt.io/v1beta1",
			"kind":       "DataVolume",
			"name":       "test-datavolume",
		},
	}
	otherOwner := []interface{}{
		map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"name":       "test-pod",
		},
	}
	cdiLabels := map[string]interface{}{
		"cdi.kubevirt.io/storage.dataVolumeName": "labelled-datavolume",
	}

	testCases := []struct {
		name string
		// Velero resets the metadata of the item being restored before restore
		// item actions run, which removes the owner references. They are only
		// left on the item taken from the backup.
		item           *unstructured.Unstructured
		itemFromBackup *unstructured.Unstructured
		skip           bool
		// The expected value of the populatedFor annotation, empty when the
		// annotation is not expected to be present.
		populatedFor string
	}{
		{
			name:           "Unfinished PVC is skipped",
			item:           newTestPVC(map[string]interface{}{AnnInProgress: "test-pvc"}, nil, nil),
			itemFromBackup: newTestPVC(map[string]interface{}{AnnInProgress: "test-pvc"}, nil, dataVolumeOwner),
			skip:           true,
		},
		{
			name:           "PVC owned by a DataVolume in the backup is annotated",
			item:           newTestPVC(nil, nil, nil),
			itemFromBackup: newTestPVC(nil, nil, dataVolumeOwner),
			populatedFor:   "test-datavolume",
		},
		{
			name:           "PVC owned by a DataVolume is annotated from the CDI label",
			item:           newTestPVC(nil, cdiLabels, nil),
			itemFromBackup: newTestPVC(nil, cdiLabels, nil),
			populatedFor:   "labelled-datavolume",
		},
		{
			name:           "Owner references take precedence over the CDI label",
			item:           newTestPVC(nil, cdiLabels, nil),
			itemFromBackup: newTestPVC(nil, cdiLabels, dataVolumeOwner),
			populatedFor:   "test-datavolume",
		},
		{
			name:           "Existing populatedFor annotation is not overwritten",
			item:           newTestPVC(map[string]interface{}{AnnPopulatedFor: "original-datavolume"}, nil, nil),
			itemFromBackup: newTestPVC(map[string]interface{}{AnnPopulatedFor: "original-datavolume"}, nil, dataVolumeOwner),
			populatedFor:   "original-datavolume",
		},
		{
			name:           "PVC not owned by a DataVolume is left alone",
			item:           newTestPVC(nil, nil, nil),
			itemFromBackup: newTestPVC(nil, nil, otherOwner),
			populatedFor:   "",
		},
		{
			name:           "PVC without any owner is left alone",
			item:           newTestPVC(nil, nil, nil),
			itemFromBackup: newTestPVC(nil, nil, nil),
			populatedFor:   "",
		},
		{
			name:         "Owner references on the restored item are still honoured",
			item:         newTestPVC(nil, nil, dataVolumeOwner),
			populatedFor: "test-datavolume",
		},
	}

	logrus.SetLevel(logrus.ErrorLevel)
	action := NewPVCRestoreItemAction(logrus.StandardLogger())
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := velero.RestoreItemActionExecuteInput{Item: tc.item}
			if tc.itemFromBackup != nil {
				input.ItemFromBackup = tc.itemFromBackup
			}

			output, err := action.Execute(&input)
			assert.NoError(t, err)
			assert.Equal(t, tc.skip, output.SkipRestore)
			if tc.skip {
				return
			}

			metadata, err := meta.Accessor(output.UpdatedItem)
			assert.NoError(t, err)
			assert.Equal(t, tc.populatedFor, metadata.GetAnnotations()[AnnPopulatedFor])
		})
	}
}
