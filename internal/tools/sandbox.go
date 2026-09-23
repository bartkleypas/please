package tools

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// SensitivePathPatterns defines segments and prefixes quarantined from tool access.
var SensitivePathPatterns = []string{
	".secrets",
	".env",
	".ssh",
	"id_rsa",
	"id_ed25519",
	"id_ecdsa",
	"id_dsa",
	".aws",
	".config/gcloud",
	".gnupg",
	"vault.db",
}

// isQuarantinedPath checks if a path matches quarantined credential or conversation database patterns.
func isQuarantinedPath(path string) (string, bool) {
	normalized := filepath.ToSlash(path)
	clean := filepath.Clean(normalized)
	lower := strings.ToLower(clean)
	segments := strings.Split(clean, "/")
	baseLower := strings.ToLower(filepath.Base(clean))

	for _, pattern := range SensitivePathPatterns {
		pLower := strings.ToLower(pattern)

		// 1. Exact match on any directory or path segment (e.g. ".secrets", ".ssh", ".aws", ".gnupg")
		for _, seg := range segments {
			if strings.ToLower(seg) == pLower {
				return pattern, true
			}
		}

		// 2. Prefix match on base filename (e.g. ".env.local" matching ".env", or "id_ed25519")
		if strings.HasPrefix(baseLower, pLower+".") || (strings.HasPrefix(pLower, "id_") && strings.HasPrefix(baseLower, pLower)) {
			return pattern, true
		}

		// 3. Subpath or prefix directory match (e.g. ".config/gcloud" or "sub/.secrets/key")
		if strings.Contains(lower, "/"+pLower+"/") || strings.HasPrefix(lower, pLower+"/") || (strings.Contains(pLower, "/") && strings.Contains(lower, pLower)) {
			return pattern, true
		}

		// 4. Database filename match (e.g. "vault.db", "vault.db-wal", "vault.db-shm")
		if strings.HasSuffix(pLower, ".db") && (baseLower == pLower || strings.HasPrefix(baseLower, pLower+"-")) {
			return pattern, true
		}
	}

	return "", false
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

	// Quarantine check on requested path, relative path, and resolved canonical path
	if matched, quarantined := isQuarantinedPath(path); quarantined {
		return "", fmt.Errorf("security error: path '%s' matches quarantined sensitive pattern '%s'", path, matched)
	}
	if matched, quarantined := isQuarantinedPath(rel); quarantined {
		return "", fmt.Errorf("security error: path '%s' matches quarantined sensitive pattern '%s'", path, matched)
	}
	if matched, quarantined := isQuarantinedPath(canonicalPath); quarantined {
		return "", fmt.Errorf("security error: path '%s' resolves to quarantined sensitive pattern '%s'", path, matched)
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

// getStringArg extracts a string argument from the tool argument map.
func getStringArg(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid '%s' argument", key)
	}
	return val, nil
}

// getIntArg extracts an integer argument from the tool argument map with type coercion and default fallback.
func getIntArg(args map[string]interface{}, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		if int(n) > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	case string:
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultVal
}

// getBoolArg extracts a boolean argument with fallback to defaultVal.
func getBoolArg(args map[string]interface{}, key string, defaultVal bool) bool {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.ToLower(val) == "true"
	}
	return defaultVal
}

// resolveToolPath extracts the 'path' argument and verifies it against the active sandbox boundary.
func resolveToolPath(args map[string]interface{}, ws, prim string) (string, string, error) {
	path, err := getStringArg(args, "path")
	if err != nil {
		return "", "", err
	}
	safePath, err := ValidateSafePath(ws, path, prim)
	if err != nil {
		return "", "", err
	}
	return path, safePath, nil
}
