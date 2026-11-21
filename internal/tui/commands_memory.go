package tui

import (
	"fmt"
	"strings"

	"github.com/bartkleypas/please/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

type MemoriesCommand struct{}

func (c *MemoriesCommand) Execute(m *Model, args []string) (tea.Model, tea.Cmd) {
	if m.Manager == nil || m.Manager.Storage == nil {
		m.Notification = "Storage not initialized"
		return m, nil
	}

	memStore, ok := m.Manager.Storage.(storage.MemoryStore)
	if !ok {
		m.Notification = "Persistent agent memory requires SQLite storage (.db)"
		return m, nil
	}

	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "diag", "diagnose", "stats":
		diag, err := memStore.DiagnoseMemories("", "")
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to diagnose memories: %v", err)
			return m, nil
		}
		var sb strings.Builder
		sb.WriteString("--- 🧠 Memory Vault Telemetry & Health (ADR 014) ---\n\n")
		sb.WriteString(fmt.Sprintf("Total Memories:    %d\n", diag.TotalMemories))
		sb.WriteString(fmt.Sprintf("Storage Footprint: %.2f KB (%d bytes)\n\n", float64(diag.StorageBytes)/1024.0, diag.StorageBytes))

		sb.WriteString("Scope Distribution:\n")
		for sc, count := range diag.ByScope {
			sb.WriteString(fmt.Sprintf("  • %-12s: %d\n", sc, count))
		}

		sb.WriteString("\nCategory Distribution:\n")
		for cat, count := range diag.ByCategory {
			sb.WriteString(fmt.Sprintf("  • %-14s: %d\n", cat, count))
		}

		if len(diag.MostAccessed) > 0 {
			sb.WriteString("\n🔥 Most Accessed Memories:\n")
			for i, mem := range diag.MostAccessed {
				if i >= 5 {
					break
				}
				sb.WriteString(fmt.Sprintf("  %d. %s (%s, %d accesses)\n", i+1, mem.Key, mem.Scope, mem.AccessCount))
			}
		}

		m.ensureViewStack()
		m.ViewStack.Push(NewTextViewLayer(m, "memories_stats", "Memory Diagnostic Telemetry", sb.String(), ""))
		return m, nil

	case "inspect", "show", "get":
		if len(args) < 2 {
			m.Notification = "Usage: /memories inspect <key>"
			return m, nil
		}
		key := args[1]
		mem, _ := memStore.GetMemory(storage.ScopeWorkspace, m.SessionID, key)
		if mem == nil {
			mem, _ = memStore.GetMemory(storage.ScopeGlobal, "", key)
		}
		if mem == nil {
			// Fuzzy / prefix search
			candidates, _ := memStore.QueryMemories(storage.MemoryFilter{Limit: 100})
			lower := strings.ToLower(key)
			for _, c := range candidates {
				if strings.Contains(strings.ToLower(c.Key), lower) {
					found := c
					mem = &found
					break
				}
			}
		}
		if mem == nil {
			m.Notification = fmt.Sprintf("Memory %q not found", key)
			return m, nil
		}

		m.ViewMode = ModeMemories
		m.MemoryDeck = []storage.Memory{*mem}
		m.MemoryDeckIndex = 0
		m.MemoryDetailCard = mem
		m.Viewport.SetContent(m.renderMemoriesView())
		m.Viewport.GotoTop()
		m.ensureViewStack()
		if m.ViewStack.Top().Name() != "memories" {
			m.ViewStack.Push(newMemoriesDeckLayer(m))
		}
		if m.ViewStack.Top().Name() != "memory_card" {
			m.ViewStack.Push(newMemoryCardOverlay(m, mem))
		}
		return m, nil

	default:
		filter := storage.MemoryFilter{Limit: 100}
		if sub == "search" || sub == "find" || sub == "q" {
			if len(args) > 1 {
				filter.Query = strings.Join(args[1:], " ")
			}
		} else if len(args) > 0 && !strings.HasPrefix(args[0], "/") {
			filter.Query = strings.Join(args, " ")
		}

		mems, err := memStore.QueryMemories(filter)
		if err != nil {
			m.Notification = fmt.Sprintf("Failed to query memories: %v", err)
			return m, nil
		}

		m.ViewMode = ModeMemories
		m.MemoryDeck = mems
		m.MemoryDeckIndex = 0
		m.MemoryDetailCard = nil
		m.MemoryDeckFilter = filter.Query
		m.Viewport.SetContent(m.renderMemoriesView())
		m.Viewport.GotoTop()
		m.ensureViewStack()
		if m.ViewStack.Top().Name() != "memories" {
			m.ViewStack.Push(newMemoriesDeckLayer(m))
		}
		return m, nil
	}
}
