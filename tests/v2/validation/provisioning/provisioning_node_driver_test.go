package provisioning

import (
	"context"
	"fmt"
	"testing"

	"github.com/rancher/rancher/tests/framework/clients/rancher"
	management "github.com/rancher/rancher/tests/framework/clients/rancher/generated/management/v3"
	"github.com/rancher/rancher/tests/framework/extensions/cloudcredentials/aws"
	"github.com/rancher/rancher/tests/framework/extensions/cloudcredentials/digitalocean"
	"github.com/rancher/rancher/tests/framework/extensions/cloudcredentials/harvester"
	"github.com/rancher/rancher/tests/framework/extensions/clusters"
	"github.com/rancher/rancher/tests/framework/extensions/machinepools"
	"github.com/rancher/rancher/tests/framework/extensions/users"
	"github.com/rancher/rancher/tests/framework/pkg/config"
	"github.com/rancher/rancher/tests/framework/pkg/namegenerator"
	"github.com/rancher/rancher/tests/framework/pkg/session"
	"github.com/rancher/rancher/tests/framework/pkg/wait"
	"github.com/rancher/rancher/tests/integration/pkg/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RKE2NodeDriverProvisioningTestSuite struct {
	suite.Suite
	client             *rancher.Client
	session            *session.Session
	standardUserClient *rancher.Client
	kubernetesVersions []string
	cnis               []string
}

const (
	baseDOClusterName        = "automationdo-"
	baseAWSClusterName       = "automationaws-"
	baseHarvesterClusterName = "automationharvester-"
	namespace                = "fleet-default"
	defaultRandStringLength  = 5
)

func AppendRandomString(baseClusterName string) string {
	clusterName := baseClusterName + namegenerator.RandStringLower(5)
	return clusterName
}

func (r *RKE2NodeDriverProvisioningTestSuite) TearDownSuite() {
	r.session.Cleanup()
}

func (r *RKE2NodeDriverProvisioningTestSuite) SetupSuite() {
	testSession := session.NewSession(r.T())
	r.session = testSession

	clustersConfig := new(Config)
	config.LoadConfig(ConfigurationFileKey, clustersConfig)

	r.kubernetesVersions = clustersConfig.KubernetesVersions
	r.cnis = clustersConfig.CNIs

	client, err := rancher.NewClient("", testSession)
	require.NoError(r.T(), err)

	r.client = client

	enabled := true
	var testuser = AppendRandomString("testuser")
	user := &management.User{
		Username: testuser,
		Password: "rancherrancher123!",
		Name:     testuser,
		Enabled:  &enabled,
	}

	newUser, err := users.CreateUserWithRole(client, user, "user")
	require.NoError(r.T(), err)

	standardUserClient, err := client.AsUser(newUser)
	require.NoError(r.T(), err)

	r.standardUserClient = standardUserClient
}

