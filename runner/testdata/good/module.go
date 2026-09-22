package good

// Module takes pointer receivers in hand-written code; generated.go adds the
// value receiver a code generator emits, which recvcheck must not count.
type Module struct {
	calls int
}

// Call records one call.
func (m *Module) Call() int {
	m.calls++
	return m.calls
}
