package tui

// ListNavigator encapsulates boundary-checked cursor movement for lists, decks, and scrollable views.
type ListNavigator struct {
	Cursor   int
	Total    int
	PageSize int
}

// NewListNavigator creates a ListNavigator with the specified total items and page size.
func NewListNavigator(total, pageSize int) *ListNavigator {
	if pageSize <= 0 {
		pageSize = 10
	}
	nav := &ListNavigator{
		Cursor:   0,
		Total:    total,
		PageSize: pageSize,
	}
	nav.Clamp()
	return nav
}

// SetTotal updates the total count and clamps the cursor within bounds [0, max(0, total-1)].
func (n *ListNavigator) SetTotal(total int) {
	n.Total = total
	n.Clamp()
}

// Clamp ensures the cursor remains strictly within valid bounds [0, max(0, total-1)].
func (n *ListNavigator) Clamp() {
	if n.Total <= 0 {
		n.Cursor = 0
		return
	}
	if n.Cursor < 0 {
		n.Cursor = 0
	}
	if n.Cursor >= n.Total {
		n.Cursor = n.Total - 1
	}
}

// Next moves the cursor forward by 1, stopping at the last item. Returns true if cursor moved.
func (n *ListNavigator) Next() bool {
	if n.Total <= 0 || n.Cursor >= n.Total-1 {
		return false
	}
	n.Cursor++
	return true
}

// Prev moves the cursor backward by 1, stopping at the first item. Returns true if cursor moved.
func (n *ListNavigator) Prev() bool {
	if n.Total <= 0 || n.Cursor <= 0 {
		return false
	}
	n.Cursor--
	return true
}

// First jumps cursor to the beginning (0). Returns true if cursor moved.
func (n *ListNavigator) First() bool {
	if n.Cursor == 0 {
		return false
	}
	n.Cursor = 0
	return true
}

// Last jumps cursor to the end (Total - 1). Returns true if cursor moved.
func (n *ListNavigator) Last() bool {
	if n.Total <= 0 {
		return false
	}
	target := n.Total - 1
	if n.Cursor == target {
		return false
	}
	n.Cursor = target
	return true
}

// PageDown advances cursor by PageSize, clamping to the end. Returns true if cursor moved.
func (n *ListNavigator) PageDown() bool {
	if n.Total <= 0 || n.Cursor >= n.Total-1 {
		return false
	}
	n.Cursor += n.PageSize
	if n.Cursor >= n.Total {
		n.Cursor = n.Total - 1
	}
	return true
}

// PageUp retreats cursor by PageSize, clamping to the start. Returns true if cursor moved.
func (n *ListNavigator) PageUp() bool {
	if n.Total <= 0 || n.Cursor <= 0 {
		return false
	}
	n.Cursor -= n.PageSize
	if n.Cursor < 0 {
		n.Cursor = 0
	}
	return true
}

// HandleKey processes standard navigation keys:
// - Up: "up", "k"
// - Down: "down", "j"
// - Top: "home", "g"
// - Bottom: "end", "G"
// - Page Up: "pgup", "ctrl+u", "ctrl+b"
// - Page Down: "pgdown", "ctrl+d", "ctrl+f"
// Returns true if the key was recognized as a navigation action.
func (n *ListNavigator) HandleKey(key string) bool {
	switch key {
	case "up", "k":
		return n.Prev()
	case "down", "j":
		return n.Next()
	case "home", "g":
		return n.First()
	case "end", "G":
		return n.Last()
	case "pgup", "ctrl+u", "ctrl+b":
		return n.PageUp()
	case "pgdown", "ctrl+d", "ctrl+f":
		return n.PageDown()
	default:
		return false
	}
}
