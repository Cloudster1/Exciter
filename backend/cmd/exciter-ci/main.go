// Command exciter-ci is what a pipeline runs against Exciter (#200): results uploads JUnit reports and waits for
// them to land, and check asks the trace check for a verdict. Its exit code is the pipeline's to stop on.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// Exit codes: the check passed, the check failed, or Exciter could not be asked.
const (
	exitPass   = 0
	exitFail   = 1
	exitBroken = 2
)

const usage = `exciter-ci talks to Exciter from a pipeline.

  exciter-ci results [flags] FILE...   upload JUnit XML reports and wait until they land
  exciter-ci check [flags]             ask for the trace check's verdict; exit 1 when it fails

Every command reads EXCITER_URL, EXCITER_TOKEN (a token with the evidence scope) and EXCITER_PROJECT, and takes the
repository, commit, branch and run address from GitHub Actions, GitLab CI or Gitea Actions when they are not given.
Run a command with -h for its flags.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is the command as a function of its arguments and environment, for the tests.
func run(ctx context.Context, args []string, env func(string) string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errOut, usage)
		return exitBroken
	}
	var (
		code int
		err  error
	)
	switch args[0] {
	case "results":
		code, err = results(ctx, args[1:], env, out)
	case "check":
		code, err = check(ctx, args[1:], env, out)
	case "-h", "--help", "help":
		fmt.Fprint(out, usage)
		return exitPass
	default:
		fmt.Fprintf(errOut, "exciter-ci: there is no command %q.\n\n%s", args[0], usage)
		return exitBroken
	}
	if err != nil {
		var asked *refusal
		if errors.As(err, &asked) {
			fmt.Fprintf(errOut, "exciter-ci: Exciter refused: %s\n", asked)
		} else {
			fmt.Fprintf(errOut, "exciter-ci: %v\n", err)
		}
		return exitBroken
	}
	return code
}
