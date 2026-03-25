package cli

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/config"
	"github.com/yanjiulab/netnslab/internal/labstate"
	"github.com/yanjiulab/netnslab/internal/logx"
	"github.com/yanjiulab/netnslab/internal/netns"
)

func NewCaptureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capture <lab> <node> <ifname>",
		Short: "Capture packets on a node interface using tcpdump",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			labName := args[0]
			nodeName := args[1]
			ifName := args[2]

			nsName := netns.NamespaceName(labName, nodeName)
			pcapPath := fmt.Sprintf("%s-%s.pcap", nodeName, ifName)
			pcapPath = netns.RunDir(labName, nodeName) + "/" + pcapPath

			// tcpdump -i <if> -w <pcap>
			ipArgs := []string{
				"netns", "exec", nsName,
				"tcpdump", "-U", "-i", ifName, "-w", pcapPath,
			}

			c := exec.Command("ip", ipArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			nodeEnv, err := netns.ReadNodeEnvFile(labName, nodeName)
			if err != nil {
				return fmt.Errorf("read node env: %w", err)
			}
			c.Env = netns.MergeEnviron(os.Environ(), nodeEnv)

			fmt.Fprintf(cmd.OutOrStdout(), "Saving capture to %s\n", pcapPath)
			return c.Run()
		},
	}
	return cmd
}

func graphDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// dotLabel escapes text for use inside a Graphviz double-quoted label.
func dotLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

func NewGraphCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph <lab>",
		Short: "Output Graphviz DOT for a deployed lab (live interface IPs from netns)",
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
			fmt.Fprintf(out, "graph \"%s\" {\n", dotLabel(st.Name))
			fmt.Fprintln(out, "  graph [overlap=false];")
			fmt.Fprintln(out, "  node [shape=box];")

			var nodeNames []string
			for n := range st.Nodes {
				nodeNames = append(nodeNames, n)
			}
			sort.Strings(nodeNames)

			for _, name := range nodeNames {
				kind := st.Nodes[name]
				mgmt := netns.QueryIfaceIPv4(labName, name, netns.MgmtIfaceName)
				lbl := fmt.Sprintf("%s (%s)\nmgmt %s: %s", name, kind, netns.MgmtIfaceName, graphDash(mgmt))
				fmt.Fprintf(out, "  \"%s\" [label=\"%s\"];\n", dotLabel(name), dotLabel(lbl))
			}

			for _, l := range st.Links {
				if len(l.Endpoints) != 2 {
					continue
				}
				ep0, ep1 := l.Endpoints[0], l.Endpoints[1]
				n0, i0 := config.SplitEndpointPublic(ep0)
				n1, i1 := config.SplitEndpointPublic(ep1)
				ip0 := netns.QueryIfaceIPv4(labName, n0, i0)
				ip1 := netns.QueryIfaceIPv4(labName, n1, i1)
				edgeLbl := fmt.Sprintf("%s:%s\n%s | %s:%s\n%s",
					n0, i0, graphDash(ip0), n1, i1, graphDash(ip1))
				fmt.Fprintf(out, "  \"%s\" -- \"%s\" [label=\"%s\"];\n",
					dotLabel(n0), dotLabel(n1), dotLabel(edgeLbl))
			}

			fmt.Fprintln(out, "}")
			return nil
		},
	}

	return cmd
}
