package constants

import "strings"

// AttachmentKind buckets MIME types so size caps and tool behaviour can route
// off a single classification.
type AttachmentKind int

const (
	AttachmentKindImage AttachmentKind = iota
	AttachmentKindText
	AttachmentKindBinary
)

// Per-kind upload size caps. Images are bounded by what the vision API can
// handle without server-side downscale; text by what the read_attachment tool
// would ever return (50 KB) plus headroom; binary by what the user might
// reasonably embed in a generated app (PDFs, fonts, ZIPs).
const (
	MaxAttachmentSizeImageBytes  = 10 * 1024 * 1024 // 10 MB
	MaxAttachmentSizeTextBytes   = 5 * 1024 * 1024  // 5 MB
	MaxAttachmentSizeBinaryBytes = 50 * 1024 * 1024 // 50 MB
)

// Per-turn attachment limits. The image cap is lower because each image costs
// vision tokens; the overall cap is a safety net against pathological clients.
const (
	MaxAttachmentsPerTurn      = 10
	MaxImageAttachmentsPerTurn = 5
)

// Manifest cap — the LLM sees the most-recent N project assets per turn.
const ManifestMaxAssets = 50

// read_attachment hard cap on returned body bytes.
const ReadAttachmentMaxBytes = 50 * 1024 // 50 KB

// ClassifyAttachment buckets a content type. Unknown / octet-stream falls
// through to Binary. The text bucket includes JSON and YAML because the
// LLM should be able to reason over them via read_attachment.
func ClassifyAttachment(contentType string) AttachmentKind {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "image/") {
		return AttachmentKindImage
	}
	if strings.HasPrefix(ct, "text/") ||
		ct == "application/json" ||
		ct == "application/yaml" ||
		ct == "application/x-yaml" {
		return AttachmentKindText
	}
	return AttachmentKindBinary
}

// MaxAttachmentSize returns the per-file byte cap for the given kind.
func MaxAttachmentSize(kind AttachmentKind) int64 {
	switch kind {
	case AttachmentKindImage:
		return MaxAttachmentSizeImageBytes
	case AttachmentKindText:
		return MaxAttachmentSizeTextBytes
	default:
		return MaxAttachmentSizeBinaryBytes
	}
}

// MaxAttachmentSizeMB is a convenience for error messages.
func MaxAttachmentSizeMB(kind AttachmentKind) int64 {
	return MaxAttachmentSize(kind) / (1024 * 1024)
}
