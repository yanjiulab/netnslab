package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/config"
	"github.com/yanjiulab/netnslab/internal/logx"
)

func NewValidateCommand() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate -f FILE",
		Short: "Validate a lab topology YAML without deploying",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			if file == "" {
				return fmt.Errorf("config file must be specified with -f")
			}

			_, err := config.LoadConfig(file)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "OK")
				return nil
			}

			var me *config.MultiError
			if errors.As(err, &me) && me != nil {
				for _, e := range me.Errors {
					var fe *config.FieldError
					if errors.As(e, &fe) && fe != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", fe.Field, fe.Message)
						continue
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%v\n", e)
				}
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), err.Error())
			return err
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "YAML topology file")
	return cmd
}
