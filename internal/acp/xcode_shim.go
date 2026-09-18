package acp

import (
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
//
// This shim acts as an acoustic damper and quarantine boundary:
//  - Discards the phantom tools, virtual manifests, and style static into /dev/null.
//    None of this boilerplate is stored in the database or persisted in the DAG.
//  - Extracts the singular grain of truth: the editor's active open file and cursor line.
//  - Returns the pristine human prompt, allowing the caller to route the file/line
//    telemetry into standard ambient metadata (<ADDITIONAL_METADATA>).
// =============================================================================

var (
	// Matches <system-reminder>...</system-reminder> XML blocks
	systemReminderRegex = regexp.MustCompile(`(?s)<system-reminder>(.*?)</system-reminder>`)

	// Matches Xcode project structure manifest blocks (from "Project structure" through package dependencies / open file notes)
	xcodeManifestRegex = regexp.MustCompile(`(?s)Project structure \(these are Xcode workspace-relative paths.*?(The user has no file currently open\.|The user (?:is looking at|has) (?:file\s+)?([a-zA-Z0-9_\-./\\]+)(?: open)?(?: at line (\d+))?\.?)`)

	// Matches individual editor open file statements (avoiding "no file")
	openFileRegex = regexp.MustCompile(`The user (?:is looking at|has) (?:file\s+)?([a-zA-Z0-9_\-./\\]+)(?: open)?(?: at line (\d+))?\.?`)
	noFileRegex   = regexp.MustCompile(`The user has no file currently open\.?`)
)

// SanitizeXcodePrompt filters an incoming client prompt through the Xcode quarantine shim.
// It strips Xcode-injected preamble noise, extracts active editor telemetry (file, line),
// and returns the clean human prompt. The discarded static is never persisted to storage.
func SanitizeXcodePrompt(raw string) (cleanedPrompt, activeFile string, cursorLine int) {
	cleaned := raw

	// 1. Strip <system-reminder> tags completely (discarded into /dev/null)
	cleaned = systemReminderRegex.ReplaceAllString(cleaned, "")

	// 2. Extract Xcode project structure and file status blocks
	if xcodeManifestRegex.MatchString(cleaned) {
		loc := xcodeManifestRegex.FindStringIndex(cleaned)
		manifestBlock := cleaned[loc[0]:loc[1]]

		// Check for open file in the manifest block (only if not "no file currently open")
		if !noFileRegex.MatchString(manifestBlock) {
			if sub := openFileRegex.FindStringSubmatch(manifestBlock); len(sub) >= 2 && sub[1] != "no" {
				activeFile = strings.TrimSpace(sub[1])
				if len(sub) >= 3 && sub[2] != "" {
					if line, err := strconv.Atoi(sub[2]); err == nil {
						cursorLine = line
					}
				}
			}
		}

		cleaned = cleaned[:loc[0]] + cleaned[loc[1]:]
	} else {
		// Standalone open file check
		if !noFileRegex.MatchString(cleaned) {
			if sub := openFileRegex.FindStringSubmatch(cleaned); len(sub) >= 2 && sub[1] != "no" {
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

	// 3. Clean up leading/trailing whitespace
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
