package acp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// =============================================================================
// Xcode Shim Stack
// =============================================================================
// Apple's Xcode Coding Assistant (macOS Goldengate / Xcode 16+) injects a massive,
// noisy preamble onto every incoming turn:
//  1. <system-reminder> XML blocks containing Swift style mandates and phantom
//     tool directives (XcodeRead, XcodeWrite, XcodeGrep) that do not exist in ACP.
//  2. Virtual workspace project manifests (e.g. "OwlPlease/Sources/...") that
//     do not correspond to physical filesystem paths on disk.
//  3. Editor state statements ("The user is looking at file X at line Y.").
//  4. Code selection statements ("The user has selected the following code...").
//
// This shim acts as an acoustic damper and quarantine boundary:
//  - Discards the phantom tools, virtual manifests, and style static into /dev/null.
//    None of this boilerplate is stored in the database or persisted in the DAG.
//  - Formats highlighted code selections into clean markdown headers ([Selected code (lines X-Y)]).
//  - Extracts the singular grain of truth: the editor's active open file and cursor/selection line.
//  - Returns the pristine human prompt, allowing the caller to route the file/line
//    telemetry into standard ambient metadata (<ADDITIONAL_METADATA>).
// =============================================================================

var (
	// Matches <system-reminder>...</system-reminder> XML blocks
	systemReminderRegex = regexp.MustCompile(`(?s)<system-reminder>(.*?)</system-reminder>`)

	// Matches individual editor open file statements (requires file extension to avoid keywords like "selected")
	openFileRegex = regexp.MustCompile(`The user (?:is looking at|has) (?:file\s+)?([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9]+)(?: open)?(?: at line (\d+))?\.?`)
	noFileRegex   = regexp.MustCompile(`The user has no file currently open\.?`)

	// Matches Xcode code selection statements
	// e.g. "The user has selected the following code from that file (lines 22-23):"
	// e.g. "The user has selected the following code from file Sources/... (lines 22-23):"
	selectionRegex = regexp.MustCompile(`(?i)The user has selected the following code(?: from (?:that file|file\s+([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9]+)))?\s*\(lines?\s*(\d+)(?:-(\d+))?\):`)
)

const xcodeManifestPrefix = "Project structure (these are Xcode workspace-relative paths"

// SanitizeXcodePrompt filters an incoming client prompt through the Xcode quarantine shim.
// It strips Xcode-injected preamble noise, extracts active editor telemetry (file, line),
// and returns the clean human prompt. The discarded static is never persisted to storage.
func SanitizeXcodePrompt(raw string) (cleanedPrompt, activeFile string, cursorLine int) {
	cleaned := raw

	// 1. Strip <system-reminder> tags completely (discarded into /dev/null)
	cleaned = systemReminderRegex.ReplaceAllString(cleaned, "")

	// 2. Extract and remove Xcode project structure manifest block
	if idx := strings.Index(cleaned, xcodeManifestPrefix); idx >= 0 {
		after := cleaned[idx:]
		end := len(after)

		mOpen := openFileRegex.FindStringSubmatchIndex(after)
		mNoFile := noFileRegex.FindStringIndex(after)
		mSel := selectionRegex.FindStringIndex(after)
		pkgIdx := strings.Index(after, "Package dependencies:")

		if len(mOpen) >= 4 && (len(mSel) == 0 || mOpen[0] < mSel[0]) {
			end = mOpen[1]
			activeFile = strings.TrimSpace(after[mOpen[2]:mOpen[3]])
			if len(mOpen) >= 6 && mOpen[4] != -1 && mOpen[5] != -1 {
				if line, err := strconv.Atoi(after[mOpen[4]:mOpen[5]]); err == nil {
					cursorLine = line
				}
			}
		} else if len(mNoFile) == 2 && (len(mSel) == 0 || mNoFile[0] < mSel[0]) {
			end = mNoFile[1]
		} else if len(mSel) == 2 {
			end = mSel[0]
		} else if pkgIdx >= 0 {
			lineEnd := strings.Index(after[pkgIdx:], "\n")
			if lineEnd >= 0 {
				end = pkgIdx + lineEnd + 1
			}
		}

		cleaned = cleaned[:idx] + cleaned[idx+end:]
	} else {
		// Standalone open file check (when manifest is not present)
		if !noFileRegex.MatchString(cleaned) {
			if sub := openFileRegex.FindStringSubmatch(cleaned); len(sub) >= 2 {
				activeFile = strings.TrimSpace(sub[1])
				if len(sub) >= 3 && sub[2] != "" {
					if line, err := strconv.Atoi(sub[2]); err == nil {
						cursorLine = line
					}
				}
				cleaned = openFileRegex.ReplaceAllString(cleaned, "")
			}
		}
		cleaned = noFileRegex.ReplaceAllString(cleaned, "")
	}

	// 3. Process Xcode code selection statements
	if selectionRegex.MatchString(cleaned) {
		cleaned = selectionRegex.ReplaceAllStringFunc(cleaned, func(match string) string {
			sub := selectionRegex.FindStringSubmatch(match)
			if len(sub) >= 4 {
				if activeFile == "" && sub[1] != "" {
					activeFile = strings.TrimSpace(sub[1])
				}
				if cursorLine == 0 && sub[2] != "" {
					if l, err := strconv.Atoi(sub[2]); err == nil {
						cursorLine = l
					}
				}
				startLine := sub[2]
				endLine := sub[3]
				if endLine != "" {
					return fmt.Sprintf("[Selected code (lines %s-%s)]:", startLine, endLine)
				}
				return fmt.Sprintf("[Selected code (line %s)]:", startLine)
			}
			return match
		})
	}

	// 4. Clean up leading/trailing whitespace
	cleanedPrompt = strings.TrimSpace(cleaned)

	// Fallback: if stripping removed everything, preserve the raw prompt
	if cleanedPrompt == "" && strings.TrimSpace(raw) != "" {
		cleanedPrompt = strings.TrimSpace(raw)
	}

	return cleanedPrompt, activeFile, cursorLine
}

// ParseClientPrompt is the general ACP entrypoint for prompt hygiene, delegating to SanitizeXcodePrompt.
func ParseClientPrompt(raw string) (cleanedPrompt, activeFile string, cursorLine int) {
	return SanitizeXcodePrompt(raw)
}
