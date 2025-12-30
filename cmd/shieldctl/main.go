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
	var test string
	var lop string

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
			lopListRaw := strings.Split(lop, ",")
			lopList := make([]string, 0, len(lopListRaw))
			for _, l := range lopListRaw {
				l = strings.TrimSpace(l)
				if l == "" {
					continue
				}
				lopList = append(lopList, l)
			}
			fmt.Println("Parsed result:")
			fmt.Println("tenant:", tenantName)
			fmt.Println("owners:", len(ownerList), ownerList)
			fmt.Println("tier  :", tier)
			fmt.Println("test: ", test)
			fmt.Println("lop  : ", len(lopList), lopList)
		},
	}

	tenantCreateCmd.Flags().StringVar(&owners, "owners", "", "Tenant owners (comma separated)")
	tenantCreateCmd.Flags().StringVar(&tier, "tier", "bronze", "Tenant tier (bronze|silver|gold)")
	tenantCreateCmd.Flags().StringVar(&test, "test", "", "A test flag")
	tenantCreateCmd.Flags().StringVar(&lop, "lop", "", "A lop flag")
	_ = tenantCreateCmd.MarkFlagRequired("owners")

	// Wire tree
	tenantCmd.AddCommand(tenantCreateCmd)
	rootCmd.AddCommand(tenantCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
