package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yourname/netnslab/internal/logx"
	"github.com/yourname/netnslab/internal/netns"
)

func NewExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <lab> <node> -- <command> [args...]",
		Short: "Execute a command inside a node namespace",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			rest := cmd.ArgsLenAtDash()
			if rest < 0 {
				return fmt.Errorf("command must follow -- (example: netnslab exec mylab r1 -- ip addr)")
			}
			if rest < 2 {
				return fmt.Errorf("need <lab> and <node> before --")
			}

			labName := args[0]
			nodeName := args[1]
			commandArgs := args[rest:]
			if len(commandArgs) == 0 {
				return fmt.Errorf("no command specified after --")
			}

			nsName := netns.NamespaceName(labName, nodeName)
			ipArgs := append([]string{"netns", "exec", nsName}, commandArgs...)

			c := exec.Command("ip", ipArgs...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			return c.Run()
		},
	}

	return cmd
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
			env := os.Environ()
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
