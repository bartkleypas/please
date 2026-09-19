package tui

import (
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// BellWriter defines where the terminal bell character (ASCII 0x07 / \a) is written.
// Defaults to os.Stderr to avoid polluting Lipgloss/BubbleTea stdout buffers,
// and can be redirected in unit tests (e.g. to bytes.Buffer or io.Discard) to keep tests hermetic and silent.
var BellWriter io.Writer = os.Stderr

// BellCmd emits an ASCII 0x07 (BEL) character to signal turn completion or human consent required.
func BellCmd() tea.Cmd {
	return func() tea.Msg {
		if BellWriter != nil {
			_, _ = BellWriter.Write([]byte("\a"))
		}
		return nil
	}
}