func (r *RKE2NodeDriverProvisioningTestSuite) TestProvisioning_RKE2DigitalOceanCluster() {
	subSession := r.session.NewSession()
	defer subSession.Cleanup()

	client, err := r.client.WithSession(subSession)
	require.NoError(r.T(), err)

	cloudCredential, err := digitalocean.CreateDigitalOceanCloudCredentials(client)
	require.NoError(r.T(), err)

	nodeRoles0 := []map[string]bool{
		{
			"controlplane": true,
			"etcd":         true,
			"worker":       true,
		},
	}

	nodeRoles1 := []map[string]bool{
		{
			"controlplane": true,
			"etcd":         false,
			"worker":       false,
		},
		{
			"controlplane": false,
			"etcd":         true,
			"worker":       false,
		},
		{
			"controlplane": false,
			"etcd":         false,
			"worker":       true,
		},
	}

	tests := []struct {
		name      string
		nodeRoles []map[string]bool
		client    *rancher.Client
	}{
		{"1 Node all roles Admin User", nodeRoles0, r.client},
		{"1 Node all roles Standard User", nodeRoles0, r.standardUserClient},
		{"3 nodes - 1 role per node Admin User", nodeRoles1, r.client},
		{"3 nodes - 1 role per node Standard User", nodeRoles1, r.standardUserClient},
	}

	var name string
	for _, tt := range tests {
		for _, kubeVersion := range r.kubernetesVersions {
			name = tt.name + " Kubernetes version: " + kubeVersion
			for _, cni := range r.cnis {
				name += " cni: " + cni
				r.Run(name, func() {
					testSession := session.NewSession(r.T())
					defer testSession.Cleanup()

					testSessionClient, err := tt.client.WithSession(testSession)
					require.NoError(r.T(), err)

					clusterName := AppendRandomString(baseDOClusterName)
					generatedPoolName := fmt.Sprintf("nc-%s-pool1-", clusterName)
					machinePoolConfig := machinepools.NewDigitalOceanMachineConfig(generatedPoolName, namespace)

					machineConfigResp, err := machinepools.CreateMachineConfig(machinepools.DOResourceConfig, machinePoolConfig, testSessionClient)
					require.NoError(r.T(), err)

					machinePools := machinepools.RKEMachinePoolSetup(tt.nodeRoles, machineConfigResp)

					cluster := clusters.NewRKE2ClusterConfig(clusterName, namespace, cni, cloudCredential.ID, kubeVersion, machinePools)

					clusterResp, err := clusters.CreateRKE2Cluster(testSessionClient, cluster)
					require.NoError(r.T(), err)

					result, err := testSessionClient.Provisioning.Clusters(namespace).Watch(context.TODO(), metav1.ListOptions{
						FieldSelector:  "metadata.name=" + clusterName,
						TimeoutSeconds: &defaults.WatchTimeoutSeconds,
					})
					require.NoError(r.T(), err)

					checkFunc := clusters.IsProvisioningClusterReady

					err = wait.WatchWait(result, checkFunc)
					assert.NoError(r.T(), err)
					assert.Equal(r.T(), clusterName, clusterResp.Name)

				})
			}
		}
	}
}

func (r *RKE2NodeDriverProvisioningTestSuite) TestProvisioning_RKE2igitalOceanClusterDynamicInput() {
	nodesAndRoles := NodesAndRolesInput()
	if len(nodesAndRoles) == 0 {
		r.T().Skip()
	}

	subSession := r.session.NewSession()
	defer subSession.Cleanup()

	client, err := r.client.WithSession(subSession)
	require.NoError(r.T(), err)

	cloudCredential, err := digitalocean.CreateDigitalOceanCloudCredentials(client)
	require.NoError(r.T(), err)

	tests := []struct {
		name   string
		client *rancher.Client
	}{
		{"Admin User", r.client},
		{"Standard User", r.standardUserClient},
	}

	var name string
	for _, tt := range tests {
		for _, kubeVersion := range r.kubernetesVersions {
			name = tt.name + " Kubernetes version: " + kubeVersion
			for _, cni := range r.cnis {
				name += " cni: " + cni
				r.Run(name, func() {
					testSession := session.NewSession(r.T())
					defer testSession.Cleanup()

					testSessionClient, err := tt.client.WithSession(testSession)
					require.NoError(r.T(), err)

					clusterName := AppendRandomString(baseDOClusterName)
					generatedPoolName := fmt.Sprintf("nc-%s-pool1-", clusterName)
					machinePoolConfig := machinepools.NewDigitalOceanMachineConfig(generatedPoolName, namespace)

					machineConfigResp, err := machinepools.CreateMachineConfig(machinepools.DOResourceConfig, machinePoolConfig, testSessionClient)
					require.NoError(r.T(), err)

					machinePools := machinepools.RKEMachinePoolSetup(nodesAndRoles, machineConfigResp)

					cluster := clusters.NewRKE2ClusterConfig(clusterName, namespace, cni, cloudCredential.ID, kubeVersion, machinePools)

					clusterResp, err := clusters.CreateRKE2Cluster(testSessionClient, cluster)
					require.NoError(r.T(), err)

					result, err := testSessionClient.Provisioning.Clusters(namespace).Watch(context.TODO(), metav1.ListOptions{
						FieldSelector:  "metadata.name=" + clusterName,
						TimeoutSeconds: &defaults.WatchTimeoutSeconds,
					})
					require.NoError(r.T(), err)

					checkFunc := clusters.IsProvisioningClusterReady

					err = wait.WatchWait(result, checkFunc)
					assert.NoError(r.T(), err)
					assert.Equal(r.T(), clusterName, clusterResp.Name)

				})
			}
		}
	}
}

