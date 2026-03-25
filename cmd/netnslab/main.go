package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/cli"
)

// Version is the release version (override at build time: build.sh or -ldflags "-X main.Version=...").
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	BuiltBy = "unknown"
)

// String returns a human-friendly version string.
func VersionString() string {
	return fmt.Sprintf(
		"%s (commit=%s, date=%s, builtBy=%s, go=%s)",
		Version,
		Commit,
		Date,
		BuiltBy,
		runtime.Version(),
	)
}

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "netnslab",
		Short:   "netnslab is a lightweight Linux netns based network lab tool",
		Version: VersionString(),
	}

	cmd.PersistentFlags().Bool("debug", false, "enable debug logging")
	_ = viper.BindPFlag("debug", cmd.PersistentFlags().Lookup("debug"))

	cmd.AddCommand(
		cli.NewDeployCommand(),
		cli.NewValidateCommand(),
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
