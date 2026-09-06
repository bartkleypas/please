package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_PaginationAndWindowing(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create a 20-line test file with one extra-long line
	var content strings.Builder
	for i := 1; i <= 20; i++ {
		if i == 10 {
			// Super long line (3000 chars)
			content.WriteString(strings.Repeat("longline-", 300))
			content.WriteString("\n")
		} else {
			fmt.Fprintf(&content, "Line %d content\n", i)
		}
	}
	if err := os.WriteFile(testFile, []byte(content.String()), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	registry := NewToolRegistry()
	RegisterDefaultTools(registry, tmpDir)

	// 1. Test basic read with pagination default
	res, err := registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path": "test.txt",
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if !strings.Contains(res, "[Lines 1-20 of 20") {
		t.Errorf("expected header with 20 lines, got:\n%s", res)
	}
	if !strings.Contains(res, "Line 1 content") {
		t.Errorf("expected Line 1 content in output")
	}
	if !strings.Contains(res, "[line truncated,") {
		t.Errorf("expected long line to be truncated, got:\n%s", res)
	}

	// 2. Test offset and limit
	res, err = registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path":   "test.txt",
		"offset": 5,
		"limit":  3,
	})
	if err != nil {
		t.Fatalf("read_file with offset/limit failed: %v", err)
	}
	if !strings.Contains(res, "[Lines 5-7 of 20") {
		t.Errorf("expected slice [Lines 5-7 of 20, got:\n%s", res)
	}
	if !strings.Contains(res, "Line 5 content") || !strings.Contains(res, "Line 7 content") {
		t.Errorf("expected lines 5 through 7, got:\n%s", res)
	}
	if strings.Contains(res, "Line 4 content") || strings.Contains(res, "Line 8 content") {
		t.Errorf("should not contain lines outside slice, got:\n%s", res)
	}
	if !strings.Contains(res, "offset=8") {
		t.Errorf("expected next offset hint (offset=8), got:\n%s", res)
	}

	// 3. Test byte budget
	res, err = registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path":      "test.txt",
		"offset":    1,
		"max_bytes": 50,
	})
	if err != nil {
		t.Fatalf("read_file with max_bytes failed: %v", err)
	}
	if !strings.Contains(res, "Byte budget reached") {
		t.Errorf("expected byte budget hint, got:\n%s", res)
	}
}

func TestWriteFile_OverwriteAndTelemetry(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, tmpDir)

	writeFile := registry.Tools["write_file"].Function
	ctx := context.Background()

	// 1. Create a new file
	res, err := writeFile(ctx, map[string]interface{}{
		"path":    "hello.txt",
		"content": "Hello\nWorld",
	})
	if err != nil {
		t.Fatalf("unexpected error creating file: %v", err)
	}
	expectedMsg := "file 'hello.txt' created successfully (11 bytes, 2 lines)"
	if res != expectedMsg {
		t.Errorf("expected '%s', got '%s'", expectedMsg, res)
	}

	diskBytes, err := os.ReadFile(filepath.Join(tmpDir, "hello.txt"))
	if err != nil || string(diskBytes) != "Hello\nWorld" {
		t.Fatalf("file content on disk mismatch: %s", string(diskBytes))
	}

	// 2. Attempt duplicate write without overwrite flag -> should fail with informative error
	_, err = writeFile(ctx, map[string]interface{}{
		"path":    "hello.txt",
		"content": "Duplicate write",
	})
	if err == nil {
		t.Fatalf("expected error when file already exists without overwrite flag")
	}
	if !strings.Contains(err.Error(), "to overwrite completely, set overwrite=true") {
		t.Errorf("expected actionable overwrite hint in error, got: %v", err)
	}

	// 3. Overwrite with overwrite: true
	res, err = writeFile(ctx, map[string]interface{}{
		"path":      "hello.txt",
		"content":   "Overwritten\nContent\nHere",
		"overwrite": true,
	})
	if err != nil {
		t.Fatalf("unexpected error overwriting file: %v", err)
	}
	expectedOverwriteMsg := "file 'hello.txt' overwritten successfully (24 bytes, 3 lines)"
	if res != expectedOverwriteMsg {
		t.Errorf("expected '%s', got '%s'", expectedOverwriteMsg, res)
	}

	diskBytes, err = os.ReadFile(filepath.Join(tmpDir, "hello.txt"))
	if err != nil || string(diskBytes) != "Overwritten\nContent\nHere" {
		t.Fatalf("overwritten file content on disk mismatch: %s", string(diskBytes))
	}

	// 4. Overwrite with overwrite: "true" string
	res, err = writeFile(ctx, map[string]interface{}{
		"path":      "hello.txt",
		"content":   "String\nTrue",
		"overwrite": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error overwriting file with string true: %v", err)
	}
	if !strings.Contains(res, "overwritten successfully") {
		t.Errorf("expected 'overwritten successfully', got '%s'", res)
	}

	// 5. Test edit_file with explicit mode
	editFile := registry.Tools["edit_file"].Function
	res, err = editFile(ctx, map[string]interface{}{
		"path":    "hello.txt",
		"mode":    "replace_string",
		"search":  "String",
		"replace": "Edited",
	})
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}
	if res != "file 'hello.txt' edited successfully (mode: replace_string)" {
		t.Errorf("expected 'file 'hello.txt' edited successfully (mode: replace_string)', got '%s'", res)
	}

	// 6. Test edit_file with omitted mode (defaults to replace_string)
	res, err = editFile(ctx, map[string]interface{}{
		"path":    "hello.txt",
		"search":  "Edited",
		"replace": "Patched",
	})
	if err != nil {
		t.Fatalf("edit_file with default mode failed: %v", err)
	}
	if res != "file 'hello.txt' edited successfully (mode: replace_string)" {
		t.Errorf("expected default replace_string mode success, got '%s'", res)
	}

	// 7. Test edit_file with block alias parameters (search_block / replace_block)
	res, err = editFile(ctx, map[string]interface{}{
		"path":          "hello.txt",
		"search_block":  "Patched\nTrue",
		"replace_block": "Final\nContent",
	})
	if err != nil {
		t.Fatalf("edit_file with search_block/replace_block failed: %v", err)
	}
	if res != "file 'hello.txt' edited successfully (mode: replace_string)" {
		t.Errorf("expected block replacement success, got '%s'", res)
	}

	diskBytes, err = os.ReadFile(filepath.Join(tmpDir, "hello.txt"))
	if err != nil || string(diskBytes) != "Final\nContent" {
		t.Fatalf("unexpected content after edits: %s", string(diskBytes))
	}
}

