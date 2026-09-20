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
	if !strings.Contains(res, "offset: 8") {
		t.Errorf("expected next offset hint (offset: 8), got:\n%s", res)
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
	if !strings.Contains(res, "Byte budget reached") || !strings.Contains(res, "call read_file with path:") {
		t.Errorf("expected actionable byte budget hint, got:\n%s", res)
	}

	// 4. Test calling read_file on a directory -> fails with directive to use list_directory
	_, err = registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path": ".",
	})
	if err == nil {
		t.Fatalf("expected error reading directory with read_file, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory, not a file (use list_directory instead)") {
		t.Errorf("expected directory warning error, got: %v", err)
	}

	// 5. Test 25KB file fits under 64KB default budget
	largeContent := strings.Repeat("a line of content that is reasonably long and descriptive for testing...\n", 130)
	os.WriteFile(filepath.Join(tmpDir, "large.txt"), []byte(largeContent), 0644)
	res, err = registry.Tools["read_file"].Function(context.Background(), map[string]interface{}{
		"path": "large.txt",
	})
	if err != nil {
		t.Fatalf("read_file large.txt failed: %v", err)
	}
	if !strings.Contains(res, "[Lines 1-130 of 130") {
		t.Errorf("expected all 130 lines to fit under 64KB budget, got header:\n%s", res[:strings.Index(res, "\n\n")])
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

func TestEditFile_FailureSignaling(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, tmpDir)

	ctx := context.Background()
	editFile := registry.Tools["edit_file"].Function

	sampleContent := "alpha\nbeta\ngamma\nbeta\nomega"
	if err := os.WriteFile(filepath.Join(tmpDir, "sample.txt"), []byte(sampleContent), 0644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	// 1. Missing search string in replace_string -> non-nil error
	_, err := editFile(ctx, map[string]interface{}{
		"path":    "sample.txt",
		"mode":    "replace_string",
		"search":  "nonexistent",
		"replace": "replacement",
	})
	if err == nil {
		t.Fatalf("expected error for missing search string, got nil")
	}
	if !strings.Contains(err.Error(), "search string not found in file 'sample.txt'") {
		t.Errorf("expected missing search string error, got: %v", err)
	}

	// 2. Empty search string in replace_string -> non-nil error
	_, err = editFile(ctx, map[string]interface{}{
		"path":    "sample.txt",
		"mode":    "replace_string",
		"search":  "",
		"replace": "replacement",
	})
	if err == nil {
		t.Fatalf("expected error for empty search string, got nil")
	}
	if !strings.Contains(err.Error(), "search parameter cannot be empty") {
		t.Errorf("expected empty search parameter error, got: %v", err)
	}

	// 3. Ambiguous multiple occurrences in replace_string -> non-nil error
	_, err = editFile(ctx, map[string]interface{}{
		"path":    "sample.txt",
		"mode":    "replace_string",
		"search":  "beta",
		"replace": "replacement",
	})
	if err == nil {
		t.Fatalf("expected error for duplicate search string matches, got nil")
	}
	if !strings.Contains(err.Error(), "search string matched 2 occurrences in 'sample.txt'") {
		t.Errorf("expected multi-match disambiguation error, got: %v", err)
	}

	// 4. Zero-match regex in replace_regex -> non-nil error
	_, err = editFile(ctx, map[string]interface{}{
		"path":    "sample.txt",
		"mode":    "replace_regex",
		"search":  "^zeta.*",
		"replace": "replacement",
	})
	if err == nil {
		t.Fatalf("expected error for zero-match regex, got nil")
	}
	if !strings.Contains(err.Error(), "matched no content in file 'sample.txt'") {
		t.Errorf("expected zero-match regex error, got: %v", err)
	}

	// 5. Missing pattern in insert_after -> non-nil error
	_, err = editFile(ctx, map[string]interface{}{
		"path":    "sample.txt",
		"mode":    "insert_after",
		"search":  "missing_target",
		"replace": "inserted",
	})
	if err == nil {
		t.Fatalf("expected error for missing insert_after pattern, got nil")
	}
	if !strings.Contains(err.Error(), "search pattern 'missing_target' not found in file 'sample.txt'") {
		t.Errorf("expected insert_after missing pattern error, got: %v", err)
	}

	// 6. Empty pattern in insert_after -> non-nil error
	_, err = editFile(ctx, map[string]interface{}{
		"path":    "sample.txt",
		"mode":    "insert_after",
		"search":  "",
		"replace": "inserted",
	})
	if err == nil {
		t.Fatalf("expected error for empty search pattern in insert_after, got nil")
	}
	if !strings.Contains(err.Error(), "search parameter cannot be empty") {
		t.Errorf("expected empty search parameter error, got: %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewToolRegistry()
	RegisterDefaultTools(registry, tmpDir)

	ctx := context.Background()
	delTool, ok := registry.Tools["delete_file"]
	if !ok {
		t.Fatalf("delete_file tool not found in registry")
	}

	// 1. Successfully delete existing file
	filePath := filepath.Join(tmpDir, "to_delete.txt")
	if err := os.WriteFile(filePath, []byte("temp"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	res, err := delTool.Function(ctx, map[string]interface{}{"path": "to_delete.txt"})
	if err != nil {
		t.Fatalf("unexpected error deleting file: %v", err)
	}
	if !strings.Contains(res, "deleted successfully") {
		t.Errorf("expected deleted successfully in response, got: %s", res)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted from disk")
	}

	// 2. Delete non-existent file
	_, err = delTool.Function(ctx, map[string]interface{}{"path": "nonexistent.txt"})
	if err == nil {
		t.Fatalf("expected error deleting non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' error, got: %v", err)
	}

	// 3. Attempt to delete a directory
	subDir := filepath.Join(tmpDir, "sub_dir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	_, err = delTool.Function(ctx, map[string]interface{}{"path": "sub_dir"})
	if err == nil {
		t.Fatalf("expected error when attempting to delete directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("expected directory error, got: %v", err)
	}

	// 4. Path traversal attempt outside workspace
	_, err = delTool.Function(ctx, map[string]interface{}{"path": "../../outside.txt"})
	if err == nil {
		t.Fatalf("expected security error on path traversal, got nil")
	}

	// 5. Quarantined file access attempt
	quarantinedFile := filepath.Join(tmpDir, ".env")
	_ = os.WriteFile(quarantinedFile, []byte("SECRET=1"), 0644)
	_, err = delTool.Function(ctx, map[string]interface{}{"path": ".env"})
	if err == nil {
		t.Fatalf("expected quarantine error for .env file, got nil")
	}
	if !strings.Contains(err.Error(), "matches quarantined sensitive pattern") {
		t.Errorf("expected quarantine security violation, got: %v", err)
	}
}

type mockMemStore struct {
	items map[string]*MemoryItem
}

func newMockMemStore() *mockMemStore {
	return &mockMemStore{items: make(map[string]*MemoryItem)}
}

func (m *mockMemStore) SaveMemory(mem *MemoryItem) error {
	m.items[mem.Key] = mem
	return nil
}

func (m *mockMemStore) GetMemory(scope, sessionID, key string) (*MemoryItem, error) {
	if item, ok := m.items[key]; ok {
		return item, nil
	}
	return nil, nil
}

func (m *mockMemStore) QueryMemories(filter MemoryFilter) ([]MemoryItem, error) {
	var out []MemoryItem
	for _, item := range m.items {
		if filter.Query != "" && !strings.Contains(item.Content, filter.Query) && !strings.Contains(item.Key, filter.Query) {
			continue
		}
		if filter.Key != "" && item.Key != filter.Key {
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

func (m *mockMemStore) DeleteMemory(scope, sessionID, key string) error {
	delete(m.items, key)
	return nil
}

func (m *mockMemStore) DiagnoseMemories(scope, sessionID string) (*MemoryDiagnostics, error) {
	diag := &MemoryDiagnostics{
		TotalMemories: len(m.items),
		ByScope:       map[string]int{"workspace": len(m.items)},
		ByCategory:    map[string]int{"constraint": len(m.items)},
		StorageBytes:  1024,
	}
	for _, it := range m.items {
		diag.MostAccessed = append(diag.MostAccessed, *it)
	}
	return diag, nil
}

func TestMemoryTools(t *testing.T) {
	store := newMockMemStore()

	registry := NewToolRegistry()
	registry.RegisterMemory(store, "workspace")

	ctx := WithMemoryContext(context.Background(), "session-42", "node-100")

	storeTool, ok := registry.Tools["memory_store"]
	if !ok {
		t.Fatalf("memory_store tool not found in registry")
	}
	recallTool, ok := registry.Tools["memory_recall"]
	if !ok {
		t.Fatalf("memory_recall tool not found in registry")
	}
	deleteTool, ok := registry.Tools["memory_delete"]
	if !ok {
		t.Fatalf("memory_delete tool not found in registry")
	}
	diagnoseTool, ok := registry.Tools["memory_diagnose"]
	if !ok {
		t.Fatalf("memory_diagnose tool not found in registry")
	}

	// 1. Store valid memory
	res, err := storeTool.Function(ctx, map[string]interface{}{
		"key":      "arch:storage:wal",
		"content":  "Always use SetMaxOpenConns(1) with SQLite WAL.",
		"category": "constraint",
		"tags":     []interface{}{"sqlite", "wal"},
	})
	if err != nil {
		t.Fatalf("memory_store failed: %v", err)
	}
	if !strings.Contains(res, "Stored memory") {
		t.Errorf("unexpected store output: %s", res)
	}

	// Verify session and node ID were attached
	mem, err := store.GetMemory("workspace", "session-42", "arch:storage:wal")
	if err != nil || mem == nil {
		t.Fatalf("failed to get stored memory: %v", err)
	}
	if mem.SessionID != "session-42" {
		t.Errorf("expected SessionID 'session-42', got %q", mem.SessionID)
	}
	if mem.SourceNodeID != "node-100" {
		t.Errorf("expected SourceNodeID 'node-100', got %q", mem.SourceNodeID)
	}

	// 2. Secret token quarantine rejection (ADR 011)
	_, err = storeTool.Function(ctx, map[string]interface{}{
		"key":      "secret:token",
		"content":  "Here is my token: ghp_123456789012345678901234567890123456",
		"category": "fact",
	})
	if err == nil {
		t.Fatalf("expected error storing secret token, got nil")
	}
	if !strings.Contains(err.Error(), "security violation (ADR 011)") {
		t.Errorf("expected ADR 011 security violation, got: %v", err)
	}

	// 3. Recall memory
	recallRes, err := recallTool.Function(ctx, map[string]interface{}{
		"query": "SetMaxOpenConns",
	})
	if err != nil {
		t.Fatalf("memory_recall failed: %v", err)
	}
	if !strings.Contains(recallRes, "arch:storage:wal") {
		t.Errorf("expected recall to find arch:storage:wal, got: %s", recallRes)
	}

	// 4. Diagnose memory bank
	diagRes, err := diagnoseTool.Function(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("memory_diagnose failed: %v", err)
	}
	if !strings.Contains(diagRes, "Total Memories: 1") {
		t.Errorf("expected 1 total memory in diagnose output, got: %s", diagRes)
	}

	// 5. Delete memory
	delRes, err := deleteTool.Function(ctx, map[string]interface{}{
		"key": "arch:storage:wal",
	})
	if err != nil {
		t.Fatalf("memory_delete failed: %v", err)
	}
	if !strings.Contains(delRes, "Deleted memory") {
		t.Errorf("unexpected delete output: %s", delRes)
	}

	// Verify recall after delete returns empty
	recallAfterDel, err := recallTool.Function(ctx, map[string]interface{}{
		"key": "arch:storage:wal",
	})
	if err != nil {
		t.Fatalf("memory_recall after delete failed: %v", err)
	}
	if !strings.Contains(recallAfterDel, "No memories found") {
		t.Errorf("expected no memories found, got: %s", recallAfterDel)
	}
}
