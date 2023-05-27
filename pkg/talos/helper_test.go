package talos

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	cloudproviderapi "k8s.io/cloud-provider/api"
)

func TestGetNodeAddresses(t *testing.T) {
	cfg := cloudConfig{}

	for _, tt := range []struct {
		name       string
		cfg        cloudConfig
		platform   string
		providedIP string
		ifaces     []network.AddressStatusSpec
		expected   []v1.NodeAddress
	}{
		{
			name:       "nocloud has no PublicIPs",
			cfg:        cfg,
			platform:   "nocloud",
			providedIP: "192.168.0.1",
			ifaces: []network.AddressStatusSpec{
				{Address: netip.MustParsePrefix("192.168.0.1/24")},
				{Address: netip.MustParsePrefix("fe80::e0b5:71ff:fe24:7e60/64")},
				{Address: netip.MustParsePrefix("fd15:1:2::192:168:0:1/64")},
				{Address: netip.MustParsePrefix("fd43:fe8a:be2:ab02:dc3c:38ff:fe51:5022/64"), LinkName: "kubespan"},
			},
			expected: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
			},
		},
		{
			name:       "nocloud has many PublicIPs",
			cfg:        cfg,
			platform:   "nocloud",
			providedIP: "192.168.0.1",
			ifaces: []network.AddressStatusSpec{
				{Address: netip.MustParsePrefix("192.168.0.1/24")},
				{Address: netip.MustParsePrefix("fe80::e0b5:71ff:fe24:7e60/64")},
				{Address: netip.MustParsePrefix("fd15:1:2::192:168:0:1/64")},
				{Address: netip.MustParsePrefix("1.2.3.4/24")},
				{Address: netip.MustParsePrefix("4.3.2.1/24")},
				{Address: netip.MustParsePrefix("2001:1234::1/64")},
				{Address: netip.MustParsePrefix("2001:1234:4321::32/64")},
			},
			expected: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
				{Type: v1.NodeExternalIP, Address: "1.2.3.4"},
				{Type: v1.NodeExternalIP, Address: "2001:1234:4321::32"},
			},
		},
		{
			name:       "nocloud has many PublicIPs (IPv6 preferred)",
			cfg:        cloudConfig{Global: cloudConfigGlobal{PreferIPv6: true}},
			platform:   "nocloud",
			providedIP: "192.168.0.1",
			ifaces: []network.AddressStatusSpec{
				{Address: netip.MustParsePrefix("192.168.0.1/24")},
				{Address: netip.MustParsePrefix("fe80::e0b5:71ff:fe24:7e60/64")},
				{Address: netip.MustParsePrefix("fd15:1:2::192:168:0:1/64")},
				{Address: netip.MustParsePrefix("1.2.3.4/24")},
				{Address: netip.MustParsePrefix("4.3.2.1/24")},
				{Address: netip.MustParsePrefix("2001:1234::1/64")},
				{Address: netip.MustParsePrefix("2001:1234:4321::32/64")},
			},
			expected: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
				{Type: v1.NodeExternalIP, Address: "2001:1234:4321::32"},
				{Type: v1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
		{
			name:       "metal has PublicIPs",
			cfg:        cfg,
			platform:   "metal",
			providedIP: "192.168.0.1",
			ifaces: []network.AddressStatusSpec{
				{Address: netip.MustParsePrefix("192.168.0.1/24")},
				{Address: netip.MustParsePrefix("fe80::e0b5:71ff:fe24:7e60/64")},
				{Address: netip.MustParsePrefix("fd15:1:2::192:168:0:1/64")},
				{Address: netip.MustParsePrefix("1.2.3.4/24")},
				{Address: netip.MustParsePrefix("2001:1234::1/128")},
			},
			expected: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
				{Type: v1.NodeExternalIP, Address: "1.2.3.4"},
				{Type: v1.NodeExternalIP, Address: "2001:1234::1"},
			},
		},
		{
			name:       "gcp has provided PublicIPs",
			cfg:        cfg,
			platform:   "gcp",
			providedIP: "192.168.0.1",
			ifaces: []network.AddressStatusSpec{
				{Address: netip.MustParsePrefix("192.168.0.1/24")},
				{Address: netip.MustParsePrefix("fe80::e0b5:71ff:fe24:7e60/64")},
				{Address: netip.MustParsePrefix("1.2.3.4/24"), LinkName: "external"},
				{Address: netip.MustParsePrefix("4.3.2.1/24")},
				{Address: netip.MustParsePrefix("2001:1234::1/128"), LinkName: "external"},
				{Address: netip.MustParsePrefix("2001:1234::123/64")},
			},
			expected: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
				{Type: v1.NodeExternalIP, Address: "1.2.3.4"},
				{Type: v1.NodeExternalIP, Address: "2001:1234::1"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			addresses := getNodeAddresses(&tt.cfg, tt.platform, tt.providedIP, tt.ifaces)

			assert.Equal(t, tt.expected, addresses)
		})
	}
}

