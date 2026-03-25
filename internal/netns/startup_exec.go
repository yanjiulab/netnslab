package netns

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunStartupExec runs topology node exec script inside the namespace (after links/routes).
// Empty or whitespace-only script is a no-op.
func RunStartupExec(labName, nodeName string, script string, env map[string]string) error {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil
	}
	nsName := NamespaceName(labName, nodeName)
	allArgs := []string{"netns", "exec", nsName, "bash", "-c", script}
	cmd := exec.CommandContext(context.Background(), "ip", allArgs...)
	cmd.Env = MergeEnviron(os.Environ(), env)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("node %q exec: %w: %s", nodeName, err, msg)
		}
		return fmt.Errorf("node %q exec: %w", nodeName, err)
	}
	return nil
}
