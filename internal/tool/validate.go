package tool

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// requireUUID extracts a required string parameter and validates it as a UUID.
func requireUUID(request mcp.CallToolRequest, name string) (string, error) {
	value, err := request.RequireString(name)
	if err != nil {
		return "", err
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", fmt.Errorf("invalid %s: not a valid UUID", name)
	}
	return value, nil
}