func TestSyncNodeLabels(t *testing.T) {
	t.Setenv("TALOSCONFIG", "../../hack/talosconfig")

	cfg := cloudConfig{Global: cloudConfigGlobal{
		ClusterName: "test-cluster",
		Endpoints:   []string{"127.0.0.1"},
	}}
	ctx := context.Background()
	nodes := &v1.NodeList{
		Items: []v1.Node{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
			},
		},
	}

	client, err := newClient(ctx, &cfg)
	assert.NoError(t, err)

	client.kclient = fake.NewSimpleClientset(nodes)

	for _, tt := range []struct {
		name          string
		node          *v1.Node
		meta          *runtime.PlatformMetadataSpec
		expectedError error
		expectedNode  *v1.Node
	}{
		{
			name: "node has no metadata",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
			},
			meta:          &runtime.PlatformMetadataSpec{},
			expectedError: nil,
			expectedNode: &v1.Node{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						ClusterNameNodeLabel: "test-cluster",
					},
				},
			},
		},
		{
			name: "node with platform name",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
			},
			meta: &runtime.PlatformMetadataSpec{
				Platform: "metal",
				Hostname: "node1",
			},
			expectedError: nil,
			expectedNode: &v1.Node{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						ClusterNameNodeLabel:     "test-cluster",
						ClusterNodePlatformLabel: "metal",
					},
				},
			},
		},
		{
			name: "spot node",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
			},
			meta: &runtime.PlatformMetadataSpec{
				Platform: "metal",
				Hostname: "node1",
				Spot:     true,
			},
			expectedError: nil,
			expectedNode: &v1.Node{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						ClusterNameNodeLabel:      "test-cluster",
						ClusterNodePlatformLabel:  "metal",
						ClusterNodeLifeCycleLabel: "spot",
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := syncNodeLabels(client, tt.node, tt.meta)

			assert.Equal(t, tt.expectedError, err)

			node, err := client.kclient.CoreV1().Nodes().Get(ctx, tt.node.Name, metav1.GetOptions{})
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedNode, node)
		})
	}
}

func TestCsrNodeChecks(t *testing.T) {
	ctx := context.Background()
	nodes := &v1.NodeList{
		Items: []v1.Node{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
					Annotations: map[string]string{
						cloudproviderapi.AnnotationAlphaProvidedIPAddr: "1.2.3.4",
					},
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-int",
					Annotations: map[string]string{
						cloudproviderapi.AnnotationAlphaProvidedIPAddr: "1.2.3.4",
					},
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "1.2.3.4",
						},
					},
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-int-ext",
					Annotations: map[string]string{
						cloudproviderapi.AnnotationAlphaProvidedIPAddr: "1.2.3.4",
					},
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "1.2.3.4",
						},
						{
							Type:    v1.NodeExternalIP,
							Address: "2000::1",
						},
					},
				},
			},
		},
	}

	for _, tt := range []struct {
		name          string
		cert          *x509.CertificateRequest
		expectedError error
		expected      bool
	}{
		{
			name: "fake node",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node-non-existing"},
			},
			expectedError: fmt.Errorf("failed to get node node-non-existing: nodes \"node-non-existing\" not found"),
			expected:      false,
		},
		{
			name: "empty node",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node1"},
			},
			expectedError: nil,
			expected:      true,
		},
		{
			name: "empty node",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node2"},
			},
			expectedError: nil,
			expected:      true,
		},
		{
			name: "node with IP",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node2"},
				IPAddresses: []net.IP{
					net.ParseIP("1.2.3.4"),
				},
			},
			expectedError: nil,
			expected:      true,
		},
		{
			name: "node with fake IPs",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node2"},
				IPAddresses: []net.IP{
					net.ParseIP("1.2.3.4"),
					net.ParseIP("2000::1"),
				},
			},
			expectedError: nil,
			expected:      false,
		},
		{
			name: "node with node-IP",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node-int"},
				IPAddresses: []net.IP{
					net.ParseIP("1.2.3.4"),
				},
			},
			expectedError: nil,
			expected:      true,
		},
		{
			name: "node with node-IPs",
			cert: &x509.CertificateRequest{
				DNSNames: []string{"node-int-ext"},
				IPAddresses: []net.IP{
					net.ParseIP("1.2.3.4"),
					net.ParseIP("2000::1"),
				},
			},
			expectedError: nil,
			expected:      true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kclient := fake.NewSimpleClientset(nodes)
			approve, err := csrNodeChecks(ctx, kclient, tt.cert)

			if tt.expectedError != nil {
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.Equal(t, tt.expected, approve)
			}
		})
	}
}
