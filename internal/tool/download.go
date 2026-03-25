package tool

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

type downloadResp struct {
	SignedURL string `json:"signedUrl"`
}

// Download downloads files from a session to the local machine.
var Download = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_download",
		mcp.WithDescription(`Download a file or directory from a running devenv session to the local machine.

The remote path is archived as tar.gz, uploaded to cloud storage, then downloaded and extracted locally.

Example: Download a build artifact:
  session_id: <uuid>
  source_path: /Users/vagrant/project/build/output
  local_destination: /Users/me/downloads/output`),
		mcp.WithString("session_id", mcp.Description("The unique identifier of the running session"), mcp.Required()),
		mcp.WithString("source_path", mcp.Description("Absolute path on the remote machine to download"), mcp.Required()),
		mcp.WithString("local_destination", mcp.Description("Local directory path where files will be extracted"), mcp.Required()),
		mcp.WithBoolean("only_contents", mcp.Description("If true and source is a directory, extract only its contents (not the directory itself)")),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		sourcePath, err := request.RequireString("source_path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		localDest, err := request.RequireString("local_destination")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{
			"source_path": sourcePath,
		}
		if oc, ok := request.GetArguments()["only_contents"]; ok {
			body["only_contents_of_folder"] = oc
		}

		// Step 1: Request download archive from the VM
		res, err := devenv.CallAPILongTimeout(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/download", sessionID)),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("request download", err), nil
		}

		var resp downloadResp
		if err := json.Unmarshal([]byte(res), &resp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("parse download response: %v", err)), nil
		}

		// Step 2: Download the tar.gz from GCS
		archiveData, err := downloadArchive(ctx, resp.SignedURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("download archive: %v", err)), nil
		}

		// Step 3: Extract locally
		if err := extractTarGz(archiveData, localDest); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("extract archive: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Download complete. Files extracted to %s", localDest)), nil
	},
}

func downloadArchive(ctx context.Context, signedURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

func extractTarGz(data []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		target := filepath.Join(destDir, header.Name) //nolint:gosec // trusted source from our own GCS
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent dir: %w", err)
			}
			f, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // trusted source
				f.Close()
				return fmt.Errorf("write file: %w", err)
			}
			f.Close()
			if err := os.Chmod(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("chmod: %w", err)
			}
		}
	}
	return nil
}