func (r *RKE2NodeDriverProvisioningTestSuite) TestProvisioning_RKE2AWSCluster() {
	subSession := r.session.NewSession()
	defer subSession.Cleanup()

	client, err := r.client.WithSession(subSession)
	require.NoError(r.T(), err)

	cloudCredential, err := aws.CreateAWSCloudCredentials(client)
	require.NoError(r.T(), err)

	nodeRoles0 := []map[string]bool{
		{
			"controlplane": true,
			"etcd":         true,
			"worker":       true,
		},
	}

	nodeRoles1 := []map[string]bool{
		{
			"controlplane": true,
			"etcd":         false,
			"worker":       false,
		},
		{
			"controlplane": false,
			"etcd":         true,
			"worker":       false,
		},
		{
			"controlplane": false,
			"etcd":         false,
			"worker":       true,
		},
	}

	tests := []struct {
		name      string
		nodeRoles []map[string]bool
		client    *rancher.Client
	}{
		{"1 Node all roles Admin User", nodeRoles0, r.client},
		{"1 Node all roles Standard User", nodeRoles0, r.standardUserClient},
		{"3 nodes - 1 role per node Admin User", nodeRoles1, r.client},
		{"3 nodes - 1 role per node Standard User", nodeRoles1, r.standardUserClient},
	}

	var name string
	for _, tt := range tests {
		for _, kubeVersion := range r.kubernetesVersions {
			name = tt.name + " Kubernetes version: " + kubeVersion
			for _, cni := range r.cnis {
				name += " cni: " + cni
				r.Run(name, func() {
					testSession := session.NewSession(r.T())
					defer testSession.Cleanup()

					testSessionClient, err := tt.client.WithSession(testSession)
					require.NoError(r.T(), err)

					clusterName := AppendRandomString(baseAWSClusterName)
					generatedPoolName := fmt.Sprintf("nc-%s-pool1-", clusterName)
					machinePoolConfig := machinepools.NewAWSMachineConfig(generatedPoolName, namespace)

					machineConfigResp, err := machinepools.CreateMachineConfig(machinepools.AWSResourceConfig, machinePoolConfig, testSessionClient)
					require.NoError(r.T(), err)

					machinePools := machinepools.RKEMachinePoolSetup(tt.nodeRoles, machineConfigResp)

					cluster := clusters.NewRKE2ClusterConfig(clusterName, namespace, cni, cloudCredential.ID, kubeVersion, machinePools)

					clusterResp, err := clusters.CreateRKE2Cluster(testSessionClient, cluster)
					require.NoError(r.T(), err)

					result, err := testSessionClient.Provisioning.Clusters(namespace).Watch(context.TODO(), metav1.ListOptions{
						FieldSelector:  "metadata.name=" + clusterName,
						TimeoutSeconds: &defaults.WatchTimeoutSeconds,
					})
					require.NoError(r.T(), err)

					checkFunc := clusters.IsProvisioningClusterReady

					err = wait.WatchWait(result, checkFunc)
					assert.NoError(r.T(), err)
					assert.Equal(r.T(), clusterName, clusterResp.Name)

				})
			}
		}
	}
}

