package commands

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Manage cluster nodes",
	Long:  `Register, list, inspect, drain, and remove compute nodes in the GoShip cluster.`,
}

// Flags for node register.
var (
	nodeEndpoint string
	nodeLabels   []string
)

var nodeRegisterCmd = &cobra.Command{
	Use:   "register <hostname>",
	Short: "Register a new node",
	Args:  cobra.ExactArgs(1),
	RunE:  runNodeRegister,
}

var nodeListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all nodes",
	Aliases: []string{"ls"},
	RunE:    runNodeList,
}

var nodeInfoCmd = &cobra.Command{
	Use:   "info <hostname>",
	Short: "Show node details",
	Args:  cobra.ExactArgs(1),
	RunE:  runNodeInfo,
}

var nodeRemoveCmd = &cobra.Command{
	Use:     "remove <hostname>",
	Short:   "Remove a node from the cluster",
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(1),
	RunE:    runNodeRemove,
}

var nodeDrainCmd = &cobra.Command{
	Use:   "drain <hostname>",
	Short: "Drain a node (mark as draining)",
	Args:  cobra.ExactArgs(1),
	RunE:  runNodeDrain,
}

func init() {
	nodeRegisterCmd.Flags().StringVar(&nodeEndpoint, "endpoint", "", "Node agent endpoint (ip:port)")
	nodeRegisterCmd.Flags().StringSliceVar(&nodeLabels, "label", nil, "Labels (key=value, repeatable)")

	nodeCmd.AddCommand(nodeRegisterCmd)
	nodeCmd.AddCommand(nodeListCmd)
	nodeCmd.AddCommand(nodeInfoCmd)
	nodeCmd.AddCommand(nodeRemoveCmd)
	nodeCmd.AddCommand(nodeDrainCmd)
}

func parseLabels(raw []string) map[string]string {
	labels := make(map[string]string)
	const kvPairLen = 2
	for _, l := range raw {
		parts := strings.SplitN(l, "=", kvPairLen)
		if len(parts) == kvPairLen {
			labels[parts[0]] = parts[1]
		}
	}
	return labels
}

func runNodeRegister(cmd *cobra.Command, args []string) error {
	hostname := args[0]
	labels := parseLabels(nodeLabels)

	if apiClient != nil {
		return runNodeRegisterAPI(cmd, hostname, labels)
	}

	// Check for duplicate hostname.
	existing := store.ListNodes()
	for _, n := range existing {
		if n.Hostname == hostname {
			return fmt.Errorf("node already exists: %s", hostname)
		}
	}

	now := time.Now()
	node := &entities.Node{
		ID:            uuid.New().String(),
		Hostname:      hostname,
		Endpoint:      nodeEndpoint,
		Status:        entities.NodeStatusOnline,
		Labels:        labels,
		LastHeartbeat: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := store.SetNode(node); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Node '%s' registered successfully\n", hostname)
	fmt.Fprintf(out, "  ID:       %s\n", node.ID)
	fmt.Fprintf(out, "  Endpoint: %s\n", node.Endpoint)
	fmt.Fprintf(out, "  Status:   %s\n", node.Status)

	return nil
}

func runNodeRegisterAPI(cmd *cobra.Command, hostname string, labels map[string]string) error {
	req := apiserver.RegisterNodeRequest{
		Hostname: hostname,
		Endpoint: nodeEndpoint,
		Labels:   labels,
	}

	node, err := apiClient.RegisterNode(req)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Node '%s' registered successfully\n", hostname)
	fmt.Fprintf(out, "  ID:       %s\n", node.ID)
	fmt.Fprintf(out, "  Endpoint: %s\n", node.Endpoint)
	fmt.Fprintf(out, "  Status:   %s\n", node.Status)

	return nil
}

func runNodeList(cmd *cobra.Command, _ []string) error {
	if apiClient != nil {
		return runNodeListAPI(cmd)
	}

	nodes := store.ListNodes()
	return printNodeList(cmd, nodes)
}

func runNodeListAPI(cmd *cobra.Command) error {
	nodes, err := apiClient.ListNodes()
	if err != nil {
		return err
	}
	return printNodeList(cmd, nodes)
}

func printNodeList(cmd *cobra.Command, nodes []*entities.Node) error {
	if len(nodes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No nodes found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "HOSTNAME\tID\tSTATUS\tENDPOINT\tLAST HEARTBEAT")

	const shortIDLen = 8
	for _, n := range nodes {
		shortID := n.ID
		if len(shortID) > shortIDLen {
			shortID = shortID[:shortIDLen]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			n.Hostname,
			shortID,
			n.Status,
			n.Endpoint,
			n.LastHeartbeat.Format("2006-01-02 15:04"),
		)
	}

	return w.Flush()
}

func runNodeInfo(cmd *cobra.Command, args []string) error {
	idOrHostname := args[0]

	if apiClient != nil {
		return runNodeInfoAPI(cmd, idOrHostname)
	}

	node, err := store.GetNode(idOrHostname)
	if err != nil {
		return err
	}

	printNodeInfo(cmd, node)
	return nil
}

func runNodeInfoAPI(cmd *cobra.Command, idOrHostname string) error {
	node, err := apiClient.GetNode(idOrHostname)
	if err != nil {
		return err
	}

	printNodeInfo(cmd, node)
	return nil
}

func printNodeInfo(cmd *cobra.Command, node *entities.Node) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Hostname:       %s\n", node.Hostname)
	fmt.Fprintf(out, "ID:             %s\n", node.ID)
	fmt.Fprintf(out, "Status:         %s\n", node.Status)
	fmt.Fprintf(out, "Endpoint:       %s\n", node.Endpoint)
	if node.Resources.CPU > 0 || node.Resources.MemoryMB > 0 {
		fmt.Fprintf(out, "CPU:            %.0f\n", node.Resources.CPU)
		fmt.Fprintf(out, "Memory:         %dMB\n", node.Resources.MemoryMB)
	}
	if len(node.Labels) > 0 {
		fmt.Fprint(out, "Labels:\n")
		for k, v := range node.Labels {
			fmt.Fprintf(out, "  %s=%s\n", k, v)
		}
	}
	fmt.Fprintf(out, "Last Heartbeat: %s\n", node.LastHeartbeat.Format(time.RFC3339))
	fmt.Fprintf(out, "Created:        %s\n", node.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "Updated:        %s\n", node.UpdatedAt.Format(time.RFC3339))
}

func runNodeRemove(cmd *cobra.Command, args []string) error {
	idOrHostname := args[0]

	if apiClient != nil {
		if err := apiClient.DeleteNode(idOrHostname); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Node '%s' removed\n", idOrHostname)
		return nil
	}

	if err := store.DeleteNode(idOrHostname); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Node '%s' removed\n", idOrHostname)
	return nil
}

func runNodeDrain(cmd *cobra.Command, args []string) error {
	idOrHostname := args[0]

	if apiClient != nil {
		if err := apiClient.DrainNode(idOrHostname); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Node '%s' is now draining\n", idOrHostname)
		return nil
	}

	node, err := store.GetNode(idOrHostname)
	if err != nil {
		return err
	}

	node.Status = entities.NodeStatusDraining
	node.UpdatedAt = time.Now()

	if err := store.SetNode(node); err != nil {
		return fmt.Errorf("failed to drain node: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Node '%s' is now draining\n", idOrHostname)
	return nil
}
