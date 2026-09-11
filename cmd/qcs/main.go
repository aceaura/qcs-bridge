// qcs is the CLI front-end of the qcs bridge: send a command to the
// CloudShell agent and print its output, or check agent liveness.
//
// Usage:
//
//	qcs status
//	qcs exec [-timeout N] <command...>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"qcs-bridge/internal/client"
	qcsconfig "qcs-bridge/internal/config"
)

const usage = `qcs — Qoder ⇄ CloudShell bridge CLI

Usage:
  qcs status                      Show agent liveness (last heartbeat, identity, uptime)
  qcs exec [-timeout N] <cmd...>  Run a command in CloudShell, print stdout/stderr, exit with remote exit code
  qcs token                       Print the qcs1:<region>:<secret> token to set up another machine

Config (environment, or ~/.qcs/config as KEY=VALUE; env wins):
  QCS_SECRET | QCS_SECRET_FILE | ~/.qcs-secret           Shared secret (required). A
                                                         qcs1:<region>:<secret> token also
                                                         supplies the region.
  AWS_REGION                                             Region of the queues (optional if the
                                                         secret is a token)
  QCS_CMD_QUEUE, QCS_RESULT_QUEUE, QCS_HEARTBEAT_QUEUE   Queue names or URLs (default: qcs-*.fifo)
  AWS_PROFILE / QCS_AWS_PROFILE, ...                     Standard AWS SDK credential chain
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "status":
		b, err := client.New(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "config error:", err)
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := b.Status(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "status error:", err)
			os.Exit(1)
		}
		fmt.Println(out)

	case "exec":
		fs := flag.NewFlagSet("exec", flag.ExitOnError)
		timeout := fs.Int("timeout", 120, "per-command timeout in seconds (max 300)")
		fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
		fs.Parse(os.Args[2:])
		cmd := strings.Join(fs.Args(), " ")
		if strings.TrimSpace(cmd) == "" {
			fmt.Fprintln(os.Stderr, "exec: missing command")
			os.Exit(2)
		}

		b, err := client.New(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "config error:", err)
			os.Exit(2)
		}
		out, exitCode, err := b.Exec(context.Background(), cmd, *timeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec error:", err)
			os.Exit(1)
		}
		fmt.Println(out)
		if exitCode != 0 {
			os.Exit(exitCode)
		}

	case "token":
		cfgFile, err := qcsconfig.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "config error:", err)
			os.Exit(2)
		}
		secret, err := qcsconfig.LoadSecret()
		if err != nil {
			fmt.Fprintln(os.Stderr, "config error:", err)
			os.Exit(2)
		}
		region := cfgFile.Get("AWS_REGION", "AWS_DEFAULT_REGION")
		if region == "" {
			region = secret.Region
		}
		if region == "" {
			fmt.Fprintf(os.Stderr, "no region known: set AWS_REGION in %s\n", qcsconfig.Path())
			os.Exit(2)
		}
		fmt.Println(qcsconfig.FormatToken(region, secret.Value))

	case "-h", "--help", "help":
		fmt.Print(usage)

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
