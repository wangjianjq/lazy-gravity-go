package tts

import (
	"testing"
)

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Bold and Italic",
			input:    "This is **bold** and *italic* text.",
			expected: "This is bold and italic text.",
		},
		{
			name:     "Links",
			input:    "Check out [OpenAI](https://openai.com).",
			expected: "Check out OpenAI.",
		},
		{
			name:     "Code Blocks",
			input:    "Code:\n```go\nfmt.Println(\"Hello\")\n```\nDone.",
			expected: "Code:\n\nDone.", // Because of how newlines might be handled, let's see. My regex replaces ```...``` with ""
		},
		{
			name:     "Inline code",
			input:    "Use the `print` function.",
			expected: "Use the print function.",
		},
		{
			name:     "Headers",
			input:    "### Header 3\nText",
			expected: "Header 3\nText",
		},
		{
			name:     "Unordered Lists",
			input:    "- Item 1\n- Item 2",
			expected: "Item 1\nItem 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripMarkdown(tt.input)
			// Relaxing exact newline match for code blocks, just checking substring mostly
			if result != tt.expected {
				t.Errorf("StripMarkdown() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSmartTruncate(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		expected string
	}{
		{
			name:     "Short text",
			text:     "Hello world.",
			maxLen:   50,
			expected: "Hello world.",
		},
		{
			name:     "Truncate at period",
			text:     "This is sentence one. This is sentence two. This is sentence three.",
			maxLen:   50,
			expected: "This is sentence one. This is sentence two.",
		},
		{
			name:     "Strict truncate without period in second half",
			text:     "A very long sequence of words without any punctuation marks whatsoever",
			maxLen:   25,
			expected: "A very long sequence of w……",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SmartTruncate(tt.text, tt.maxLen)
			if result != tt.expected {
				t.Errorf("SmartTruncate() = %q, want %q", result, tt.expected)
			}
		})
	}
}
