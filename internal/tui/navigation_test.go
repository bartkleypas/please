package tui

import "testing"

func TestListNavigator_BoundaryAndClamping(t *testing.T) {
	nav := NewListNavigator(5, 3)

	if nav.Cursor != 0 {
		t.Fatalf("expected initial cursor 0, got %d", nav.Cursor)
	}

	// Prev at 0 returns false
	if nav.Prev() {
		t.Errorf("expected Prev() at index 0 to return false")
	}
	if nav.Cursor != 0 {
		t.Errorf("expected cursor to remain at 0, got %d", nav.Cursor)
	}

	// Move forward to end
	for i := 0; i < 4; i++ {
		if !nav.Next() {
			t.Errorf("expected Next() at %d to succeed", i)
		}
	}
	if nav.Cursor != 4 {
		t.Fatalf("expected cursor at 4, got %d", nav.Cursor)
	}

	// Next at end returns false
	if nav.Next() {
		t.Errorf("expected Next() at end to return false")
	}
	if nav.Cursor != 4 {
		t.Errorf("expected cursor to remain at 4, got %d", nav.Cursor)
	}

	// First / Last
	if !nav.First() {
		t.Errorf("expected First() to return true when cursor at 4")
	}
	if nav.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", nav.Cursor)
	}
	if nav.First() {
		t.Errorf("expected First() to return false when already at 0")
	}

	if !nav.Last() {
		t.Errorf("expected Last() to return true when cursor at 0")
	}
	if nav.Cursor != 4 {
		t.Errorf("expected cursor 4, got %d", nav.Cursor)
	}

	// Page down / Page up
	nav.First()
	if !nav.PageDown() {
		t.Errorf("expected PageDown() to succeed")
	}
	if nav.Cursor != 3 { // Page size 3
		t.Errorf("expected cursor 3 after PageDown(), got %d", nav.Cursor)
	}
	if !nav.PageDown() {
		t.Errorf("expected second PageDown() to clamp to end")
	}
	if nav.Cursor != 4 {
		t.Errorf("expected cursor clamped to 4, got %d", nav.Cursor)
	}

	if !nav.PageUp() {
		t.Errorf("expected PageUp() to succeed")
	}
	if nav.Cursor != 1 { // 4 - 3 = 1
		t.Errorf("expected cursor 1 after PageUp(), got %d", nav.Cursor)
	}

	// SetTotal and clamping
	nav.SetTotal(2) // Cursor was 1, total now 2 -> cursor remains 1
	if nav.Cursor != 1 {
		t.Errorf("expected cursor 1, got %d", nav.Cursor)
	}
	nav.SetTotal(1) // Cursor was 1, total now 1 -> cursor clamped to 0
	if nav.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", nav.Cursor)
	}
	nav.SetTotal(0) // Empty
	if nav.Cursor != 0 {
		t.Errorf("expected cursor 0 for empty list, got %d", nav.Cursor)
	}
}

func TestListNavigator_HandleKey(t *testing.T) {
	nav := NewListNavigator(10, 5)

	// Down / j
	if !nav.HandleKey("j") || nav.Cursor != 1 {
		t.Errorf("expected j to move cursor to 1, got %d", nav.Cursor)
	}
	if !nav.HandleKey("down") || nav.Cursor != 2 {
		t.Errorf("expected down to move cursor to 2, got %d", nav.Cursor)
	}

	// Up / k
	if !nav.HandleKey("k") || nav.Cursor != 1 {
		t.Errorf("expected k to move cursor to 1, got %d", nav.Cursor)
	}
	if !nav.HandleKey("up") || nav.Cursor != 0 {
		t.Errorf("expected up to move cursor to 0, got %d", nav.Cursor)
	}

	// Bottom / G
	if !nav.HandleKey("G") || nav.Cursor != 9 {
		t.Errorf("expected G to move cursor to 9, got %d", nav.Cursor)
	}

	// Top / g
	if !nav.HandleKey("g") || nav.Cursor != 0 {
		t.Errorf("expected g to move cursor to 0, got %d", nav.Cursor)
	}

	// Page down / ctrl+d
	if !nav.HandleKey("ctrl+d") || nav.Cursor != 5 {
		t.Errorf("expected ctrl+d to move cursor to 5, got %d", nav.Cursor)
	}

	// Page up / ctrl+u
	if !nav.HandleKey("ctrl+u") || nav.Cursor != 0 {
		t.Errorf("expected ctrl+u to move cursor to 0, got %d", nav.Cursor)
	}

	// Unrelated key
	if nav.HandleKey("enter") {
		t.Errorf("expected enter not to be handled by ListNavigator")
	}
}