func (r *RKE2NodeDriverProvisioningTestSuite) TestProvisioning_RKE2AWSClusterDynamicInput() {
	nodesAndRoles := NodesAndRolesInput()
	if len(nodesAndRoles) == 0 {
		r.T().Skip()
	}

	subSession := r.session.NewSession()
	defer subSession.Cleanup()

	client, err := r.client.WithSession(subSession)
	require.NoError(r.T(), err)

	cloudCredential, err := aws.CreateAWSCloudCredentials(client)
	require.NoError(r.T(), err)

	tests := []struct {
		name   string
		client *rancher.Client
	}{
		{"Admin User", r.client},
		{"Standard User", r.standardUserClient},
	}

	var name string
	for _, tt := range tests {
		for _, kubeVersion := range r.kubernetesVersions {
			name = tt.name + " Kubernetes version: " + kubeVersion
			for _, cni := range r.cnis {
				name += " cni: " + cni
				r.Run(name, func() {
					testSession := session.NewSession(r.T())
					defer testSession.Cleanup()

					testSessionClient, err := tt.client.WithSession(testSession)
					require.NoError(r.T(), err)

					clusterName := AppendRandomString(baseAWSClusterName)
					generatedPoolName := fmt.Sprintf("nc-%s-pool1-", clusterName)
					machinePoolConfig := machinepools.NewAWSMachineConfig(generatedPoolName, namespace)

					machineConfigResp, err := machinepools.CreateMachineConfig(machinepools.AWSResourceConfig, machinePoolConfig, testSessionClient)
					require.NoError(r.T(), err)

					machinePools := machinepools.RKEMachinePoolSetup(nodesAndRoles, machineConfigResp)

					cluster := clusters.NewRKE2ClusterConfig(clusterName, namespace, cni, cloudCredential.ID, kubeVersion, machinePools)

					clusterResp, err := clusters.CreateRKE2Cluster(testSessionClient, cluster)
					require.NoError(r.T(), err)

					result, err := testSessionClient.Provisioning.Clusters(namespace).Watch(context.TODO(), metav1.ListOptions{
						FieldSelector:  "metadata.name=" + clusterName,
						TimeoutSeconds: &defaults.WatchTimeoutSeconds,
					})
					require.NoError(r.T(), err)

					checkFunc := clusters.IsProvisioningClusterReady

					err = wait.WatchWait(result, checkFunc)
					assert.NoError(r.T(), err)
					assert.Equal(r.T(), clusterName, clusterResp.Name)

				})
			}
		}
	}
}

func (r *RKE2NodeDriverProvisioningTestSuite) TestProvisioning_RKE2HarvesterCluster() {
	subSession := r.session.NewSession()
	defer subSession.Cleanup()

	client, err := r.client.WithSession(subSession)
	require.NoError(r.T(), err)

	cloudCredential, err := harvester.CreateHarvesterCloudCredentials(client)
	require.NoError(r.T(), err)

	nodeRoles0 := []map[string]bool{
		{
			"controlplane": true,
			"etcd":         true,
			"worker":       true,
		},
	}

	nodeRoles1 := []map[string]bool{
		{
			"controlplane": true,
			"etcd":         false,
			"worker":       false,
		},
		{
			"controlplane": false,
			"etcd":         true,
			"worker":       false,
		},
		{
			"controlplane": false,
			"etcd":         false,
			"worker":       true,
		},
	}

	tests := []struct {
		name      string
		nodeRoles []map[string]bool
		client    *rancher.Client
	}{
		{"1 Node all roles Admin User", nodeRoles0, r.client},
		{"1 Node all roles Standard User", nodeRoles0, r.standardUserClient},
		{"3 nodes - 1 role per node Admin User", nodeRoles1, r.client},
		{"3 nodes - 1 role per node Standard User", nodeRoles1, r.standardUserClient},
	}

	var name string
	for _, tt := range tests {
		for _, kubeVersion := range r.kubernetesVersions {
			name = tt.name + " Kubernetes version: " + kubeVersion
			for _, cni := range r.cnis {
				name += " cni: " + cni
				r.Run(name, func() {
					testSession := session.NewSession(r.T())
					defer testSession.Cleanup()

					testSessionClient, err := tt.client.WithSession(testSession)
					require.NoError(r.T(), err)

					clusterName := AppendRandomString(baseHarvesterClusterName)
					generatedPoolName := fmt.Sprintf("nc-%s-pool1-", clusterName)
					machinePoolConfig := machinepools.NewHarvesterMachineConfig(generatedPoolName, namespace)

					machineConfigResp, err := machinepools.CreateMachineConfig(machinepools.HarvesterResourceConfig, machinePoolConfig, testSessionClient)
					require.NoError(r.T(), err)

					machinePools := machinepools.RKEMachinePoolSetup(tt.nodeRoles, machineConfigResp)

					cluster := clusters.NewRKE2ClusterConfig(clusterName, namespace, cni, cloudCredential.ID, kubeVersion, machinePools)

					clusterResp, err := clusters.CreateRKE2Cluster(testSessionClient, cluster)
					require.NoError(r.T(), err)

					result, err := testSessionClient.Provisioning.Clusters(namespace).Watch(context.TODO(), metav1.ListOptions{
						FieldSelector:  "metadata.name=" + clusterName,
						TimeoutSeconds: &defaults.WatchTimeoutSeconds,
					})
					require.NoError(r.T(), err)

					checkFunc := clusters.IsProvisioningClusterReady

					err = wait.WatchWait(result, checkFunc)
					assert.NoError(r.T(), err)
					assert.Equal(r.T(), clusterName, clusterResp.Name)

				})
			}
		}
	}
}

