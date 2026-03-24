package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/config"
	"github.com/yanjiulab/netnslab/internal/labstate"
	"github.com/yanjiulab/netnslab/internal/logx"
	"github.com/yanjiulab/netnslab/internal/netns"
)

func NewListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List deployed labs",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			entries, err := os.ReadDir(netns.RunBaseDir())
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read labs dir: %w", err)
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No labs found.")
				return nil
			}

			var labs []string
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				labs = append(labs, e.Name())
			}
			sort.Strings(labs)

			for _, lab := range labs {
				nodesDir := filepath.Join(netns.RunBaseDir(), lab)
				nodeEntries, _ := os.ReadDir(nodesDir)
				nNodeDirs := 0
				for _, ne := range nodeEntries {
					if ne.IsDir() {
						nNodeDirs++
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t(nodes: %d)\n", lab, nNodeDirs)
			}

			return nil
		},
	}
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func NewShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <lab>",
		Short: "Show runtime state of a deployed lab (live IPs from netns)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			labName := args[0]
			statePath := netns.LabStatePath(labName)
			st, err := labstate.Load(statePath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("lab %q not found or not deployed (missing %s)", labName, statePath)
				}
				return fmt.Errorf("load lab state: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Lab: %s\n", st.Name)
			fmt.Fprintf(out, "Routing auto_static: %v\n", st.Routing.AutoStatic)
			fmt.Fprintf(out, "Mgmt: enabled=%v", st.Mgmt.Enable)
			if st.Mgmt.IPv4 != "" {
				fmt.Fprintf(out, ", subnet=%s", st.Mgmt.IPv4)
			}
			fmt.Fprintln(out)

			var nodeNames []string
			for n := range st.Nodes {
				nodeNames = append(nodeNames, n)
			}
			sort.Strings(nodeNames)

			fmt.Fprintf(out, "Nodes (%d):\n", len(nodeNames))
			for _, name := range nodeNames {
				kind := st.Nodes[name]
				mgmtIP := netns.QueryIfaceIPv4(labName, name, netns.MgmtIfaceName)
				fmt.Fprintf(out, "  %s\t%s\tmgmt_ip=%s\n", name, kind, dash(mgmtIP))
			}

			fmt.Fprintf(out, "Links (%d):\n", len(st.Links))
			for i, l := range st.Links {
				n0, i0 := config.SplitEndpointPublic(l.Endpoints[0])
				n1, i1 := config.SplitEndpointPublic(l.Endpoints[1])
				ip0 := netns.QueryIfaceIPv4(labName, n0, i0)
				ip1 := netns.QueryIfaceIPv4(labName, n1, i1)
				netemStr := "-"
				if l.Netem != nil {
					netemStr = l.Netem.NetemSummary()
				}
				fmt.Fprintf(out, "  %d\t%s\t%s\t%s\t%s\tnetem=%s\n",
					i, l.Endpoints[0], dash(ip0), l.Endpoints[1], dash(ip1), netemStr)
			}

			return nil
		},
	}
	return cmd
}
