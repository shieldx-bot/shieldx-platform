package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	platformv1alpha1 "github.com/shieldx-bot/shieldx-platform/api/v1alpha1"
	"github.com/shieldx-bot/shieldx-platform/internal/webhook/k8s"

	"github.com/spf13/cobra"
)

type ResourceQuota struct {
	RequestsCPU     string
	RequestsMemory  string
	LimitsCPU       string
	LimitsMemory    string
	RequestsStorage string
	Pods            string
}

func main() {
	// Root: shieldctl
	rootCmd := &cobra.Command{
		Use:   "shieldctl",
		Short: "ShieldX Platform CLI",
	}
	var NameTenant string
	DeleteTenantCmd := &cobra.Command{
		Use:   "delete-tenant TENANT_NAME",
		Short: "Delete a tenant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			NameTenant = args[0]
			fmt.Printf("Deleting tenant: %s\n", NameTenant)
			err := k8s.DeleleteReconciliation(NameTenant)
			if err != nil {
				fmt.Printf("Error deleting tenant: %v\n", err)
				return err
			}
			// Here you would add the logic to delete the tenant
			return nil
		},
	}
	rootCmd.AddCommand(DeleteTenantCmd)

	// Root leaf: shieldctl status
	var statusOutput string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show shieldctl status",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(strings.TrimSpace(statusOutput)) {
			case "", "text":
				fmt.Println("shieldctl: ok")
				return nil
			case "json":
				out := map[string]any{
					"ok":      true,
					"command": "status",
				}
				b, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			default:
				return fmt.Errorf("invalid --output %q (expected text|json)", statusOutput)
			}
		},
	}
	statusCmd.Flags().StringVarP(&statusOutput, "output", "o", "text", "Output format (text|json)")
	rootCmd.AddCommand(statusCmd)

	// Parent: shieldctl tenant
	tenantCmd := &cobra.Command{
		Use:   "tenant",
		Short: "Tenant operations",
	}

	// Flags for: shieldctl tenant create
	var owners string
	var tier string
	var isolation string
	var resourceQuota platformv1alpha1.ResourceQuota
	var networkPolicy platformv1alpha1.NetworkPolicy
	var npIngressFrom []string
	var npEgressTo []string

	// Leaf: shieldctl tenant create TENANT_NAME
	tenantCreateCmd := &cobra.Command{
		Use: "create TENANT_NAME",
		// Short: "Create a new tenant",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tenantName := args[0]

			ownerListRaw := strings.Split(owners, ",")
			ownerList := make([]string, 0, len(ownerListRaw))
			for _, o := range ownerListRaw {
				o = strings.TrimSpace(o)
				if o == "" {
					continue
				}
				ownerList = append(ownerList, o)
			}

			PodSelector := make(map[string]string)
			for k, v := range networkPolicy.PodSelector {
				PodSelector[k] = v
			}

			PolicyTypes := make([]string, 0, len(networkPolicy.PolicyTypes))
			for _, v := range networkPolicy.PolicyTypes {
				PolicyTypes = append(PolicyTypes, v)
			}

			Ingress := make([]platformv1alpha1.NetworkPolicyIngressRule, 0, len(npIngressFrom))
			for _, raw := range npIngressFrom {
				var rule platformv1alpha1.NetworkPolicyIngressRule
				if err := json.Unmarshal([]byte(raw), &rule); err != nil {
					return fmt.Errorf("invalid --np-ingress-from value %q (expected JSON): %w", raw, err)
				}
				Ingress = append(Ingress, rule)
			}

			Egress := make([]platformv1alpha1.NetworkPolicyEgressRule, 0, len(npEgressTo))
			for _, raw := range npEgressTo {
				var rule platformv1alpha1.NetworkPolicyEgressRule
				if err := json.Unmarshal([]byte(raw), &rule); err != nil {
					return fmt.Errorf("invalid --np-egress-to value %q (expected JSON): %w", raw, err)
				}
				Egress = append(Egress, rule)
			}

			fmt.Println("Creating tenant with the following parameters:")

			resourceQuotaSpec := platformv1alpha1.ResourceQuota{
				RequestsCPU:     resourceQuota.RequestsCPU,
				RequestsMemory:  resourceQuota.RequestsMemory,
				LimitsCPU:       resourceQuota.LimitsCPU,
				LimitsMemory:    resourceQuota.LimitsMemory,
				RequestsStorage: resourceQuota.RequestsStorage,
				Pods:            resourceQuota.Pods,
			}
			networkPolicy := platformv1alpha1.NetworkPolicy{
				PodSelector: PodSelector,
				PolicyTypes: PolicyTypes,
				Ingress:     Ingress,
				Egress:      Egress,
			}
			k8s.CreateReconciliation(tenantName, tier, isolation, ownerList, resourceQuotaSpec, networkPolicy)
			fmt.Printf(tenantName, tier, isolation, ownerList, resourceQuotaSpec, networkPolicy)

			return nil
		},
	}

	tenantCreateCmd.Flags().StringVar(&owners, "owners", "", "Tenant owners (comma separated)")
	tenantCreateCmd.Flags().StringVar(&tier, "tier", "bronze", "Tenant tier (bronze|silver|gold)")
	tenantCreateCmd.Flags().StringVar(&isolation, "isolation", "namespace", "Tenant isolation (namespace|cluster)")
	tenantCreateCmd.Flags().StringVar(&resourceQuota.RequestsCPU, "rq-requests-cpu", "", "ResourceQuota requests CPU")
	tenantCreateCmd.Flags().StringVar(&resourceQuota.RequestsMemory, "rq-requests-memory", "", "ResourceQuota requests Memory")
	tenantCreateCmd.Flags().StringVar(&resourceQuota.LimitsCPU, "rq-limits-cpu", "", "ResourceQuota limits CPU")
	tenantCreateCmd.Flags().StringVar(&resourceQuota.LimitsMemory, "rq-limits-memory", "", "ResourceQuota limits Memory")
	tenantCreateCmd.Flags().StringVar(&resourceQuota.RequestsStorage, "rq-requests-storage", "", "ResourceQuota requests Storage")
	tenantCreateCmd.Flags().StringVar(&resourceQuota.Pods, "rq-pods", "", "ResourceQuota pods")
	tenantCreateCmd.Flags().StringToStringVar(&networkPolicy.PodSelector, "np-pod-selector", nil, "NetworkPolicy pod selector (key=value)")
	tenantCreateCmd.Flags().StringSliceVar(&networkPolicy.PolicyTypes, "np-policy-types", nil, "NetworkPolicy policy types (Ingress,Egress)")
	tenantCreateCmd.Flags().StringArrayVar(&npIngressFrom, "np-ingress-from", nil, "NetworkPolicy ingress rules (JSON)")
	tenantCreateCmd.Flags().StringArrayVar(&npEgressTo, "np-egress-to", nil, "NetworkPolicy egress rules (JSON)")

	_ = tenantCreateCmd.MarkFlagRequired("owners")

	// Wire tree
	tenantCmd.AddCommand(tenantCreateCmd)
	rootCmd.AddCommand(tenantCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// apiVersion: platform.shieldx.io/v1alpha1
// kind: Tenant
// metadata:
//   name: payment-team
// spec:
//   tier: basic
//   isolation: namespace
//   owners:
//     - admin@example.com
//     - client@example.com

//   resourceQuota:
//     requestsCPU: 500m
//     requestsMemory: 512Mi
//     limitsCPU: "1"
//     limitsMemory: 1Gi
//     requestsStorage: 1Gi
//     pods: "10"

//   networkPolicy:
//     podSelector:
//       app: backend
//       tenant: payment
//     policyTypes:
//       - Ingress
//       - Egress
//     ingress:
//       - from:
//           pod:
//             app: frontend
//             tenant: payment
//     egress:
//       - to:
//           pod:
//             app: db
//             tenant: payment
