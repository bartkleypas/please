package tools

// GetDefaultTools returns a slice of standard built-in tools scoped to workspaceDir.
func GetDefaultTools(workspaceDir ...string) []Tool {
	ws := "."
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
	}

	return []Tool{
		ReadFileTool(ws),
		WriteFileTool(ws),
		AppendFileTool(ws),
		ListDirectoryTool(ws),
		GrepSearchTool(ws),
		ExecuteCommandTool(ws),
		EditFileTool(ws),
		ListFilesRecursiveTool(ws),
	}
}

// RegisterDefaultTools registers all default tools into the provided registry scoped to workspaceDir.
func RegisterDefaultTools(registry *ToolRegistry, workspaceDir ...string) {
	ws := "."
	if len(workspaceDir) > 0 && workspaceDir[0] != "" {
		ws = workspaceDir[0]
	}

	for _, t := range GetDefaultTools(ws) {
		registry.Register(t)
	}
}
