package engine

import (
	"github.com/bartkleypas/please/internal/providers"
	"github.com/bartkleypas/please/internal/tools"
)

// Re-export core tool types from internal/tools for backward compatibility.
type ToolCategory = tools.ToolCategory

const (
	CategorySensory = tools.CategorySensory
	CategoryMutate  = tools.CategoryMutate
	CategoryExecute = tools.CategoryExecute
)

type Tool = tools.Tool
type ToolRegistry = tools.ToolRegistry

var NewToolRegistry = tools.NewToolRegistry

// ToolCall represents a specific request from the LLM to run a tool
type ToolCall = providers.ToolCall
