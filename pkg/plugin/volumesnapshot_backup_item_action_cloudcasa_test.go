package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v7/apis/volumesnapshot/v1"
	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
)

// A VolumeSnapshot outlives the PVC it was taken from. Such a snapshot is
// backed up as it is, without the PVC UID label, instead of failing the backup.
func TestVolumeSnapshotBackupOfADeletedPVC(t *testing.T) {
	vs := &snapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vs", Namespace: "test-namespace"},
		Spec: snapshotv1.VolumeSnapshotSpec{
			Source: snapshotv1.VolumeSnapshotSource{PersistentVolumeClaimName: stringPtr("deleted-pvc")},
		},
	}
	item, err := runtime.DefaultUnstructuredConverter.ToUnstructured(vs)
	require.NoError(t, err)

	action := &VolumeSnapshotBackupItemAction{
		log: logrus.StandardLogger(),
		// The namespace's PVCs were listed already and deleted-pvc is not one of them.
		namespacePVCs: map[string]map[string]string{"test-namespace": {"other-pvc": "uid-1"}},
	}
	result, _, err := action.Execute(&unstructured.Unstructured{Object: item}, &v1.Backup{})
	require.NoError(t, err)
	require.NotNil(t, result)

	var resultVS snapshotv1.VolumeSnapshot
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(result.UnstructuredContent(), &resultVS))
	assert.NotContains(t, resultVS.Labels, util.PVCUIDLabel)
}
