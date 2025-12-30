package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	// Root: shieldctl
	rootCmd := &cobra.Command{
		Use:   "shieldctl",
		Short: "ShieldX Platform CLI",
	}

	// Parent: shieldctl tenant
	tenantCmd := &cobra.Command{
		Use:   "tenant",
		Short: "Tenant operations",
	}

	// Flags for: shieldctl tenant create
	var owners string
	var tier string

	// Leaf: shieldctl tenant create TENANT_NAME
	tenantCreateCmd := &cobra.Command{
		Use: "create TENANT_NAME",
		// Short: "Create a new tenant",
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
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

			fmt.Println("Parsed result:")
			fmt.Println("tenant:", tenantName)
			fmt.Println("owners:", ownerList)
			fmt.Println("tier  :", tier)
		},
	}

	tenantCreateCmd.Flags().StringVar(&owners, "owners", "", "Tenant owners (comma separated)")
	tenantCreateCmd.Flags().StringVar(&tier, "tier", "bronze", "Tenant tier (bronze|silver|gold)")
	_ = tenantCreateCmd.MarkFlagRequired("owners")

	// Wire tree
	tenantCmd.AddCommand(tenantCreateCmd)
	rootCmd.AddCommand(tenantCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
