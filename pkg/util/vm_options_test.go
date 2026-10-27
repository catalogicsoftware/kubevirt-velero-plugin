package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMOptionsOf(t *testing.T) {
	restore := &velerov1.Restore{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{VMOptionsAnnotation: `[
		{"namespace": "ns1", "name": "vm1", "newName": "vm1-clone", "items": [
			{"resource": "persistentvolumeclaims", "name": "vm1-disk", "newName": "vm1-clone-disk"}
		]}
	]`}}}

	rename, err := VMOptionsOf(restore, "ns1", "vm1")
	require.NoError(t, err)
	require.NotNil(t, rename)
	assert.Equal(t, "vm1-clone", rename.NewName)
	assert.Equal(t, "vm1-clone-disk", rename.NewNameOf("persistentvolumeclaims", "vm1-disk"))
	assert.Empty(t, rename.NewNameOf("datavolumes.cdi.kubevirt.io", "vm1-disk"))

	for _, vm := range [][2]string{{"ns2", "vm1"}, {"ns1", "vm2"}} {
		rename, err = VMOptionsOf(restore, vm[0], vm[1])
		assert.NoError(t, err)
		assert.Nil(t, rename, "%s/%s is not renamed", vm[0], vm[1])
	}

	rename, err = VMOptionsOf(&velerov1.Restore{}, "ns1", "vm1")
	assert.NoError(t, err)
	assert.Nil(t, rename)

	restore.Annotations[VMOptionsAnnotation] = "{"
	_, err = VMOptionsOf(restore, "ns1", "vm1")
	assert.Error(t, err)
}
