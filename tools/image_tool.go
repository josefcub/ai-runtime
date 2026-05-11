package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/agent-project/harness/imageutil"
	"github.com/agent-project/harness/sandbox"
)

// imageToolResult is the structured return value for view_image.
type imageToolResult struct {
	Attachment struct {
		Data     string `json:"data"`
		MIMEType string `json:"mime_type"`
	} `json:"__attachment"`
	Text string `json:"text"`
}

// RegisterImageTools registers image-related tools on the given registry.
func RegisterImageTools(reg *Registry) {
	workingDir := reg.workingDir

	reg.Register("view_image",
		"View an image file. Loads the image into the conversation context so it can be referenced and analyzed by the agent. Supports PNG, JPEG, GIF, WebP, BMP, and TIFF formats (max 4 MB).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Relative path from working directory to the image file."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolViewImage(workingDir, args)
		})
}

// toolViewImage reads an image file and returns it as a base64-encoded attachment.
func toolViewImage(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %v", err)
	}

	if len(data) > imageutil.MaxImageSize {
		return "", fmt.Errorf("image too large: %d bytes (max %d)", len(data), imageutil.MaxImageSize)
	}

	mime := imageutil.DetectMIME(data)
	if mime == "" {
		return "", fmt.Errorf("not a recognized image file: %s", path)
	}

	// Get file size for the text description
	sizeKB := len(data) / 1024
	if len(data)%1024 != 0 {
		sizeKB++
	}

	result := imageToolResult{
		Text: fmt.Sprintf("Image loaded: %s (%s, %d KB)", path, mime, sizeKB),
	}
	result.Attachment.Data = base64.StdEncoding.EncodeToString(data)
	result.Attachment.MIMEType = mime

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal result: %v", err)
	}

	return strings.TrimSpace(string(out)), nil
}
