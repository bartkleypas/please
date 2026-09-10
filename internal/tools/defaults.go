package tools

// GetDefaultTools returns a slice of standard built-in tools scoped to workspaceDir.
// If a second argument is provided, it is treated as primaryWorkspace for path virtualization.
func GetDefaultTools(workspaceDir ...string) []Tool {
	ws, prim := parseWorkspaceArgs(workspaceDir...)

	return []Tool{
		ReadFileTool(ws, prim),
		WriteFileTool(ws, prim),
		AppendFileTool(ws, prim),
		ListDirectoryTool(ws, prim),
		GrepSearchTool(ws, prim),
		ExecuteCommandTool(ws),
		EditFileTool(ws, prim),
		ListFilesRecursiveTool(ws, prim),
	}
}

// RegisterDefaultTools registers all default tools into the provided registry scoped to workspaceDir.
func RegisterDefaultTools(registry *ToolRegistry, workspaceDir ...string) {
	for _, t := range GetDefaultTools(workspaceDir...) {
		registry.Register(t)
	}
}
