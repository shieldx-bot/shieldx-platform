package main

import (
	"fmt"

	platformv1alpha1 "github.com/shieldx-bot/shieldx-platform/api/v1alpha1"
	"github.com/shieldx-bot/shieldx-platform/internal/webhook/k8s"
)

func main() {
	name := "payment-team"
	tier := "basic"
	isolation := "namespace"
	owners := []string{"admin@example.com", "client@example.com"}

	resourceQuota := platformv1alpha1.ResourceQuota{
		RequestsCPU:     "500m",
		RequestsMemory:  "512Mi",
		LimitsCPU:       "1",
		LimitsMemory:    "1Gi",
		RequestsStorage: "1Gi",
		Pods:            "10",
	}

	networkPolicy := platformv1alpha1.NetworkPolicy{
		PodSelector: map[string]string{
			"app":    "backend",
			"tenant": "payment",
		},
		PolicyTypes: []string{"Ingress", "Egress"},
		Ingress: []platformv1alpha1.NetworkPolicyIngressRule{
			{
				From: platformv1alpha1.NetworkPolicyPeer{
					Pod: map[string]string{
						"app":    "frontend",
						"tenant": "payment",
					},
				},
			},
		},
		Egress: []platformv1alpha1.NetworkPolicyEgressRule{
			{
				To: platformv1alpha1.NetworkPolicyPeer{
					Pod: map[string]string{
						"app":    "db",
						"tenant": "payment",
					},
				},
			},
		},
	}

	fmt.Printf("Demo NetworkPolicy input: %+v\n", networkPolicy)

	if err := k8s.CreateReconciliation(name, tier, isolation, owners, resourceQuota, networkPolicy); err != nil {
		fmt.Printf("Reconciliation failed: %v\n", err)
		return
	}

	fmt.Println("Reconciliation succeeded")
}
