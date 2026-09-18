package acp

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// Matches <system-reminder>...</system-reminder> XML blocks
	systemReminderRegex = regexp.MustCompile(`(?s)<system-reminder>(.*?)</system-reminder>`)

	// Matches Xcode project structure manifest blocks (from "Project structure" through package dependencies / open file notes)
	xcodeManifestRegex = regexp.MustCompile(`(?s)Project structure \(these are Xcode workspace-relative paths.*?(The user has no file currently open\.|The user (?:is looking at|has) (?:file\s+)?([a-zA-Z0-9_\-./\\]+)(?: open)?(?: at line (\d+))?\.?)`)

	// Matches individual editor open file statements (avoiding "no file")
	openFileRegex = regexp.MustCompile(`The user (?:is looking at|has) (?:file\s+)?([a-zA-Z0-9_\-./\\]+)(?: open)?(?: at line (\d+))?\.?`)
	noFileRegex   = regexp.MustCompile(`The user has no file currently open\.?`)
)

// ParseClientPrompt extracts client-injected system reminders, Xcode project structure manifests,
// and editor state statements from incoming ACP prompts, returning the sanitized human prompt,
// extracted system reminder text, and active file / cursor line telemetry.
func ParseClientPrompt(raw string) (cleanedPrompt, reminder, activeFile string, cursorLine int) {
	cleaned := raw
	var reminders []string

	// 1. Extract <system-reminder> tags
	matches := systemReminderRegex.FindAllStringSubmatchIndex(cleaned, -1)
	if len(matches) > 0 {
		// Collect reminder contents
		for _, m := range matches {
			if len(m) >= 4 {
				reminders = append(reminders, strings.TrimSpace(cleaned[m[2]:m[3]]))
			}
		}
		// Remove tags from prompt
		cleaned = systemReminderRegex.ReplaceAllString(cleaned, "")
	}

	// 2. Extract Xcode project structure and file status blocks
	if xcodeManifestRegex.MatchString(cleaned) {
		loc := xcodeManifestRegex.FindStringIndex(cleaned)
		manifestBlock := cleaned[loc[0]:loc[1]]
		reminders = append(reminders, strings.TrimSpace(manifestBlock))

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
	reminder = strings.TrimSpace(strings.Join(reminders, "\n\n"))

	// Fallback: if stripping removed everything, preserve the raw prompt
	if cleanedPrompt == "" && strings.TrimSpace(raw) != "" {
		cleanedPrompt = strings.TrimSpace(raw)
	}

	return cleanedPrompt, reminder, activeFile, cursorLine
}
