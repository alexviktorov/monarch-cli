package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"monarch-cli/pkg/monarch"
)

const appVersion = "1.0.0"

// cmdMCP serves the MCP server over stdio. Write tools are dual-key gated:
// they are REGISTERED (and therefore advertised/callable) only when both
// --allow-writes and MONARCH_MCP_ALLOW_WRITES=1 are present; a mismatch is
// a startup error so a config can never silently run in the wrong mode.
func cmdMCP(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	allowWrites := fs.Bool("allow-writes", false,
		"register the update_transaction write tool (also requires MONARCH_MCP_ALLOW_WRITES=1)")
	fs.Parse(args)

	writesEnv := os.Getenv("MONARCH_MCP_ALLOW_WRITES") == "1"
	if *allowWrites != writesEnv {
		fatal("write tools need BOTH --allow-writes and MONARCH_MCP_ALLOW_WRITES=1; got one without the other")
	}
	writes := *allowWrites && writesEnv

	// stdout is the protocol channel; all logging goes to stderr. The
	// interactive login/prompt paths are never reachable from mcp mode.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var opts []monarch.Option
	if writes {
		opts = append(opts, monarch.WithWritesEnabled())
	}
	// Tolerate no session at startup: the monarch_login tool can establish
	// one interactively via elicitation, so a GUI host isn't dead-ended
	// when the saved session has expired.
	client, err := newClientMaybeLoggedIn(opts...)
	if err != nil {
		fatal("%v", err)
	}

	server := buildMCPServer(&liveAPI{c: client}, &liveAuth{c: client}, writes, logger, newToolLimits())
	if writes {
		logger.Warn("WRITE TOOLS ENABLED", "tools", "update_transaction")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = server.Run(ctx, &mcp.StdioTransport{})
	// The client closing stdin (EOF) is the normal way a stdio session ends.
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
		fatal("mcp server: %v", err)
	}
}
