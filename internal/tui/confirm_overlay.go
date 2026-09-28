package tui

import (
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmOverlay is an interactive modal layer on the ViewStack for yes/no approval gates.
type ConfirmOverlay struct {
	name      string
	title     string
	warning   string
	details   []string
	prompt    string
	stack     *ViewStack
	onConfirm func() tea.Cmd
	onCancel  func() tea.Cmd
}

// NewPruneConfirmOverlay creates a confirmation overlay for branch pruning.
func NewPruneConfirmOverlay(stack *ViewStack, nodeID string, onConfirm func() tea.Cmd, onCancel func() tea.Cmd) *ConfirmOverlay {
	return &ConfirmOverlay{
		name:      "confirm_prune",
		warning:   "PRUNE BRANCH: This will hide this node and all descendants.",
		prompt:    "Confirm pruning? (y/n)",
		stack:     stack,
		onConfirm: onConfirm,
		onCancel:  onCancel,
	}
}

// NewCompactConfirmOverlay creates a confirmation overlay for range compaction.
func NewCompactConfirmOverlay(stack *ViewStack, targetCount int, onConfirm func() tea.Cmd, onCancel func() tea.Cmd) *ConfirmOverlay {
	return &ConfirmOverlay{
		name:      "confirm_compact",
		warning:   fmt.Sprintf("COMPACT BRANCH: Summarize %d nodes into a Supernode?", targetCount),
		prompt:    "Confirm compaction? (y/n)",
		stack:     stack,
		onConfirm: onConfirm,
		onCancel:  onCancel,
	}
}

// NewToolConfirmOverlay creates a confirmation overlay for tool execution approval.
func NewToolConfirmOverlay(stack *ViewStack, toolCalls []domain.ToolCall, onConfirm func() tea.Cmd, onCancel func() tea.Cmd) *ConfirmOverlay {
	var details []string
	hasRedirection := false
	dangerChars := []string{">", "|", "&", "<"}

	for _, call := range toolCalls {
		args := string(call.Function.Arguments)
		for _, char := range dangerChars {
			if strings.Contains(args, char) {
				hasRedirection = true
				break
			}
		}
		details = append(details, fmt.Sprintf(" - %s(%s)", call.Function.Name, args))
	}

	var warning string
	if hasRedirection {
		warning = "CAUTION: Shell redirection, piping, or chaining detected!"
	}

	return &ConfirmOverlay{
		name:      "confirm_tool",
		title:     "Tool Call Confirmation Required:",
		warning:   warning,
		details:   details,
		prompt:    "Execute these tools? (y/n)",
		stack:     stack,
		onConfirm: onConfirm,
		onCancel:  onCancel,
	}
}

func (o *ConfirmOverlay) Name() string {
	return o.name
}

func (o *ConfirmOverlay) IsOverlay() bool {
	return true
}

func (o *ConfirmOverlay) View(width, height int) string {
	var sb strings.Builder
	if o.title != "" {
		sb.WriteString(markStyle.Render(o.title) + "\n")
	}
	for _, d := range o.details {
		sb.WriteString(d + "\n")
	}
	if o.warning != "" {
		sb.WriteString("\n" + warningStyle.Render(o.warning) + "\n")
	}
	if o.prompt != "" {
		sb.WriteString("\n" + markStyle.Render(o.prompt) + "\n")
	}
	return sb.String()
}

func (o *ConfirmOverlay) Update(msg tea.Msg) (tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	switch keyMsg.String() {
	case "y", "Y":
		if o.stack != nil {
			o.stack.Pop()
		}
		if o.onConfirm != nil {
			return o.onConfirm(), true
		}
		return nil, true
	case "n", "N", "esc":
		if o.stack != nil {
			o.stack.Pop()
		}
		if o.onCancel != nil {
			return o.onCancel(), true
		}
		return nil, true
	default:
		// Absorb all other keystrokes to strictly prevent key leakage into underlying views
		return nil, true
	}
}
