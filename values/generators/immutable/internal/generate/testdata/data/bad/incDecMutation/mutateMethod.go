package data

//go:generate immutable -type Generic

// Generic comment.
type Generic struct {
	// Count comment.
	Count uint64
	Name  string
}

// Inc mutates a field via the increment operator (r.Count++), which detectFieldMutation must catch
// just like a plain assignment, so an immutable version cannot be generated.
func (g *Generic) Inc() {
	g.Count++
}
