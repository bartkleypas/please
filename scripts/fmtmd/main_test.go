package main

import (
	"testing"
)

func TestFormatMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		wantChanged bool
	}{
		{
			name: "unordered 3-space lists normalized",
			input: `*   [item1](doc1.md) - Description 1
*   [item2](doc2.md) - Description 2`,
			expected: `* [item1](doc1.md) - Description 1
* [item2](doc2.md) - Description 2`,
			wantChanged: true,
		},
		{
			name: "nested list indentation preserved",
			input: `* item
  *   nested item
    *   deeply nested`,
			expected: `* item
  * nested item
    * deeply nested`,
			wantChanged: true,
		},
		{
			name: "ordered list with multiple spaces",
			input: `1.   first item
2.   second item`,
			expected: `1. first item
2. second item`,
			wantChanged: true,
		},
		{
			name:        "code blocks are ignored",
			input:       "```markdown\n*   should not change\n```\n*   should change",
			expected:    "```markdown\n*   should not change\n```\n* should change",
			wantChanged: true,
		},
		{
			name: "frontmatter is ignored",
			input: `---
tags:
  -   tag1
---
*   should change`,
			expected: `---
tags:
  -   tag1
---
* should change`,
			wantChanged: true,
		},
		{
			name: "already standard list unchanged",
			input: `* standard item
- dash item
1. ordered item`,
			expected: `* standard item
- dash item
1. ordered item`,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := formatMarkdown(tt.input)
			if changed != tt.wantChanged {
				t.Fatalf("expected changed=%v, got %v", tt.wantChanged, changed)
			}
			if got != tt.expected {
				t.Fatalf("output mismatch:\nGot:\n%s\nExpected:\n%s", got, tt.expected)
			}
		})
	}
}