func (r *RKE2NodeDriverProvisioningTestSuite) TestProvisioning_RKE2HarvesterClusterDynamicInput() {
	nodesAndRoles := NodesAndRolesInput()
	if len(nodesAndRoles) == 0 {
		r.T().Skip()
	}

	subSession := r.session.NewSession()
	defer subSession.Cleanup()

	client, err := r.client.WithSession(subSession)
	require.NoError(r.T(), err)

	cloudCredential, err := harvester.CreateHarvesterCloudCredentials(client)
	require.NoError(r.T(), err)

	tests := []struct {
		name   string
		client *rancher.Client
	}{
		{"Admin User", r.client},
		{"Standard User", r.standardUserClient},
	}

	var name string
	for _, tt := range tests {
		for _, kubeVersion := range r.kubernetesVersions {
			name = tt.name + " Kubernetes version: " + kubeVersion
			for _, cni := range r.cnis {
				name += " cni: " + cni
				r.Run(name, func() {
					testSession := session.NewSession(r.T())
					defer testSession.Cleanup()

					testSessionClient, err := tt.client.WithSession(testSession)
					require.NoError(r.T(), err)

					clusterName := AppendRandomString(baseHarvesterClusterName)
					generatedPoolName := fmt.Sprintf("nc-%s-pool1-", clusterName)
					machinePoolConfig := machinepools.NewHarvesterMachineConfig(generatedPoolName, namespace)

					machineConfigResp, err := machinepools.CreateMachineConfig(machinepools.HarvesterResourceConfig, machinePoolConfig, testSessionClient)
					require.NoError(r.T(), err)

					machinePools := machinepools.RKEMachinePoolSetup(nodesAndRoles, machineConfigResp)

					cluster := clusters.NewRKE2ClusterConfig(clusterName, namespace, cni, cloudCredential.ID, kubeVersion, machinePools)

					clusterResp, err := clusters.CreateRKE2Cluster(testSessionClient, cluster)
					require.NoError(r.T(), err)

					result, err := testSessionClient.Provisioning.Clusters(namespace).Watch(context.TODO(), metav1.ListOptions{
						FieldSelector:  "metadata.name=" + clusterName,
						TimeoutSeconds: &defaults.WatchTimeoutSeconds,
					})
					require.NoError(r.T(), err)

					checkFunc := clusters.IsProvisioningClusterReady

					err = wait.WatchWait(result, checkFunc)
					assert.NoError(r.T(), err)
					assert.Equal(r.T(), clusterName, clusterResp.Name)

				})
			}
		}
	}
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestProvisioningTestSuite(t *testing.T) {
	suite.Run(t, new(RKE2NodeDriverProvisioningTestSuite))
}
