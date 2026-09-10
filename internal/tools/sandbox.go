package tools

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

// StrictAllowedCommands contains minimal read-only and benign inspection tools.
var StrictAllowedCommands = []string{
	"git", "go", "swift", "ls", "grep", "cat", "echo", "find", "pwd", "date", "mkdir",
}

// StandardAllowedCommands includes build and workspace file manipulation utilities.
var StandardAllowedCommands = []string{
	"git", "go", "swift", "ls", "grep", "cat", "echo", "find", "pwd", "date", "mkdir",
	"make", "npm", "cargo", "rm", "diff", "wc", "head", "tail", "touch", "node", "python3", "ssh",
}

// GetAllowedCommands returns the allowed binaries for a given policy name.
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

// parseWorkspaceArgs extracts workspaceDir and optional primaryWorkspace from variadic arguments.
func parseWorkspaceArgs(workspaceDir ...string) (string, string) {
	ws := "."
	prim := ""
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
	}
	if len(workspaceDir) > 1 && workspaceDir[1] != "" {
		prim = workspaceDir[1]
	}
	return ws, prim
}

// ValidateSafePath verifies that the target path does not escape the workspace root.
// It also resolves symbolic links to prevent directory traversal escapes.
// If primaryWorkspace is provided and path is an absolute path originating inside primaryWorkspace,
// it is virtualized by rebasing the relative path onto workspaceDir.
func ValidateSafePath(workspaceDir, path string, primaryWorkspace ...string) (string, error) {
	base := workspaceDir
	if base == "" {
		base = "."
	}
	absRoot := canonicalizePath(base)

	// Virtualize path if it originates from primaryWorkspace
	if len(primaryWorkspace) > 0 && primaryWorkspace[0] != "" && filepath.IsAbs(path) {
		primRoot := canonicalizePath(primaryWorkspace[0])
		canonPath := canonicalizePath(path)
		if relFromPrim, err := filepath.Rel(primRoot, canonPath); err == nil && !strings.HasPrefix(relFromPrim, "..") {
			path = relFromPrim
		}
	}

	var targetPath string
	if filepath.IsAbs(path) {
		targetPath = filepath.Clean(path)
	} else {
		targetPath = filepath.Join(absRoot, path)
	}

	canonicalPath := canonicalizePath(targetPath)

	// Boundary check against canonical workspace root
	rel, err := filepath.Rel(absRoot, canonicalPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("security error: path '%s' is outside of workspace root (%s)", path, absRoot)
	}

	return canonicalPath, nil
}

// canonicalizePath resolves symlinks on existing ancestors of a path
func canonicalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	checkPath := abs
	for checkPath != "" && checkPath != "/" && checkPath != "." {
		if evalPath, err := filepath.EvalSymlinks(checkPath); err == nil {
			relFromCheck, err := filepath.Rel(checkPath, abs)
			if err == nil {
				if relFromCheck == "." {
					return evalPath
				}
				return filepath.Join(evalPath, relFromCheck)
			}
			break
		}
		parent := filepath.Dir(checkPath)
		if parent == checkPath {
			break
		}
		checkPath = parent
	}
	return abs
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
