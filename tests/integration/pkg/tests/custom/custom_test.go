package custom

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	provisioningv1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	"github.com/rancher/rancher/pkg/provisioningv2/rke2/planner"
	"github.com/rancher/rancher/tests/integration/pkg/clients"
	"github.com/rancher/rancher/tests/integration/pkg/cluster"
	"github.com/rancher/rancher/tests/integration/pkg/systemdnode"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSystemAgentVersion(t *testing.T) {
	clients, err := clients.New()
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()

	setting, err := clients.Mgmt.Setting().Get("system-agent-version", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, setting.Value)
	assert.True(t, setting.Value == os.Getenv("CATTLE_SYSTEM_AGENT_VERSION"))
}

func TestCustomOneNode(t *testing.T) {
	if strings.ToLower(os.Getenv("DIST")) == "rke2" {
		t.Skip()
	}
	clients, err := clients.New()
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()

	c, err := cluster.New(clients, &provisioningv1.Cluster{
		Spec: provisioningv1.ClusterSpec{
			RKEConfig: &provisioningv1.RKEConfig{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	command, err := cluster.CustomCommand(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, command)

	_, err = systemdnode.New(clients, c.Namespace, "#!/usr/bin/env sh\n"+command+" --worker --etcd --controlplane --label foo=bar --label ball=life")
	if err != nil {
		t.Fatal(err)
	}

	_, err = cluster.WaitForCreate(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	machines, err := cluster.Machines(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.Len(t, machines.Items, 1)
	assert.Equal(t, machines.Items[0].Labels[planner.WorkerRoleLabel], "true")
	assert.Equal(t, machines.Items[0].Labels[planner.ControlPlaneRoleLabel], "true")
	assert.Equal(t, machines.Items[0].Labels[planner.EtcdRoleLabel], "true")
	assert.Len(t, machines.Items[0].Status.Addresses, 2)

	assert.NotEmpty(t, machines.Items[0].Annotations[planner.LabelsAnnotation])
	var labels map[string]string
	if err := json.Unmarshal([]byte(machines.Items[0].Annotations[planner.LabelsAnnotation]), &labels); err != nil {
		t.Error(err)
	}
	assert.Equal(t, labels, map[string]string{"cattle.io/os": "linux", "foo": "bar", "ball": "life"})
}

func TestCustomThreeNode(t *testing.T) {
	clients, err := clients.New()
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()

	c, err := cluster.New(clients, &provisioningv1.Cluster{
		Spec: provisioningv1.ClusterSpec{
			RKEConfig: &provisioningv1.RKEConfig{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	command, err := cluster.CustomCommand(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, command)

	for i := 0; i < 3; i++ {
		_, err = systemdnode.New(clients, c.Namespace, "#!/usr/bin/env sh\n"+command+" --worker --etcd --controlplane --label rancher=awesome")
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err = cluster.WaitForCreate(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	machines, err := cluster.Machines(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.Len(t, machines.Items, 3)
	for _, m := range machines.Items {
		assert.Equal(t, m.Labels[planner.WorkerRoleLabel], "true")
		assert.Equal(t, m.Labels[planner.ControlPlaneRoleLabel], "true")
		assert.Equal(t, m.Labels[planner.EtcdRoleLabel], "true")

		assert.NotEmpty(t, m.Annotations[planner.LabelsAnnotation])
		var labels map[string]string
		if err := json.Unmarshal([]byte(m.Annotations[planner.LabelsAnnotation]), &labels); err != nil {
			t.Error(err)
		}
		assert.Equal(t, labels, map[string]string{"cattle.io/os": "linux", "rancher": "awesome"})
	}
}

func TestCustomUniqueRoles(t *testing.T) {
	clients, err := clients.New()
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()

	c, err := cluster.New(clients, &provisioningv1.Cluster{
		Spec: provisioningv1.ClusterSpec{
			RKEConfig: &provisioningv1.RKEConfig{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	command, err := cluster.CustomCommand(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, command)

	for i := 0; i < 3; i++ {
		_, err = systemdnode.New(clients, c.Namespace, "#!/usr/bin/env sh\n"+command+" --etcd")
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 1; i++ {
		_, err = systemdnode.New(clients, c.Namespace, "#!/usr/bin/env sh\n"+command+" --controlplane")
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err = systemdnode.New(clients, c.Namespace, "#!/usr/bin/env sh\n"+command+" --worker")
	if err != nil {
		t.Fatal(err)
	}

	_, err = cluster.WaitForCreate(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	machines, err := cluster.Machines(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.Len(t, machines.Items, 5)
	var (
		worker       = 0
		controlPlane = 0
		etcd         = 0
	)
	for _, m := range machines.Items {
		if m.Labels[planner.WorkerRoleLabel] == "true" {
			worker++
		}
		if m.Labels[planner.ControlPlaneRoleLabel] == "true" {
			controlPlane++
		}
		if m.Labels[planner.EtcdRoleLabel] == "true" {
			etcd++
		}
	}

	assert.Equal(t, worker, 1)
	assert.Equal(t, etcd, 3)
	assert.Equal(t, controlPlane, 1)
}

func TestCustomThreeNodeWithTaints(t *testing.T) {
	if strings.ToLower(os.Getenv("DIST")) == "rke2" {
		t.Skip()
	}
	clients, err := clients.New()
	if err != nil {
		t.Fatal(err)
	}
	defer clients.Close()

	c, err := cluster.New(clients, &provisioningv1.Cluster{
		Spec: provisioningv1.ClusterSpec{
			RKEConfig: &provisioningv1.RKEConfig{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	command, err := cluster.CustomCommand(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, command)

	for i := 0; i < 3; i++ {
		var taint string
		// Put a taint on one of the nodes.
		if i == 1 {
			taint = " --taint key=value:NoExecute"
		}
		_, err = systemdnode.New(clients, c.Namespace, "#!/usr/bin/env sh\n"+command+" --worker --etcd --controlplane --label rancher=awesome"+taint)
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err = cluster.WaitForCreate(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	machines, err := cluster.Machines(clients, c)
	if err != nil {
		t.Fatal(err)
	}

	var taintFound bool
	assert.Len(t, machines.Items, 3)
	for _, m := range machines.Items {
		assert.Equal(t, m.Labels[planner.WorkerRoleLabel], "true")
		assert.Equal(t, m.Labels[planner.ControlPlaneRoleLabel], "true")
		assert.Equal(t, m.Labels[planner.EtcdRoleLabel], "true")

		assert.NotEmpty(t, m.Annotations[planner.LabelsAnnotation])
		var labels map[string]string
		if err := json.Unmarshal([]byte(m.Annotations[planner.LabelsAnnotation]), &labels); err != nil {
			t.Error(err)
		}
		assert.Equal(t, labels, map[string]string{"cattle.io/os": "linux", "rancher": "awesome"})

		if len(m.Annotations[planner.TaintsAnnotation]) != 0 {
			// Only one node should have the taint
			assert.False(t, taintFound)

			var taints []corev1.Taint
			if err := json.Unmarshal([]byte(m.Annotations[planner.TaintsAnnotation]), &taints); err != nil {
				t.Error(err)
			}

			assert.Equal(t, len(taints), 1)
			assert.Equal(t, taints[0].Key, "key")
			assert.Equal(t, taints[0].Value, "value")
			assert.Equal(t, taints[0].Effect, corev1.TaintEffect("NoExecute"))
			taintFound = true
		}
	}

	assert.True(t, taintFound)
}
