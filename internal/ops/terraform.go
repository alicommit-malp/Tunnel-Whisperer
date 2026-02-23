package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunTerraform executes a terraform command in dir with the given env vars.
// Progress events are emitted via the callback if non-nil.
func (o *Ops) RunTerraform(ctx context.Context, dir string, env map[string]string, progress ProgressFunc, args ...string) error {
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Capture combined output so we can report errors.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("terraform %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

// TerraformOutput reads a single output value from a Terraform state.
func (o *Ops) TerraformOutput(dir string, env map[string]string, name string) (string, error) {
	cmd := exec.Command("terraform", "output", "-raw", name)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TerraformAvailable returns true if terraform is on the PATH.
func TerraformAvailable() bool {
	_, err := exec.LookPath("terraform")
	return err == nil
}
