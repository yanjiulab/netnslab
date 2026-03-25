package cli

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/labstate"
	"github.com/yanjiulab/netnslab/internal/logx"
	"github.com/yanjiulab/netnslab/internal/netns"
)

func NewExecCommand() *cobra.Command {
	var allNodes bool
	var kindFilter string

	cmd := &cobra.Command{
		Use:   "exec <lab> [node] -- <command> [args...]",
		Short: "Execute a command inside a node namespace",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			rest := cmd.ArgsLenAtDash()
			if rest < 0 {
				return fmt.Errorf("command must follow -- (example: netnslab exec mylab r1 -- ip addr)")
			}
			if rest < 1 {
				return fmt.Errorf("need <lab> before --")
			}

			labName := args[0]
			commandArgs := args[rest:]
			if len(commandArgs) == 0 {
				return fmt.Errorf("no command specified after --")
			}

			kindFilter = strings.TrimSpace(kindFilter)
			batchMode := allNodes || kindFilter != ""
			if !batchMode {
				if rest < 2 {
					return fmt.Errorf("need <lab> and <node> before --")
				}
				nodeName := args[1]
				return runExecSingleNodeInteractive(labName, nodeName, commandArgs)
			}

			if rest != 1 {
				return fmt.Errorf("batch exec mode expects only <lab> before --")
			}
			return runExecBatch(cmd, labName, kindFilter, commandArgs)
		},
	}

	cmd.Flags().BoolVar(&allNodes, "all", false, "execute command on all nodes in the lab")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "execute command on nodes of a specific kind (host|router|bridge)")

	return cmd
}

func runExecSingleNodeInteractive(labName, nodeName string, commandArgs []string) error {
	nsName := netns.NamespaceName(labName, nodeName)
	ipArgs := append([]string{"netns", "exec", nsName}, commandArgs...)

	c := exec.Command("ip", ipArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	nodeEnv, err := netns.ReadNodeEnvFile(labName, nodeName)
	if err != nil {
		return fmt.Errorf("read node env: %w", err)
	}
	c.Env = netns.MergeEnviron(os.Environ(), nodeEnv)
	return c.Run()
}

func runExecBatch(cmd *cobra.Command, labName, kindFilter string, commandArgs []string) error {
	st, err := labstate.Load(netns.LabStatePath(labName))
	if err != nil {
		return fmt.Errorf("load lab state for %q: %w", labName, err)
	}

	var nodeNames []string
	for node, kind := range st.Nodes {
		if kindFilter != "" && kind != kindFilter {
			continue
		}
		nodeNames = append(nodeNames, node)
	}
	sort.Strings(nodeNames)
	if len(nodeNames) == 0 {
		if kindFilter != "" {
			return fmt.Errorf("no nodes match kind=%q in lab %q", kindFilter, labName)
		}
		return fmt.Errorf("no nodes found in lab %q", labName)
	}

	failures := 0
	for _, nodeName := range nodeNames {
		fmt.Fprintf(cmd.OutOrStdout(), "=== %s ===\n", nodeName)
		nsName := netns.NamespaceName(labName, nodeName)
		ipArgs := append([]string{"netns", "exec", nsName}, commandArgs...)
		c := exec.Command("ip", ipArgs...)
		nodeEnv, envErr := netns.ReadNodeEnvFile(labName, nodeName)
		if envErr != nil {
			failures++
			fmt.Fprintf(cmd.ErrOrStderr(), "[%s] read node env: %v\n", nodeName, envErr)
			continue
		}
		c.Env = netns.MergeEnviron(os.Environ(), nodeEnv)
		out, runErr := c.CombinedOutput()
		if len(out) > 0 {
			_, _ = cmd.OutOrStdout().Write(out)
		}
		if runErr != nil {
			failures++
			fmt.Fprintf(cmd.ErrOrStderr(), "[%s] command failed: %v\n", nodeName, runErr)
		}
	}

	if failures > 0 {
		return fmt.Errorf("batch exec finished with %d/%d failures", failures, len(nodeNames))
	}
	return nil
}

func NewEnterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enter <lab> <node>",
		Short: "Enter an interactive shell inside a node namespace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			labName := args[0]
			nodeName := args[1]
			nsName := netns.NamespaceName(labName, nodeName)

			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/bash"
			}

			prompt := fmt.Sprintf("netnslab-%s:/# ", nodeName)
			nodeEnv, err := netns.ReadNodeEnvFile(labName, nodeName)
			if err != nil {
				return fmt.Errorf("read node env: %w", err)
			}
			env := netns.MergeEnviron(os.Environ(), nodeEnv)
			env = append(env, "PS1="+prompt)

			ipArgs := []string{"netns", "exec", nsName, shell}
			c := exec.Command("ip", ipArgs...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.Env = env

			return c.Run()
		},
	}

	return cmd
}
