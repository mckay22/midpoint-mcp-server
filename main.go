// Command midpoint-mcp-server exposes Evolveum midPoint over the Model Context
// Protocol. It runs over stdio by default (personal mode: the configured
// credentials' identity) or over streamable HTTP with --http. HTTP mode is
// loopback-only until per-request auth lands (see PLAN.md M4.5).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverName = "midpoint-mcp-server"

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.0.1-dev"

func main() {
	var (
		httpAddr    string
		showVersion bool
	)
	flag.StringVar(&httpAddr, "http", "", "serve the streamable HTTP transport on this address (e.g. :3001 or 127.0.0.1:3001); default is stdio. Loopback only until M4.5.")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("%s %s\n", serverName, version)
		return
	}

	if err := run(httpAddr); err != nil {
		log.Fatalf("%s: %v", serverName, err)
	}
}

// run wires up the server and serves it over the selected transport.
func run(httpAddr string) error {
	cfg, err := midpoint.ConfigFromEnv()
	if err != nil {
		return err
	}
	client := midpoint.NewClient(cfg)

	// Protocol traffic owns stdout; diagnostics go to stderr.
	log.SetOutput(os.Stderr)

	if httpAddr == "" {
		return serveStdio(client, cfg)
	}
	return serveHTTP(httpAddr, client, cfg)
}

// newMCPServer builds a server with every tool registered.
func newMCPServer(client *midpoint.Client, cfg midpoint.Config) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: version}, nil)
	registerPing(server, client)
	registerReadTools(server, client)
	registerWriteTools(server, client, cfg.AllowWrites)
	registerRequestTools(server, client, cfg.AllowWrites)
	return server
}

// writeState describes the write gate for startup logging.
func writeState(cfg midpoint.Config) string {
	if cfg.AllowWrites {
		return "ENABLED"
	}
	return "disabled (dry-run previews)"
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
