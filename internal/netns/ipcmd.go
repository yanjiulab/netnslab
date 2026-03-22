package netns

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// runIP executes the `ip` command with the provided arguments in the host namespace.
func runIP(args ...string) error {
	return runCommand("ip", args...)
}

// runInNetns executes a command inside the given network namespace using `ip netns exec`.
func runInNetns(namespace string, command string, args ...string) error {
	allArgs := append([]string{"netns", "exec", namespace, command}, args...)
	return runCommand("ip", allArgs...)
}

func runCommand(bin string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("%s %s failed: %v: %s", bin, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s failed: %w", bin, strings.Join(args, " "), err)
	}
	return nil
}

