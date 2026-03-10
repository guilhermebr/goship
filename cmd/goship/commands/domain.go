package commands

import (
	"fmt"
	"slices"
	"text/tabwriter"

	"github.com/spf13/cobra"

	apiserver "github.com/guilhermebr/goship/internal/api"
)

var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Manage project domains for reverse proxy routing",
	Long: `Set, list, and remove domains assigned to a project.
Domains are used by the reverse proxy to route HTTP traffic to VMs.`,
}

var domainDefaultFlag string

var domainSetCmd = &cobra.Command{
	Use:   "set <project> <domain> [domain...]",
	Short: "Set domains for a project (replaces all existing domains)",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runDomainSet,
}

var domainListCmd = &cobra.Command{
	Use:     "list <project>",
	Short:   "List domains assigned to a project",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(1),
	RunE:    runDomainList,
}

var domainRemoveCmd = &cobra.Command{
	Use:     "remove <project> <domain> [domain...]",
	Short:   "Remove specific domains from a project",
	Aliases: []string{"rm"},
	Args:    cobra.MinimumNArgs(2),
	RunE:    runDomainRemove,
}

func init() {
	domainSetCmd.Flags().StringVar(&domainDefaultFlag, "default", "", "Override which domain is the default")

	domainCmd.AddCommand(domainSetCmd)
	domainCmd.AddCommand(domainListCmd)
	domainCmd.AddCommand(domainRemoveCmd)
}

func runDomainSet(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	domains := args[1:]

	defaultDomain := domainDefaultFlag
	if defaultDomain == "" && len(domains) > 0 {
		defaultDomain = domains[0]
	}

	if apiClient != nil {
		_, err := apiClient.UpdateDomains(projectName, apiserver.UpdateDomainsRequest{
			Domains:       domains,
			DefaultDomain: defaultDomain,
		})
		if err != nil {
			return err
		}
		printDomainSummary(cmd, projectName, domains, defaultDomain)
		return nil
	}

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	project.Domains = domains
	project.DefaultDomain = defaultDomain

	if err := store.UpdateProject(project); err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	printDomainSummary(cmd, projectName, domains, defaultDomain)
	return nil
}

func runDomainList(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	var domains []string
	var defaultDomain string

	if apiClient != nil {
		resp, err := apiClient.GetProject(projectName)
		if err != nil {
			return err
		}
		domains = resp.Domains
		defaultDomain = resp.DefaultDomain
	} else {
		project, err := store.GetProject(projectName)
		if err != nil {
			return fmt.Errorf("project not found: %s", projectName)
		}
		domains = project.Domains
		defaultDomain = project.DefaultDomain
	}

	out := cmd.OutOrStdout()

	if len(domains) == 0 {
		fmt.Fprintf(out, "No domains configured for project '%s'.\n", projectName)
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DOMAIN\tDEFAULT")
	for _, d := range domains {
		def := ""
		if d == defaultDomain {
			def = "*"
		}
		fmt.Fprintf(w, "%s\t%s\n", d, def)
	}
	return w.Flush()
}

func runDomainRemove(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	toRemove := args[1:]

	if apiClient != nil {
		resp, err := apiClient.GetProject(projectName)
		if err != nil {
			return err
		}

		remaining := filterDomains(resp.Domains, toRemove)
		defaultDomain := resp.DefaultDomain
		if slices.Contains(toRemove, defaultDomain) && len(remaining) > 0 {
			defaultDomain = remaining[0]
		}

		_, err = apiClient.UpdateDomains(projectName, apiserver.UpdateDomainsRequest{
			Domains:       remaining,
			DefaultDomain: defaultDomain,
		})
		if err != nil {
			return err
		}

		printRemoveSummary(cmd, projectName, toRemove)
		return nil
	}

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	remaining := filterDomains(project.Domains, toRemove)
	project.Domains = remaining

	if slices.Contains(toRemove, project.DefaultDomain) {
		if len(remaining) > 0 {
			project.DefaultDomain = remaining[0]
		} else {
			project.DefaultDomain = ""
		}
	}

	if err := store.UpdateProject(project); err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	printRemoveSummary(cmd, projectName, toRemove)
	return nil
}

// filterDomains returns domains that are not in the remove list.
func filterDomains(domains, remove []string) []string {
	var result []string
	for _, d := range domains {
		if !slices.Contains(remove, d) {
			result = append(result, d)
		}
	}
	return result
}

func printDomainSummary(cmd *cobra.Command, project string, domains []string, defaultDomain string) {
	out := cmd.OutOrStdout()
	for _, d := range domains {
		if d == defaultDomain {
			fmt.Fprintf(out, "  %s (default)\n", d)
		} else {
			fmt.Fprintf(out, "  %s\n", d)
		}
	}
	fmt.Fprintf(out, "Domains updated for project '%s'\n", project)
}

func printRemoveSummary(cmd *cobra.Command, project string, removed []string) {
	out := cmd.OutOrStdout()
	for _, d := range removed {
		fmt.Fprintf(out, "  %s: removed\n", d)
	}
	fmt.Fprintf(out, "Domains updated for project '%s'\n", project)
}
