// Command midpoint-mcp-server exposes Evolveum midPoint over the Model Context
// Protocol. M0 ships a single stdio server with one tool, ping, that verifies
// connectivity and returns the authenticated identity via /ws/rest/self.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName = "midpoint-mcp-server"
	version    = "0.0.1"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%s: %v", serverName, err)
	}
}

func run() error {
	cfg, err := midpoint.ConfigFromEnv()
	if err != nil {
		return err
	}
	client := midpoint.NewClient(cfg)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: version,
	}, nil)
	registerPing(server, client)
	registerReadTools(server, client)
	registerWriteTools(server, client, cfg.AllowWrites)
	registerRequestTools(server, client, cfg.AllowWrites)

	// Protocol traffic owns stdout; diagnostics go to stderr.
	log.SetOutput(os.Stderr)
	writeState := "disabled (dry-run previews)"
	if cfg.AllowWrites {
		writeState = "ENABLED"
	}
	log.Printf("%s %s serving on stdio (midPoint: %s; writes: %s)", serverName, version, cfg.BaseURL, writeState)

	// End the session cleanly on Ctrl-C / SIGTERM as well as stdin EOF.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return server.Run(ctx, &mcp.StdioTransport{})
}

// pingInput has no fields: ping takes no arguments.
type pingInput struct{}

// pingOutput is the structured result of the ping tool.
type pingOutput struct {
	OID          string `json:"oid" jsonschema:"the authenticated user's midPoint OID"`
	Name         string `json:"name" jsonschema:"the authenticated user's login name"`
	FullName     string `json:"fullName,omitempty" jsonschema:"the authenticated user's full name, if set"`
	EmailAddress string `json:"emailAddress,omitempty" jsonschema:"the authenticated user's email address, if set"`
}

func registerPing(server *mcp.Server, client *midpoint.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Title:       "Ping midPoint",
		Description: "Check connectivity to midPoint and report the authenticated identity (calls GET /ws/rest/self).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ pingInput) (*mcp.CallToolResult, pingOutput, error) {
		id, err := client.Self(ctx)
		if err != nil {
			return nil, pingOutput{}, err
		}
		out := pingOutput{
			OID:          id.OID,
			Name:         id.Name,
			FullName:     id.FullName,
			EmailAddress: id.EmailAddress,
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Connected to midPoint as %q (oid %s).", id.Name, id.OID),
			}},
		}, out, nil
	})
}
