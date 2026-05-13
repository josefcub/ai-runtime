package tools

import (
	"encoding/json"
	"fmt"
)

// ToolDef is the OpenAI-compatible tool definition sent to the LLM.
type ToolDef struct {
	Type     string          `json:"type"`
	Function json.RawMessage `json:"function"`
}

// FunctionSchema is the JSON Schema for a tool's parameters.
type FunctionSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolFn is the execution function for a registered tool.
type ToolFn func(args map[string]interface{}) (string, error)

// ToolEntry holds the definition and execution function for a tool.
type ToolEntry struct {
	Definition ToolDef
	Execute    ToolFn
}

// Registry holds all registered tools and the sandbox working directory.
type Registry struct {
	workingDir string
	tools      map[string]ToolEntry
}

// New creates a new tool registry with the given working directory.
func New(workingDir string) *Registry {
	return &Registry{
		workingDir: workingDir,
		tools:      make(map[string]ToolEntry),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(name, description string, parameters map[string]interface{}, fn ToolFn) {
	schema := FunctionSchema{
		Name:        name,
		Description: description,
		Parameters:  parameters,
	}
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("tools: marshal schema for %s: %v", name, err))
	}

	r.tools[name] = ToolEntry{
		Definition: ToolDef{
			Type:     "function",
			Function: schemaJSON,
		},
		Execute: fn,
	}
}

// Definitions returns the list of tool definitions for sending to the LLM.
func (r *Registry) Definitions() []ToolDef {
	defs := make([]ToolDef, 0, len(r.tools))
	for _, entry := range r.tools {
		defs = append(defs, entry.Definition)
	}
	return defs
}

// Dispatch executes a tool by name with the given JSON-encoded arguments string
// (as returned by the LLM). Returns the tool result or an error.
func (r *Registry) Dispatch(name string, argumentsJSON string) (string, error) {
	entry, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments for %s: %v", name, err)
	}

	return entry.Execute(args)
}
