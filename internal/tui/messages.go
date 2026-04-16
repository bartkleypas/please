package tui

// tickMsg is sent to trigger the spinner animation
type tickMsg struct{}

// llmStreamMsg is sent for each chunk of content in a streaming response
type llmStreamMsg struct {
	content  string
	parentID string
}

// llmStreamFinishedMsg is sent when a streaming response completes
type llmStreamFinishedMsg struct {
	err      error
	parentID string
}

// exportResultMsg is sent when the export file operation completes
type exportResultMsg struct {
	filename string
	err      error
}
