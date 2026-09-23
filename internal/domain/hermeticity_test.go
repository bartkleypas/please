package domain_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDomainHermeticity verifies ADR-018 Layer 0 invariant:
// internal/domain must have ZERO imports from github.com/bartkleypas/please/...
// and must only depend on the Go standard library.
func TestDomainHermeticity(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		// Only parse production Go files in the package (skip _test.go files)
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("failed to parse domain package files: %v", err)
	}

	if len(pkgs) == 0 {
		t.Fatalf("no package found in current directory")
	}

	for pkgName, pkg := range pkgs {
		for filePath, file := range pkg.Files {
			baseName := filepath.Base(filePath)
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)

				// Assert no imports from internal please repository
				if strings.Contains(importPath, "github.com/bartkleypas/please") {
					t.Errorf("HERMETICITY VIOLATION in %s (%s): domain layer must not import please packages: %s",
						baseName, pkgName, importPath)
				}

				// Assert standard library only (no dot in the first path segment)
				firstSegment := strings.Split(importPath, "/")[0]
				if strings.Contains(firstSegment, ".") {
					t.Errorf("HERMETICITY VIOLATION in %s (%s): domain layer must only depend on Go standard library, got third-party package: %s",
						baseName, pkgName, importPath)
				}
			}
		}
	}
}
