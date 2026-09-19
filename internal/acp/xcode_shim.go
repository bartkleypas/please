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
//  3. Editor state statements:
//     - "The user is looking at file X at line Y."
//     - "The user is currently inside this file: X"
//     - "The user has no code selected."
//  4. Code selection statements ("The user has selected the following code...").
//
// This shim acts as an acoustic damper and quarantine boundary:
//  - Discards the phantom tools, virtual manifests, and style static into /dev/null.
//    None of this boilerplate is stored in the database or persisted in the DAG.
//  - Normalizes virtual package container prefixes (e.g. "PleasePackage/Sources/..." -> "Sources/...").
//  - Surgically extracts code selection snippets and line ranges into peripheral telemetry.
//  - Preserves the sacred human voice: cleanedPrompt contains strictly what the user typed.
//  - Returns the clean prompt and ambient telemetry (activeFile, cursorLine, selectedLines, selectedCode)
//    so the harness can route them into the standard <ADDITIONAL_METADATA> envelope.
// =============================================================================

var (
	// Matches <system-reminder>...</system-reminder> XML blocks
	systemReminderRegex = regexp.MustCompile(`(?s)<system-reminder>(.*?)</system-reminder>`)

	// Matches individual editor open file statements (requires file extension to avoid keywords like "selected")
	openFileRegex = regexp.MustCompile(`The user (?:is looking at|has) (?:file\s+)?([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9]+)(?: open)?(?: at line (\d+))?\.?`)
	noFileRegex   = regexp.MustCompile(`The user has no file currently open\.?`)

	// Matches Xcode "currently inside this file" preamble variation (Xcode 16+ without selection)
	insideFileRegex = regexp.MustCompile(`The user is currently inside this file:\s*([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9]+)`)
	noCodeSelRegex  = regexp.MustCompile(`The user has no code selected\.?`)

	// Matches Xcode code selection statements
	// e.g. "The user has selected the following code from that file (lines 22-23):"
	// e.g. "The user has selected the following code from file Sources/... (lines 22-23):"
	selectionRegex = regexp.MustCompile(`(?i)The user has selected the following code(?: from (?:that file|file\s+([a-zA-Z0-9_\-./\\]+\.[a-zA-Z0-9]+)))?\s*\(lines?\s*(\d+)(?:-(\d+))?\):`)
)

const xcodeManifestPrefix = "Project structure (these are Xcode workspace-relative paths"

// normalizeXcodePath strips virtual Xcode package container prefixes (e.g. "PleasePackage/Sources/..." -> "Sources/...").
func normalizeXcodePath(p string) string {
	trimmed := strings.TrimSpace(p)
	parts := strings.Split(trimmed, "/")
	if len(parts) > 1 {
		switch parts[1] {
		case "Sources", "Tests", "Docs", "scripts", ".please":
			return strings.Join(parts[1:], "/")
		}
	}
	return trimmed
}

// SanitizeXcodePrompt filters an incoming client prompt through the Xcode quarantine shim.
// It strips Xcode-injected preamble noise, extracts active editor telemetry (file, line, selection),
// removes the highlighted code snippet from the prompt so only the user's authentic words remain,
// and returns the clean human prompt along with the extracted telemetry.
func SanitizeXcodePrompt(raw string) (cleanedPrompt, activeFile string, cursorLine int, selectedLines, selectedCode string) {
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
			activeFile = normalizeXcodePath(after[mOpen[2]:mOpen[3]])
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
				activeFile = normalizeXcodePath(sub[1])
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

	// 3. Check for Xcode "currently inside this file" preamble variation
	if sub := insideFileRegex.FindStringSubmatch(cleaned); len(sub) >= 2 {
		if activeFile == "" {
			activeFile = normalizeXcodePath(sub[1])
		}
		cleaned = insideFileRegex.ReplaceAllString(cleaned, "")
	}
	cleaned = noCodeSelRegex.ReplaceAllString(cleaned, "")

	// 4. Surgically extract Xcode code selection and snippet
	if loc := selectionRegex.FindStringSubmatchIndex(cleaned); len(loc) >= 6 {
		if loc[2] != -1 && loc[3] != -1 && activeFile == "" {
			activeFile = normalizeXcodePath(cleaned[loc[2]:loc[3]])
		}

		startLine := 0
		if loc[4] != -1 && loc[5] != -1 {
			if l, err := strconv.Atoi(cleaned[loc[4]:loc[5]]); err == nil {
				startLine = l
				if cursorLine == 0 {
					cursorLine = l
				}
			}
		}

		endLine := startLine
		if len(loc) >= 8 && loc[6] != -1 && loc[7] != -1 {
			if l, err := strconv.Atoi(cleaned[loc[6]:loc[7]]); err == nil {
				endLine = l
			}
		}

		if endLine > startLine {
			selectedLines = fmt.Sprintf("%d-%d", startLine, endLine)
		} else if startLine > 0 {
			selectedLines = fmt.Sprintf("%d", startLine)
		}

		lineCount := 1
		if endLine >= startLine && startLine > 0 {
			lineCount = endLine - startLine + 1
		}

		before := cleaned[:loc[0]]
		after := cleaned[loc[1]:]

		afterLines := strings.Split(after, "\n")
		idx := 0
		if len(afterLines) > 0 && strings.TrimSpace(afterLines[0]) == "" {
			idx = 1
		}

		codeEnd := idx + lineCount
		if codeEnd > len(afterLines) {
			codeEnd = len(afterLines)
		}

		selectedCode = strings.TrimSpace(strings.Join(afterLines[idx:codeEnd], "\n"))
		userRest := strings.TrimSpace(strings.Join(afterLines[codeEnd:], "\n"))

		if strings.TrimSpace(before) != "" && userRest != "" {
			cleaned = strings.TrimSpace(before) + "\n\n" + userRest
		} else if strings.TrimSpace(before) != "" {
			cleaned = strings.TrimSpace(before)
		} else {
			cleaned = userRest
		}
	}

	// 5. Clean up leading/trailing whitespace
	cleanedPrompt = strings.TrimSpace(cleaned)

	// Fallback: if stripping removed everything, provide a natural prompt or preserve raw
	if cleanedPrompt == "" && selectedCode != "" {
		cleanedPrompt = "Please examine the selected code."
	} else if cleanedPrompt == "" && strings.TrimSpace(raw) != "" {
		cleanedPrompt = strings.TrimSpace(raw)
	}

	return cleanedPrompt, activeFile, cursorLine, selectedLines, selectedCode
}

// ParseClientPrompt is the general ACP entrypoint for prompt hygiene, delegating to SanitizeXcodePrompt.
func ParseClientPrompt(raw string) (cleanedPrompt, activeFile string, cursorLine int, selectedLines, selectedCode string) {
	return SanitizeXcodePrompt(raw)
}
