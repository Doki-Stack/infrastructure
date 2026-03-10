package runner

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Result struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
}

func (r Result) Success() bool {
	return r.ExitCode == 0
}

func Run(name string, args ...string) Result {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return Result{
		Command:  name + " " + strings.Join(args, " "),
		ExitCode: exitCode,
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
	}
}

func RunShell(command string) Result {
	return Run("bash", "-c", command)
}

func RunStream(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func RequireCommands(names ...string) error {
	var missing []string
	for _, n := range names {
		if !CommandExists(n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required commands not found: %s", strings.Join(missing, ", "))
	}
	return nil
}
