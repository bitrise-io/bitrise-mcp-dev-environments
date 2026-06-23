package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jinzhu/configor"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/tool"
)

type config struct {
	// Addr is the host:port to listen on for the HTTP transport. When set the
	// server uses HTTP (and OAuth, if configured); when empty it uses stdio.
	Addr string `env:"ADDR"`
	// BitriseToken is the Bitrise API token used to authenticate requests in
	// stdio mode. Required in stdio mode; must be empty in HTTP mode (HTTP
	// clients authenticate per-request via OAuth or an Authorization header).
	BitriseToken string `env:"BITRISE_TOKEN"`
	// BitriseWorkspaceID is the default workspace ID (slug) for workspace-scoped
	// API calls in stdio mode. In HTTP mode the workspace is taken from the
	// x-bitrise-workspace-id header, or auto-detected when the user has one.
	BitriseWorkspaceID string `env:"BITRISE_WORKSPACE_ID"`
	// BitriseAPIBaseURL is the base URL for the Dev Environments backend.
	BitriseAPIBaseURL string `env:"BITRISE_API_BASE_URL" default:"https://codespaces-api.services.bitrise.io"`
	// BitriseMainAPIBaseURL is the base URL of the main Bitrise API, used for
	// workspace discovery (GET /organizations). The codespaces backend has no
	// list-workspaces endpoint.
	BitriseMainAPIBaseURL string `env:"BITRISE_MAIN_API_BASE_URL" default:"https://api.bitrise.io/v0.1"`
	// LogLevel is the log level for the application.
	LogLevel string `env:"LOG_LEVEL" default:"info"`
	// ExternalOAuthIssuer is the issuer URL of an external OAuth authorization
	// server. When set, the HTTP transport advertises
	// /.well-known/oauth-protected-resource and challenges credential-less
	// requests with a 401 so OAuth clients can discover and start the flow.
	// Requires OIDCTokenEndpoint and ServerBaseURL.
	ExternalOAuthIssuer string `env:"EXTERNAL_OAUTH_ISSUER"`
	// OIDCTokenEndpoint is the full URL of the OIDC token exchange endpoint
	// (RFC 8693) used to trade an external JWT for a Bitrise PAT. When set,
	// Bearer tokens that look like JWTs are exchanged before being used.
	OIDCTokenEndpoint string `env:"OIDC_TOKEN_ENDPOINT"`
	// ServerBaseURL is the public base URL of this MCP server (e.g.
	// https://mcp.example.com). Required when ExternalOAuthIssuer is set —
	// used in WWW-Authenticate headers and the protected resource metadata.
	ServerBaseURL string `env:"SERVER_BASE_URL"`
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

	// Set global config for API calls.
	devenv.BaseURL = strings.TrimRight(cfg.BitriseAPIBaseURL, "/")
	devenv.MainAPIBaseURL = strings.TrimRight(cfg.BitriseMainAPIBaseURL, "/")

	// Create tool belt and MCP server.
	belt := tool.NewBelt()
	mcpServer := server.NewMCPServer(
		"bitrise-mcp-dev-environments",
		"1.0.0",
		server.WithToolFilter(belt.FilterTools),
		server.WithRecovery(),
		server.WithLogging(),
	)
	belt.RegisterAll(mcpServer)

	if cfg.Addr == "" {
		logger.Info("starting MCP server in stdio mode")
		return runStdioTransport(cfg, belt, mcpServer)
	}
	logger.Info("starting MCP server in http mode")
	return runHTTPTransport(cfg, belt, mcpServer, logger)
}

func runStdioTransport(cfg config, belt *tool.Belt, mcpServer *server.MCPServer) error {
	if cfg.BitriseToken == "" {
		return fmt.Errorf("BITRISE_TOKEN must be set in stdio mode")
	}

	// Inject the configured token (and default workspace) into the context for
	// every tool call, then gate/resolve the workspace.
	server.WithToolHandlerMiddleware(func(fn server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx = devenv.ContextWithPAT(ctx, cfg.BitriseToken)
			if cfg.BitriseWorkspaceID != "" {
				ctx = devenv.ContextWithWorkspace(ctx, cfg.BitriseWorkspaceID)
			}
			ctx, errRes := belt.GateAndResolveWorkspace(ctx, request)
			if errRes != nil {
				return errRes, nil
			}
			return fn(ctx, request)
		}
	})(mcpServer)

	if err := server.ServeStdio(mcpServer); err != nil {
		return fmt.Errorf("serve stdio: %w", err)
	}
	return nil
}

