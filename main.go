package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jinzhu/configor"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/tool"
)

type config struct {
	// BitriseToken is the Bitrise API token used to authenticate requests.
	BitriseToken string `env:"BITRISE_TOKEN" required:"true"`
	// BitriseAPIBaseURL is the base URL for Bitrise API requests.
	BitriseAPIBaseURL string `env:"BITRISE_API_BASE_URL" default:"https://codespaces-api.services.bitrise.io"`
	// LogLevel is the log level for the application.
	LogLevel string `env:"LOG_LEVEL" default:"info"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("error: %v\n", err)
	}
}

func run() error {
	var cfg config
	if err := configor.Load(&cfg); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := newLogger(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	// Set global base URL for API calls
	devenv.BaseURL = strings.TrimRight(cfg.BitriseAPIBaseURL, "/")

	// Create tool belt and MCP server
	belt := tool.NewBelt()
	mcpServer := server.NewMCPServer(
		"bitrise-mcp-dev-environments",
		"1.0.0",
		server.WithRecovery(),
		server.WithLogging(),
	)
	belt.RegisterAll(mcpServer)

	// Inject token into context for all tool calls
	server.WithToolHandlerMiddleware(func(fn server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return fn(devenv.ContextWithPAT(ctx, cfg.BitriseToken), request)
		}
	})(mcpServer)

	logger.Info("starting MCP server in stdio mode")
	return server.ServeStdio(mcpServer)
}

func newLogger(level string) (*zap.SugaredLogger, error) {
	cfg := zap.NewProductionConfig()
	switch level {
	case "debug":
		cfg.Level.SetLevel(zap.DebugLevel)
	case "warn":
		cfg.Level.SetLevel(zap.WarnLevel)
	case "error":
		cfg.Level.SetLevel(zap.ErrorLevel)
	default:
		cfg.Level.SetLevel(zap.InfoLevel)
	}
	cfg.OutputPaths = []string{"stderr"} // MCP stdio uses stdout, logs go to stderr
	cfg.ErrorOutputPaths = []string{"stderr"}

	l, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	return l.Sugar(), nil
}
