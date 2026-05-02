package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
)

// ReadAttachmentTool lets the LLM read the textual contents of a non-image
// project asset. Images return an error pointing the model at vision; binary
// files return an error pointing the model at the URL. Returned bodies are
// hard-capped at constants.ReadAttachmentMaxBytes.
type ReadAttachmentTool struct {
	Deps *RunDependencies
}

func (ReadAttachmentTool) Name() string { return "read_attachment" }

func (ReadAttachmentTool) Description() string {
	return "Read the text contents of a non-image project asset by its id. " +
		"Use this for CSV, JSON, YAML, Markdown, or other text files the user uploaded. " +
		"Returns up to 50 KB of UTF-8 text. " +
		"For images, you can already see them when attached to the user's current message — no tool call needed. " +
		"For binary files (PDF, ZIP, fonts, 3D models), embed the URL directly in your generated code."
}

func (ReadAttachmentTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"asset_id": map[string]any{
				"type":        "string",
				"format":      "uuid",
				"description": "The asset ID from the Project Assets manifest.",
			},
		},
		"required":             []string{"asset_id"},
		"additionalProperties": false,
	}
}

type readAttachmentInput struct {
	AssetID string `json:"asset_id"`
}

func (t ReadAttachmentTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var in readAttachmentInput
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	assetID, err := uuid.Parse(in.AssetID)
	if err != nil {
		return fmt.Sprintf("asset_id must be a valid UUID, got %q", in.AssetID), true, nil
	}

	asset, err := t.Deps.DB.GetAppAssetByID(assetID.String())
	if err != nil || asset == nil {
		return fmt.Sprintf("asset %s not found", assetID), true, nil
	}
	// Cross-project leak protection: confirm the asset belongs to the project
	// this run is scoped to.
	if asset.AppProjectID != rc.Project.ID {
		return fmt.Sprintf("asset %s not found", assetID), true, nil
	}

	switch constants.ClassifyAttachment(asset.ContentType) {
	case constants.AttachmentKindImage:
		return "This is an image (" + asset.ContentType + "). It is already visible to you when attached to the current user message; otherwise reference it by URL: " + asset.PublicURL, true, nil
	case constants.AttachmentKindBinary:
		return "Binary file (" + asset.ContentType + "). Embed the URL in generated app code; the bytes are not directly readable. URL: " + asset.PublicURL, true, nil
	}

	// Text kind. Fetch from S3, slice to the cap.
	body, err := t.Deps.AssetsStorage.ReadObjectBytes(ctx, asset.S3Key)
	if err != nil {
		return fmt.Sprintf("failed to fetch asset bytes: %v", err), true, nil
	}
	originalSize := len(body)
	truncated := false
	if originalSize > constants.ReadAttachmentMaxBytes {
		body = body[:constants.ReadAttachmentMaxBytes]
		truncated = true
	}
	out := string(body)
	if truncated {
		out += fmt.Sprintf("\n\n[truncated — original size: %d KB; cap: %d KB]", originalSize/1024, constants.ReadAttachmentMaxBytes/1024)
	}
	wrapped := "<attachment filename=\"" + escapeAttr(asset.FileName) + "\" mime=\"" + escapeAttr(asset.ContentType) + "\">" +
		escapeUntrustedTag(out, "attachment") +
		"</attachment>"
	return wrapped, false, nil
}

// escapeAttr neutralises the closing-tag delimiters and quote characters in
// filename/mime values so a malicious filename like `evil"><script>` can't
// inject extra attributes or break out of the wrapping <attachment> element.
func escapeAttr(v string) string {
	r := strings.NewReplacer(
		`"`, "&quot;",
		`<`, "&lt;",
		`>`, "&gt;",
	)
	return r.Replace(v)
}
