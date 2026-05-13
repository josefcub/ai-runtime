package tools

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestRegister(t *testing.T) {
	reg := New("/tmp/test-work")

	reg.Register("echo", "Echoes input back", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{"type": "string"},
		},
	}, func(args map[string]interface{}) (string, error) {
		msg, _ := args["message"].(string)
		return msg, nil
	})

	if _, ok := reg.tools["echo"]; !ok {
		t.Fatal("expected 'echo' to be registered")
	}
}

func TestDispatch(t *testing.T) {
	reg := New("/tmp/test-work")

	reg.Register("add", "Add two numbers", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "integer"},
			"b": map[string]interface{}{"type": "integer"},
		},
	}, func(args map[string]interface{}) (string, error) {
		a, _ := args["a"].(float64)
		b, _ := args["b"].(float64)
		return string(rune('0' + int(a+b))), nil
	})

	argsJSON := `{"a": 3, "b": 4}`
	result, err := reg.Dispatch("add", argsJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "7" {
		t.Errorf("expected '7', got %q", result)
	}
}

func TestDispatch_UnknownTool(t *testing.T) {
	reg := New("/tmp/test-work")

	_, err := reg.Dispatch("nonexistent", `{}`)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDispatch_InvalidArgs(t *testing.T) {
	reg := New("/tmp/test-work")

	reg.Register("dummy", "dummy", map[string]interface{}{}, func(args map[string]interface{}) (string, error) {
		return "ok", nil
	})

	_, err := reg.Dispatch("dummy", `{invalid json}`)
	if err == nil {
		t.Fatal("expected error for invalid JSON arguments")
	}
}

func TestDefinitions(t *testing.T) {
	reg := New("/tmp/test-work")

	reg.Register("foo", "Does foo", map[string]interface{}{
		"type": "object",
	}, func(args map[string]interface{}) (string, error) {
		return "", nil
	})

	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}

	if defs[0].Type != "function" {
		t.Errorf("expected type 'function', got %q", defs[0].Type)
	}

	var schema FunctionSchema
	if err := json.Unmarshal(defs[0].Function, &schema); err != nil {
		t.Fatalf("unmarshal function schema: %v", err)
	}
	if schema.Name != "foo" {
		t.Errorf("expected name 'foo', got %q", schema.Name)
	}
}

func TestDispatch_ErrorPropagation(t *testing.T) {
	reg := New("/tmp/test-work")

	reg.Register("fail", "Always fails", map[string]interface{}{}, func(args map[string]interface{}) (string, error) {
		return "", fmt.Errorf("tool error")
	})

	_, err := reg.Dispatch("fail", `{}`)
	if err == nil {
		t.Fatal("expected error from tool execution")
	}
	if err.Error() != "tool error" {
		t.Errorf("expected 'tool error', got %v", err)
	}
}
