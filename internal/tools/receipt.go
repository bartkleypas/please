package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
	"github.com/bartkleypas/please/internal/storage"
)

var receiptExtractor = regexp.MustCompile(`obs_[0-9a-fA-F]{8}`)

// ObservationStore defines the lookup contract for inspecting raw telemetry receipts.
type ObservationStore interface {
	GetObservationBlob(receiptID string) (*storage.ObservationBlob, error)
}

// LegacyObservationResolver provides non-destructive read-through from in-node legacy observations.
type LegacyObservationResolver func(receiptID string) (string, error)

// InspectReceiptTool creates the inspect_receipt sensory tool (ADR 021).
func InspectReceiptTool(store ObservationStore, legacyResolver LegacyObservationResolver) Tool {
	return Tool{
		Name:        "inspect_receipt",
		Category:    domain.CategorySensory,
		Description: "Inspects raw telemetry from a past tool observation receipt by its receipt_id. Use this when you need exact lines, error traces, or verbose output from an earlier command.",
		Interactive: false,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"receipt_id": map[string]interface{}{
					"type":        "string",
					"description": "The receipt ID to look up (e.g. 'obs_94a2f8b1').",
				},
				"start_line": map[string]interface{}{
					"type":        "integer",
					"description": "Optional 1-indexed start line to page in (default: 1).",
				},
				"line_count": map[string]interface{}{
					"type":        "integer",
					"description": "Optional number of lines to retrieve (default: 50, max: 200).",
				},
				"grep": map[string]interface{}{
					"type":        "string",
					"description": "Optional regex pattern to filter lines from the raw output.",
				},
			},
			"required": []interface{}{"receipt_id"},
		},
		Function: func(ctx context.Context, args map[string]interface{}) (string, error) {
			receiptIDRaw, ok := args["receipt_id"].(string)
			if !ok || strings.TrimSpace(receiptIDRaw) == "" {
				return "", fmt.Errorf("receipt_id is required")
			}
			receiptID := strings.TrimSpace(receiptIDRaw)
			if matched := receiptExtractor.FindString(receiptID); matched != "" {
				receiptID = matched
			}

			var rawText string
			if store != nil {
				blob, err := store.GetObservationBlob(receiptID)
				if err == nil && blob != nil {
					rawText = string(blob.Payload)
				}
			}

			if rawText == "" && legacyResolver != nil {
				legacyText, err := legacyResolver(receiptID)
				if err == nil && legacyText != "" {
					rawText = legacyText
				}
			}

			if rawText == "" {
				return fmt.Sprintf("[Notice: Telemetry for receipt %q was not found or has been pruned by retention policy.]", receiptID), nil
			}

			allLines := strings.Split(rawText, "\n")
			type lineEntry struct {
				lineNum int
				text    string
			}
			var filtered []lineEntry

			grepPattern, _ := args["grep"].(string)
			if grepPattern != "" {
				re, err := regexp.Compile(grepPattern)
				if err != nil {
					return "", fmt.Errorf("invalid grep regex %q: %w", grepPattern, err)
				}
				for idx, l := range allLines {
					if re.MatchString(l) {
						filtered = append(filtered, lineEntry{lineNum: idx + 1, text: l})
					}
				}
			} else {
				filtered = make([]lineEntry, len(allLines))
				for idx, l := range allLines {
					filtered[idx] = lineEntry{lineNum: idx + 1, text: l}
				}
			}

			if len(filtered) == 0 {
				return fmt.Sprintf("[Receipt %s: No lines matched pattern %q across %d total lines.]", receiptID, grepPattern, len(allLines)), nil
			}

			startLine := 1
			if slVal, ok := args["start_line"].(float64); ok && slVal > 0 {
				startLine = int(slVal)
			}
			lineCount := 50
			if lcVal, ok := args["line_count"].(float64); ok && lcVal > 0 {
				lineCount = int(lcVal)
			}
			if lineCount > 200 {
				lineCount = 200
			}

			startIdx := 0
			if grepPattern == "" {
				startIdx = startLine - 1
				if startIdx < 0 {
					startIdx = 0
				}
				if startIdx >= len(filtered) {
					return fmt.Sprintf("[Receipt %s: start_line %d exceeds total line count %d.]", receiptID, startLine, len(allLines)), nil
				}
			} else {
				found := false
				for i, item := range filtered {
					if item.lineNum >= startLine {
						startIdx = i
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("[Receipt %s: No matching lines at or beyond start_line %d.]", receiptID, startLine), nil
				}
			}

			endIdx := startIdx + lineCount
			if endIdx > len(filtered) {
				endIdx = len(filtered)
			}

			slice := filtered[startIdx:endIdx]
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("[Receipt %s: Showing lines %d-%d of %d (total %d bytes)]\n",
				receiptID, slice[0].lineNum, slice[len(slice)-1].lineNum, len(allLines), len(rawText)))

			for _, item := range slice {
				sb.WriteString(fmt.Sprintf("%4d: %s\n", item.lineNum, item.text))
			}
			if endIdx < len(filtered) {
				sb.WriteString(fmt.Sprintf("... [%d more lines omitted. Use start_line=%d to continue.]\n",
					len(filtered)-endIdx, slice[len(slice)-1].lineNum+1))
			}

			return strings.TrimRight(sb.String(), "\n"), nil
		},
	}
}
