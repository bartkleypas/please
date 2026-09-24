package tui

import (
	"github.com/bartkleypas/please/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

// MemoriesDeckLayer represents the full-screen browseable memory deck on the ViewStack.
type MemoriesDeckLayer struct {
	m *Model
}

func newMemoriesDeckLayer(m *Model) *MemoriesDeckLayer {
	return &MemoriesDeckLayer{m: m}
}

func (l *MemoriesDeckLayer) setModel(m *Model) {
	l.m = m
}

func (l *MemoriesDeckLayer) Name() string {
	return "memories"
}

func (l *MemoriesDeckLayer) IsOverlay() bool {
	return false
}

func (l *MemoriesDeckLayer) View(width, height int) string {
	if l.m == nil {
		return ""
	}
	return l.m.View()
}

func (l *MemoriesDeckLayer) Update(msg tea.Msg) (tea.Cmd, bool) {
	if l.m == nil {
		return nil, false
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	newM, cmd := l.m.handleMemoriesKeys(keyMsg)
	l.m = newM

	// If a detail card was opened, push the card overlay
	if l.m.MemoryDetailCard != nil && l.m.ViewStack != nil && l.m.ViewStack.Top() == l {
		l.m.ViewStack.Push(newMemoryCardOverlay(l.m, l.m.MemoryDetailCard))
	}

	return cmd, true
}

// MemoryCardOverlay represents a focused inspection card floating over the deck.
type MemoryCardOverlay struct {
	m    *Model
	card *storage.Memory
}

func newMemoryCardOverlay(m *Model, card *storage.Memory) *MemoryCardOverlay {
	return &MemoryCardOverlay{m: m, card: card}
}

func (l *MemoryCardOverlay) setModel(m *Model) {
	l.m = m
}

func (l *MemoryCardOverlay) Name() string {
	return "memory_card"
}

func (l *MemoryCardOverlay) IsOverlay() bool {
	return true
}

func (l *MemoryCardOverlay) View(width, height int) string {
	if l.m == nil || l.card == nil {
		return ""
	}
	return l.m.renderMemoryCard(l.card)
}

func (l *MemoryCardOverlay) Update(msg tea.Msg) (tea.Cmd, bool) {
	if l.m == nil {
		return nil, false
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	newM, cmd := l.m.handleMemoriesKeys(keyMsg)
	l.m = newM

	// If card was dismissed (MemoryDetailCard set to nil), pop overlay back to deck
	if l.m.MemoryDetailCard == nil && l.m.ViewStack != nil && l.m.ViewStack.Top() == l {
		l.m.ViewStack.Pop()
	}

	return cmd, true
}
