package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Matches unordered list items with 2+ spaces after the marker: "*   foo", "-   bar"
	unorderedListRe = regexp.MustCompile(`^(\s*[-*+])\s{2,}(.*)$`)
	// Matches ordered list items with 2+ spaces after the marker: "1.   foo"
	orderedListRe = regexp.MustCompile(`^(\s*\d+\.)\s{2,}(.*)$`)
	// Matches fenced code block delimiters: ``` or ~~~
	codeFenceRe = regexp.MustCompile(`^(\s*` + "```" + `|\s*~~~)`)
)

func formatMarkdown(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	changed := false

	inFrontmatter := false
	inCodeBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for YAML frontmatter at the very beginning of the document
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}

		// Check for fenced code block toggle
		if codeFenceRe.MatchString(line) {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		// Normalize list markers with excessive whitespace
		if unorderedListRe.MatchString(line) {
			newLine := unorderedListRe.ReplaceAllString(line, "$1 $2")
			if newLine != line {
				lines[i] = newLine
				changed = true
			}
			continue
		}

		if orderedListRe.MatchString(line) {
			newLine := orderedListRe.ReplaceAllString(line, "$1 $2")
			if newLine != line {
				lines[i] = newLine
				changed = true
			}
			continue
		}
	}

	if !changed {
		return content, false
	}

	return strings.Join(lines, "\n"), true
}

func main() {
	root := "."
	var formattedCount int

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()
		if d.IsDir() {
			// Skip hidden directories, version control, build artifacts, and vendor
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			if name == "vendor" || name == "node_modules" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed reading %s: %w", path, err)
		}

		newContent, changed := formatMarkdown(string(data))
		if changed {
			if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
				return fmt.Errorf("failed writing %s: %w", path, err)
			}
			fmt.Printf("formatted %s\n", path)
			formattedCount++
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting markdown: %v\n", err)
		os.Exit(1)
	}

	if formattedCount > 0 {
		fmt.Printf("Normalized list spacing in %d markdown file(s).\n", formattedCount)
	}
}