func runHTTPTransport(cfg config, belt *tool.Belt, mcpServer *server.MCPServer, logger *zap.SugaredLogger) error {
	if cfg.BitriseToken != "" {
		return fmt.Errorf("BITRISE_TOKEN must not be set in http mode (clients authenticate via OAuth or a per-request Authorization header)")
	}

	// OAuth is active only when an external issuer is configured. It requires
	// the token-exchange endpoint and this server's public base URL.
	externalOAuthConfigured := cfg.ExternalOAuthIssuer != ""
	if externalOAuthConfigured && (cfg.OIDCTokenEndpoint == "" || cfg.ServerBaseURL == "") {
		return fmt.Errorf("EXTERNAL_OAUTH_ISSUER requires OIDC_TOKEN_ENDPOINT and SERVER_BASE_URL to be set")
	}

	var exchanger *jwtExchanger
	if cfg.OIDCTokenEndpoint != "" {
		exchanger = &jwtExchanger{tokenEndpoint: cfg.OIDCTokenEndpoint}
	}

	var metadataURL string
	httpServerOpts := []server.StreamableHTTPOption{
		server.WithStateLess(true),
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			ctx = devenv.ContextWithHostedMode(ctx)
			pat, err := extractPAT(r, exchanger)
			if err != nil {
				logger.Warnw("JWT→PAT exchange failed", "error", err)
			} else if pat != "" {
				ctx = devenv.ContextWithPAT(ctx, pat)
			}
			if ws := r.Header.Get("x-bitrise-workspace-id"); ws != "" {
				ctx = devenv.ContextWithWorkspace(ctx, ws)
			}
			return ctx
		}),
		server.WithDisableStreaming(true),
	}
	if externalOAuthConfigured {
		protectedResourceCfg := server.ProtectedResourceMetadataConfig{
			Resource:               cfg.ServerBaseURL,
			AuthorizationServers:   []string{cfg.ExternalOAuthIssuer},
			BearerMethodsSupported: []string{"header"},
		}
		httpServerOpts = append(httpServerOpts, server.WithProtectedResourceMetadata(protectedResourceCfg))
		metadataURL = cfg.ServerBaseURL + server.WellKnownProtectedResourcePath
	}

	// Gate host-dependent tools and resolve the workspace. PAT and the header
	// workspace are already in context from the HTTP context func above.
	server.WithToolHandlerMiddleware(func(fn server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, errRes := belt.GateAndResolveWorkspace(ctx, request)
			if errRes != nil {
				return errRes, nil
			}
			return fn(ctx, request)
		}
	})(mcpServer)

	mcpHandler := server.NewStreamableHTTPServer(mcpServer, httpServerOpts...)

	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	var mcpEntry http.Handler = mcpHandler
	if externalOAuthConfigured {
		mcpEntry = requireAuthMiddleware(mcpHandler, exchanger, metadataURL, logger)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Browser navigations get redirected to the docs instead of being
		// treated as MCP requests.
		if r.Header.Get("Sec-Fetch-Mode") == "navigate" {
			http.Redirect(w, r, "https://github.com/bitrise-io/bitrise-mcp-dev-environments/blob/main/README.md", http.StatusTemporaryRedirect)
			return
		}
		mcpEntry.ServeHTTP(w, r)
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errListen := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errListen <- fmt.Errorf("listen and serve: %w", err)
			return
		}
		errListen <- nil
	}()
	logger.Infof("listening on %q", cfg.Addr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		logger.Info("shutting down http server")
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}
	case err := <-errListen:
		return err
	}
	return nil
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
