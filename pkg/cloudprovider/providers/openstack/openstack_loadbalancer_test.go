// +build integration

package openstack

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/openstack/clientconfig"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestEnsureLoadBalancer(t *testing.T) {
	ao, err := clientconfig.AuthOptions(nil)
	if err != nil {
		t.Skipf("unable to authenticate to OpenStack: %v", err)
	}

	provider, err := openstack.AuthenticatedClient(*ao)
	if err != nil {
		t.Skipf("unable to get AuthenticatedClient: %v", err)
	}

	auth := provider.GetAuthResult()
	v3auth, ok := auth.(tokens.CreateResult)
	if !ok {
		t.Skip("keystone does not support v3")
	}
	catalog, err := v3auth.ExtractServiceCatalog()
	if err != nil {
		t.Skipf("unable to find Service Catalog: %v", err)
	}

	isOctavia := func() bool {
		for _, entry := range catalog.Entries {
			if entry.Type == "load-balancer" {
				return true
			}
		}
		return false
	}()
	if !isOctavia {
		t.Skip("this test only supports octavia")
	}

	netClient, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		t.Skipf("unable to create network client: %v", err)
	}
	computeClient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		t.Skipf("unable to create compute client: %v", err)
	}
	lbClient, err := openstack.NewLoadBalancerV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		t.Skipf("unable to create lb client: %v", err)
	}

	lb := LbaasV2{
		LoadBalancer: LoadBalancer{
			network: netClient,
			compute: computeClient,
			lb:      lbClient,
			opts: LoadBalancerOpts{
				LBVersion:         "v2",
				UseOctavia:        true,
				SubnetID:          os.Getenv("LB_TEST_SUBNET_ID"),
				NetworkID:         os.Getenv("LB_TEST_NETWORK_ID"),
				FloatingNetworkID: os.Getenv("LB_TEST_FLOATING_NETWORK_ID"),
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	nodes := []*corev1.Node{
		&corev1.Node{
			Status: corev1.NodeStatus{
				Phase: corev1.NodeRunning,
				Conditions: []corev1.NodeCondition{
					corev1.NodeCondition{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
				Addresses: []corev1.NodeAddress{
					corev1.NodeAddress{
						Type:    corev1.NodeInternalIP,
						Address: "10.250.240.1",
					},
				},
			},
		},
	}

	testSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-loadbalancer",
		},
		Spec: corev1.ServiceSpec{
			SessionAffinity: corev1.ServiceAffinityNone,
			Type:            corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Name:       "port1",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					NodePort:   31111,
				},
				corev1.ServicePort{
					Name:       "dns",
					Protocol:   corev1.ProtocolUDP,
					Port:       53,
					TargetPort: intstr.FromInt(9053),
					NodePort:   32053,
				},
			},
		},
	}
	res, err := lb.EnsureLoadBalancer(
		ctx,
		"testing",
		&testSvc,
		nodes,
	)
	if err != nil {
		t.Fatalf("unable to create loadbalancer: %v", err)
	}

	fmt.Printf("%#v\n", res)
}
