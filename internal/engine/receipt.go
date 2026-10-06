package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
)

var receiptIDRegex = regexp.MustCompile(`obs_[0-9a-fA-F]{8}`)

// GenerateReceiptID returns a deterministic content-addressable ID based on tool and payload.
func GenerateReceiptID(tool string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(tool))
	h.Write([]byte{0})
	h.Write(payload)
	sum := h.Sum(nil)
	return "obs_" + hex.EncodeToString(sum[:4])
}

// ExtractReceiptID attempts to extract an "obs_<8hex>" identifier from a string.
func ExtractReceiptID(str string) string {
	return receiptIDRegex.FindString(str)
}

// CreateSmartReceipt constructs a deterministic SmartReceipt from execution telemetry.
func CreateSmartReceipt(toolName string, args map[string]interface{}, rawResult string, hasBlob bool) domain.SmartReceipt {
	receiptID := GenerateReceiptID(toolName, []byte(rawResult))
	lines := strings.Split(rawResult, "\n")
	lineCount := len(lines)
	byteCount := len(rawResult)

	var banner string
	var summary string
	var command string
	var path string

	if cmdVal, ok := args["command"].(string); ok {
		command = cmdVal
	}
	if pathVal, ok := args["path"].(string); ok {
		path = pathVal
	}

	// Extract high-signal failure or continuation banner
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "FAIL:") || strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "error:") || strings.HasPrefix(trimmed, "Error:") {
			banner = trimmed
			break
		}
		if banner == "" && (strings.HasPrefix(trimmed, "[Lines ") || strings.HasPrefix(trimmed, "[Offset ")) {
			banner = trimmed
		}
	}

	if banner == "" && len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		if len(firstLine) > 80 {
			firstLine = firstLine[:77] + "..."
		}
		banner = firstLine
	}

	summary = fmt.Sprintf("%d lines, %d bytes", lineCount, byteCount)

	return domain.SmartReceipt{
		ReceiptID: receiptID,
		Tool:      toolName,
		Command:   command,
		Path:      path,
		Lines:     lineCount,
		Bytes:     byteCount,
		Summary:   summary,
		Banner:    banner,
		HasBlob:   hasBlob,
	}
}

// FormatReceiptString formats a SmartReceipt into a deterministic string representation for prompt contexts.
func FormatReceiptString(receipt domain.SmartReceipt) string {
	bannerPart := ""
	if receipt.Banner != "" {
		bannerPart = fmt.Sprintf(" Banner: %s.", receipt.Banner)
	}

	if receipt.HasBlob {
		return fmt.Sprintf("[Receipt %s: Tool '%s' execution completed (%s).%s Telemetry stored out-of-band; dereference via inspect_receipt('%s').]",
			receipt.ReceiptID, receipt.Tool, receipt.Summary, bannerPart, receipt.ReceiptID)
	}

	return fmt.Sprintf("[Receipt %s: Tool '%s' execution completed (%s).%s]",
		receipt.ReceiptID, receipt.Tool, receipt.Summary, bannerPart)
}

// FormatObservationReceipt formats an observation into its receipt representation for prompt contexts.
// If the observation has a typed SmartReceipt, it formats that receipt directly without string heuristic parsing.
// Otherwise, it falls back to formatCompactedToolObservation.
func FormatObservationReceipt(toolName string, obs domain.ToolObservation) string {
	if obs.Receipt != nil {
		return FormatReceiptString(*obs.Receipt)
	}
	return formatCompactedToolObservation(toolName, obs.Result)
}

