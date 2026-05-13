package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBasic(t *testing.T) {
	data := []byte(`[section1]
key1 = value1
key2 = value2
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section1"]["key1"] != "value1" {
		t.Errorf("expected section1.key1 = value1, got %q", result["section1"]["key1"])
	}
	if result["section1"]["key2"] != "value2" {
		t.Errorf("expected section1.key2 = value2, got %q", result["section1"]["key2"])
	}
}

func TestParseComments(t *testing.T) {
	data := []byte(`# This is a comment
; This is also a comment
[section]
key = value # inline comment
key2 = value2 ; another inline comment
key3 = value3
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section"]["key"] != "value" {
		t.Errorf("expected key = value, got %q", result["section"]["key"])
	}
	if result["section"]["key2"] != "value2" {
		t.Errorf("expected key2 = value2, got %q", result["section"]["key2"])
	}
	if result["section"]["key3"] != "value3" {
		t.Errorf("expected key3 = value3, got %q", result["section"]["key3"])
	}
}

func TestParseMultiline(t *testing.T) {
	data := []byte(`[section]
prompt = """
This is a
multiline value
"""
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "This is a\nmultiline value"
	if result["section"]["prompt"] != expected {
		t.Errorf("expected multiline value, got %q", result["section"]["prompt"])
	}
}

func TestParseMultilineSameLine(t *testing.T) {
	data := []byte(`[section]
prompt = """single line triple-quoted"""
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section"]["prompt"] != "single line triple-quoted" {
		t.Errorf("expected single line value, got %q", result["section"]["prompt"])
	}
}

func TestParseBlankLines(t *testing.T) {
	data := []byte(`[section]

key = value


key2 = value2

`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section"]["key"] != "value" {
		t.Errorf("expected key = value, got %q", result["section"]["key"])
	}
}

func TestParseTopLevelKeys(t *testing.T) {
	data := []byte(`topkey = toplevel
[section]
key = value
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result[""]["topkey"] != "toplevel" {
		t.Errorf("expected top-level key, got %q", result[""]["topkey"])
	}
}

func TestParseQuotedValues(t *testing.T) {
	data := []byte(`[section]
key = "quoted value"
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section"]["key"] != "quoted value" {
		t.Errorf("expected quoted value, got %q", result["section"]["key"])
	}
}

func TestParseQuotedValueWithComment(t *testing.T) {
	// Quoted values should preserve content even if there's a # inside
	data := []byte(`[section]
key = "value with # hash"
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section"]["key"] != "value with # hash" {
		t.Errorf("expected quoted value with hash, got %q", result["section"]["key"])
	}
}

func TestParseUnclosedMultiline(t *testing.T) {
	data := []byte(`[section]
key = """
unclosed multiline
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for unclosed multiline value")
	}
	if !strings.Contains(err.Error(), "unclosed") {
		t.Errorf("expected 'unclosed' in error, got: %v", err)
	}
}

func TestParseFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.ini")
	content := `[section]
key = value
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["section"]["key"] != "value" {
		t.Errorf("expected key = value, got %q", result["section"]["key"])
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/path/config.ini")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseMultilineWithContentOnOpenLine(t *testing.T) {
	data := []byte(`[section]
key = """first line
second line
last line"""
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "first line\nsecond line\nlast line"
	if result["section"]["key"] != expected {
		t.Errorf("expected %q, got %q", expected, result["section"]["key"])
	}
}

func TestParseMultipleSections(t *testing.T) {
	data := []byte(`[section1]
key = one

[section2]
key = two
`)
	result, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["section1"]["key"] != "one" {
		t.Errorf("expected section1.key = one, got %q", result["section1"]["key"])
	}
	if result["section2"]["key"] != "two" {
		t.Errorf("expected section2.key = two, got %q", result["section2"]["key"])
	}
}

func TestStripInlineComment(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"value # comment", "value"},
		{"value ; comment", "value"},
		{"value", "value"},
		{"\"value # not comment\"", "\"value # not comment\""},
		{"\"value # not comment\"  # trailing comment", "\"value # not comment\""},
		{"", ""},
		{"# not a value", ""},
	}

	for _, tc := range tests {
		result := stripInlineComment(tc.input)
		if result != tc.expected {
			t.Errorf("stripInlineComment(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\"quoted\"", "quoted"},
		{"unquoted", "unquoted"},
		{"\"", "\""},
		{"", ""},
	}

	for _, tc := range tests {
		result := stripQuotes(tc.input)
		if result != tc.expected {
			t.Errorf("stripQuotes(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
