package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kvcore "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
)

// backedUpStandaloneVMI is a VMI that no VM owns, with a MAC address and a
// firmware UUID.
func backedUpStandaloneVMI() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachineInstance",
		"metadata": map[string]interface{}{
			"name":      "vmi1",
			"namespace": "ns1",
			"labels":    map[string]interface{}{"app": "vmi1", "kubevirt.io/nodeName": "node1"},
		},
		"spec": map[string]interface{}{
			"domain": map[string]interface{}{
				"firmware": map[string]interface{}{"uuid": "original-uuid"},
				"devices": map[string]interface{}{
					"interfaces": []interface{}{map[string]interface{}{"name": "default", "macAddress": "02:00:00:00:00:01"}},
				},
			},
		},
	}}
}

func restoreStandaloneVMI(t *testing.T, labels map[string]string) (*unstructured.Unstructured, *kvcore.VirtualMachineInstance) {
	t.Helper()
	backedUp := backedUpStandaloneVMI()
	output, err := NewVMIRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
		Item:           backedUp.DeepCopy(),
		ItemFromBackup: backedUp,
		Restore:        &velerov1.Restore{ObjectMeta: metav1.ObjectMeta{Name: "test-restore", Namespace: "velero", Labels: labels}},
	})
	require.NoError(t, err)
	require.False(t, output.SkipRestore)
	item := output.UpdatedItem.(*unstructured.Unstructured)
	vmi := new(kvcore.VirtualMachineInstance)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(item.UnstructuredContent(), vmi))
	return item, vmi
}

func TestAStandaloneVMIIsRestoredWithANewMacAddress(t *testing.T) {
	_, vmi := restoreStandaloneVMI(t, map[string]string{util.ClearMacAddressLabel: "true"})

	assert.Empty(t, vmi.Spec.Domain.Devices.Interfaces[0].MacAddress)
	assert.Equal(t, "default", vmi.Spec.Domain.Devices.Interfaces[0].Name)
	assert.Equal(t, "original-uuid", string(vmi.Spec.Domain.Firmware.UUID))
	assert.Equal(t, map[string]string{"app": "vmi1"}, vmi.Labels, "restricted labels are still removed")
}

func TestAStandaloneVMIIsRestoredWithANewFirmwareUUID(t *testing.T) {
	_, vmi := restoreStandaloneVMI(t, map[string]string{util.GenerateNewFirmwareUUIDLabel: "true"})

	assert.NotEmpty(t, vmi.Spec.Domain.Firmware.UUID)
	assert.NotEqual(t, "original-uuid", string(vmi.Spec.Domain.Firmware.UUID))
	assert.Equal(t, "02:00:00:00:00:01", vmi.Spec.Domain.Devices.Interfaces[0].MacAddress)
}

// Without the options the spec is restored exactly as it was backed up.
func TestAStandaloneVMIWithoutTheOptionsKeepsItsSpec(t *testing.T) {
	item, _ := restoreStandaloneVMI(t, nil)

	assert.Equal(t, backedUpStandaloneVMI().Object["spec"], item.Object["spec"])
}

// A VMI of a newer KubeVirt than the plugin knows keeps the fields the plugin
// does not know, with the options on.
func TestAStandaloneVMIKeepsTheFieldsThePluginDoesNotKnow(t *testing.T) {
	backedUp := backedUpStandaloneVMI()
	spec := backedUp.Object["spec"].(map[string]interface{})
	spec["futureField"] = map[string]interface{}{"x": "y"}
	iface := spec["domain"].(map[string]interface{})["devices"].(map[string]interface{})["interfaces"].([]interface{})[0].(map[string]interface{})
	iface["futureInterfaceField"] = "z"

	output, err := NewVMIRestoreItemAction(logrus.New()).Execute(&velero.RestoreItemActionExecuteInput{
		Item:           backedUp.DeepCopy(),
		ItemFromBackup: backedUp,
		Restore: &velerov1.Restore{ObjectMeta: metav1.ObjectMeta{Name: "test-restore", Namespace: "velero",
			Labels: map[string]string{util.ClearMacAddressLabel: "true", util.GenerateNewFirmwareUUIDLabel: "true"}}},
	})
	require.NoError(t, err)

	item := output.UpdatedItem.(*unstructured.Unstructured)
	assert.Equal(t, map[string]interface{}{"x": "y"}, item.Object["spec"].(map[string]interface{})["futureField"])
	interfaces, _, _ := unstructured.NestedSlice(item.Object, "spec", "domain", "devices", "interfaces")
	assert.Equal(t, map[string]interface{}{"name": "default", "futureInterfaceField": "z"}, interfaces[0])
	uuid, _, _ := unstructured.NestedString(item.Object, "spec", "domain", "firmware", "uuid")
	assert.NotEqual(t, "original-uuid", uuid)
	assert.NotEmpty(t, uuid)
}
