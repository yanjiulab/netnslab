package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yourname/netnslab/internal/cli"
)

// version is the release version (override at build time with -ldflags "-X main.version=...").
var version = "0.1.0"

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "netnslab",
		Short:   "netnslab is a lightweight Linux netns based network lab tool",
		Version: version,
	}

	cmd.PersistentFlags().Bool("debug", false, "enable debug logging")
	_ = viper.BindPFlag("debug", cmd.PersistentFlags().Lookup("debug"))

	cmd.AddCommand(
		cli.NewDeployCommand(),
		cli.NewDestroyCommand(),
		cli.NewExecCommand(),
		cli.NewEnterCommand(),
		cli.NewListCommand(),
		cli.NewShowCommand(),
		cli.NewCaptureCommand(),
		cli.NewGraphCommand(),
	)

	return cmd
}

func main() {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

