// qcs-mcp is a local MCP server that lets Qoder execute commands inside
// AWS CloudShell via the SQS bridge (qcs-agent on the far side).
package main

import (
	"context"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"qcs-bridge/internal/client"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("[qcs-mcp] ")

	b, err := client.New(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	s := server.NewMCPServer("qcs-bridge", "0.2.0", server.WithToolCapabilities(false))

	s.AddTool(mcp.NewTool("cloudshell_exec",
		mcp.WithDescription("Execute a shell command inside AWS CloudShell (with that environment's IAM identity and network position) via the SQS bridge. Agent runs in read-only allowlist mode by default: kubectl get/describe/logs/top, aws describe/list/get families, basic diagnostics. Output is capped at ~200KB."),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to execute in CloudShell")),
		mcp.WithNumber("timeout_seconds", mcp.Description("Per-command timeout (1-300, default 120)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cmd := req.GetString("command", "")
		timeout := int(req.GetFloat("timeout_seconds", 120))
		out, _, err := b.Exec(ctx, cmd, timeout)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	})

	s.AddTool(mcp.NewTool("cloudshell_status",
		mcp.WithDescription("Check whether the CloudShell bridge agent is alive: last heartbeat age, IAM identity, host, uptime."),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		out, err := b.Status(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	})

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}