func TestDefaultTools_AppendFile(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, tmpDir)

	ctx := context.Background()
	appendFile := registry.Tools["append_file"].Function

	// 1. Append to non-existent file in sub-directory -> creates file and parent dirs cleanly
	res, err := appendFile(ctx, map[string]interface{}{
		"path":    "logs/sub/events.log",
		"content": "event 1: initialized",
	})
	if err != nil {
		t.Fatalf("unexpected error creating file via append: %v", err)
	}
	if !strings.Contains(res, "created successfully via append") {
		t.Errorf("expected creation message, got: %s", res)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "logs/sub/events.log"))
	if err != nil || string(content) != "event 1: initialized" {
		t.Fatalf("unexpected disk content: %q", string(content))
	}

	// 2. Append to existing file without trailing newline -> automatically inserts newline boundary
	res, err = appendFile(ctx, map[string]interface{}{
		"path":    "logs/sub/events.log",
		"content": "event 2: connected\n",
	})
	if err != nil {
		t.Fatalf("unexpected error appending: %v", err)
	}
	if !strings.Contains(res, "appended successfully") {
		t.Errorf("expected appended message, got: %s", res)
	}

	content, err = os.ReadFile(filepath.Join(tmpDir, "logs/sub/events.log"))
	if err != nil {
		t.Fatalf("failed reading file: %v", err)
	}
	expected := "event 1: initialized\nevent 2: connected\n"
	if string(content) != expected {
		t.Fatalf("expected clean newline insertion between appends.\nExpected:\n%q\nGot:\n%q", expected, string(content))
	}

	// 3. Append to existing file with trailing newline when content starts with newline -> no double blank lines
	res, err = appendFile(ctx, map[string]interface{}{
		"path":    "logs/sub/events.log",
		"content": "\nevent 3: completed",
	})
	if err != nil {
		t.Fatalf("unexpected error appending: %v", err)
	}

	content, err = os.ReadFile(filepath.Join(tmpDir, "logs/sub/events.log"))
	if err != nil {
		t.Fatalf("failed reading file: %v", err)
	}
	expected = "event 1: initialized\nevent 2: connected\nevent 3: completed"
	if string(content) != expected {
		t.Fatalf("expected no duplicate blank lines when appending with leading newline.\nExpected:\n%q\nGot:\n%q", expected, string(content))
	}

	// 4. Sandboxing check: cannot write outside workspace
	_, err = appendFile(ctx, map[string]interface{}{
		"path":    "../../outside.txt",
		"content": "escape attempt",
	})
	if err == nil {
		t.Fatalf("expected sandbox violation error for path traversal")
	}
}

func TestDefaultTools_SearchAndExec(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, tmpDir)

	ctx := context.Background()

	// Write files
	err := os.WriteFile(filepath.Join(tmpDir, "foo.go"), []byte("package main\nfunc hello() {}\n"), 0644)
	if err != nil {
		t.Fatalf("failed writing foo.go: %v", err)
	}
	err = os.WriteFile(filepath.Join(tmpDir, "bar.txt"), []byte("some text\n"), 0644)
	if err != nil {
		t.Fatalf("failed writing bar.txt: %v", err)
	}

	// list_directory
	listDir := registry.Tools["list_directory"].Function
	res, err := listDir(ctx, map[string]interface{}{"path": "."})
	if err != nil {
		t.Fatalf("list_directory failed: %v", err)
	}
	if !strings.Contains(res, "foo.go") || !strings.Contains(res, "bar.txt") {
		t.Errorf("expected foo.go and bar.txt in list_directory output, got: %s", res)
	}

	// grep_search
	grepSearch := registry.Tools["grep_search"].Function
	res, err = grepSearch(ctx, map[string]interface{}{"pattern": "func hello"})
	if err != nil {
		t.Fatalf("grep_search failed: %v", err)
	}
	if !strings.Contains(res, "foo.go:2: func hello() {}") {
		t.Errorf("expected grep match in foo.go:2, got: %s", res)
	}

	// list_files_recursive
	listFiles := registry.Tools["list_files_recursive"].Function
	res, err = listFiles(ctx, map[string]interface{}{"path": "."})
	if err != nil {
		t.Fatalf("list_files_recursive failed: %v", err)
	}
	if !strings.Contains(res, "foo.go") {
		t.Errorf("expected foo.go in recursive list, got: %s", res)
	}

	// execute_command (allowed command: echo)
	execCmd := registry.Tools["execute_command"].Function
	res, err = execCmd(ctx, map[string]interface{}{"command": "echo test_exec"})
	if err != nil {
		t.Fatalf("execute_command failed: %v", err)
	}
	if !strings.Contains(res, "test_exec") {
		t.Errorf("expected 'test_exec' in command output, got: %s", res)
	}
}
