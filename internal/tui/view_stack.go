package tui

import tea "github.com/charmbracelet/bubbletea"

// ViewLayer represents an isolated visual surface and event handler on the ViewStack.
type ViewLayer interface {
	// Name returns a human-readable identifier for diagnostics and telemetry.
	Name() string

	// Update handles Bubble Tea messages when this layer resides at the top of the stack.
	// Returns a command and a boolean indicating whether the message was consumed.
	Update(msg tea.Msg) (tea.Cmd, bool)

	// View renders the visual output of this layer for the specified dimensions.
	View(width, height int) string

	// IsOverlay returns true if this layer renders as a floating modal on top of
	// the underlying layer, or false if it completely replaces the screen.
	IsOverlay() bool
}

// ViewStack manages a LIFO stack of ViewLayers, coordinating input dispatch and rendering hierarchy.
type ViewStack struct {
	layers []ViewLayer
}

// NewViewStack initializes a stack anchored by a permanent root layer.
func NewViewStack(root ViewLayer) *ViewStack {
	if root == nil {
		panic("root layer cannot be nil")
	}
	return &ViewStack{layers: []ViewLayer{root}}
}

// Push adds a new layer to the top of the stack.
func (s *ViewStack) Push(layer ViewLayer) {
	if layer == nil {
		return
	}
	s.layers = append(s.layers, layer)
}

// Pop removes and returns the top layer. The permanent root layer cannot be popped.
func (s *ViewStack) Pop() ViewLayer {
	if len(s.layers) <= 1 {
		return nil
	}
	top := s.layers[len(s.layers)-1]
	s.layers = s.layers[:len(s.layers)-1]
	return top
}

// Top returns the active top layer that owns input focus.
func (s *ViewStack) Top() ViewLayer {
	if len(s.layers) == 0 {
		return nil
	}
	return s.layers[len(s.layers)-1]
}

// Len returns the current depth of the stack.
func (s *ViewStack) Len() int {
	return len(s.layers)
}

// Layers returns a copy of the current layers in the stack from bottom to top.
func (s *ViewStack) Layers() []ViewLayer {
	res := make([]ViewLayer, len(s.layers))
	copy(res, s.layers)
	return res
}

// LayerBelow returns the layer immediately beneath the specified layer, or nil if none exists.
// Useful for compositing floating overlay dialogs over their background screen.
func (s *ViewStack) LayerBelow(layer ViewLayer) ViewLayer {
	for i := len(s.layers) - 1; i > 0; i-- {
		if s.layers[i] == layer {
			return s.layers[i-1]
		}
	}
	return nil
}

// Update dispatches a message to the top layer.
// If the top layer does not consume an 'esc' key message, ViewStack automatically pops the layer.
func (s *ViewStack) Update(msg tea.Msg) (tea.Cmd, bool) {
	top := s.Top()
	if top == nil {
		return nil, false
	}

	cmd, handled := top.Update(msg)
	if handled {
		return cmd, true
	}

	// Universal Escape contract: if top layer didn't consume 'esc', pop it
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "esc" {
		if s.Len() > 1 {
			s.Pop()
			return nil, true
		}
	}

	return cmd, false
}
