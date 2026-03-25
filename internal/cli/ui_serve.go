package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/logx"
	"github.com/yanjiulab/netnslab/internal/netns"
	"github.com/yanjiulab/netnslab/internal/ui"
)

func NewUIServeCommand() *cobra.Command {
	var (
		addr      string
		labFilter string
	)

	cmd := &cobra.Command{
		Use:   "ui serve",
		Short: "Serve visual topology UI and APIs",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			_ = labFilter
			if addr == "" {
				addr = "127.0.0.1:8080"
			}

			// Pre-check that the runtime base directory exists (best-effort).
			// The server still starts if there are no labs deployed.
			_ = netns.RunBaseDir()

			return ui.Serve(addr, labFilter)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "listen address (host:port)")
	cmd.Flags().StringVar(&labFilter, "lab", "", "optional lab name to pre-filter API/UI")
	return cmd
}
