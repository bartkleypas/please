package engine

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	SandboxPolicyStrict     = "strict"
	SandboxPolicyStandard   = "standard"
	SandboxPolicyPermissive = "permissive"
)

// StrictAllowedCommands contains minimal read-only and benign inspection tools
var StrictAllowedCommands = []string{
	"git", "go", "swift", "ls", "grep", "cat", "echo", "find", "pwd", "date", "mkdir",
}

// StandardAllowedCommands includes build and workspace file manipulation utilities
var StandardAllowedCommands = []string{
	"git", "go", "swift", "ls", "grep", "cat", "echo", "find", "pwd", "date", "mkdir",
	"make", "npm", "cargo", "rm", "diff", "wc", "head", "tail", "touch", "node", "python3",
}

// GetAllowedCommands returns the allowed binaries for a given policy name
func GetAllowedCommands(policy string) []string {
	switch strings.ToLower(policy) {
	case SandboxPolicyStrict:
		return StrictAllowedCommands
	case SandboxPolicyPermissive:
		return nil // Nil indicates unrestricted
	case SandboxPolicyStandard, "":
		fallthrough
	default:
		return StandardAllowedCommands
	}
}

// ValidateSafePath verifies that the target path does not escape the workspace root.
// It also resolves symbolic links to prevent directory traversal escapes.
func ValidateSafePath(workspaceDir, path string) (string, error) {
	base := workspaceDir
	if base == "" {
		base = "."
	}
	absRoot, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute workspace root: %w", err)
	}

	// Resolve symlinks on the root itself if present
	if evalRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = evalRoot
	}

	var targetPath string
	if filepath.IsAbs(path) {
		targetPath = filepath.Clean(path)
	} else {
		targetPath = filepath.Join(absRoot, path)
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Canonicalize absPath if it or any ancestor exists
	canonicalPath := absPath
	checkPath := absPath
	for checkPath != "" && checkPath != "/" && checkPath != "." {
		if evalPath, err := filepath.EvalSymlinks(checkPath); err == nil {
			relFromCheck, err := filepath.Rel(checkPath, absPath)
			if err == nil {
				if relFromCheck == "." {
					canonicalPath = evalPath
				} else {
					canonicalPath = filepath.Join(evalPath, relFromCheck)
				}
			}
			break
		}
		parent := filepath.Dir(checkPath)
		if parent == checkPath {
			break
		}
		checkPath = parent
	}

	// Boundary check against canonical workspace root
	rel, err := filepath.Rel(absRoot, canonicalPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("security error: path '%s' is outside of workspace root (%s)", path, absRoot)
	}

	return canonicalPath, nil
}

// ParseAndValidatePipeline decomposes compound shell commands and verifies that every binary is permitted.
func ParseAndValidatePipeline(command string, allowedList []string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("empty command")
	}

	// Permissive mode allows all commands
	if allowedList == nil {
		return nil
	}

	// 1. Forbid dangerous subshell substitutions and privileged escalations
	if strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`") {
		return fmt.Errorf("security error: subshell execution ($(...) or backticks) is prohibited in sandbox")
	}
	if strings.Contains(trimmed, "<(") || strings.Contains(trimmed, ">(") {
		return fmt.Errorf("security error: process substitution (<(...) or >(...)) is prohibited in sandbox")
	}

	// 2. Split pipeline on shell separators: &&, ||, ;, |, and newlines
	// Normalize separators to a common delimiter
	normalized := trimmed
	normalized = strings.ReplaceAll(normalized, "&&", "\n")
	normalized = strings.ReplaceAll(normalized, "||", "\n")
	normalized = strings.ReplaceAll(normalized, ";", "\n")
	normalized = strings.ReplaceAll(normalized, "|", "\n")

	lines := strings.Split(normalized, "\n")
	for _, segment := range lines {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}

		parts := strings.Fields(seg)
		if len(parts) == 0 {
			continue
		}

		rawBinary := parts[0]

		// Disallow sudo explicitly
		if rawBinary == "sudo" {
			return fmt.Errorf("security error: sudo is strictly prohibited")
		}

		// Extract base name if full path is used (e.g. /usr/bin/git -> git)
		binary := filepath.Base(rawBinary)

		allowed := false
		for _, a := range allowedList {
			if binary == a {
				allowed = true
				break
			}
		}

		if !allowed {
			return fmt.Errorf("security error: command '%s' is not in the allow-list under active sandbox policy", binary)
		}
	}

	return nil
}
