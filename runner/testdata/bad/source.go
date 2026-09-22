package bad

// bidichk: the right-to-left override in the trailing comment reorders how
// the rest of the line displays, so a reviewer does not see the bytes the
// compiler reads. ST1018 covers string literals only.
const Role = "user" // ‮"nimda"

// gocheckcompilerdirectives: go generate and the compiler skip a directive
// they do not know, so this misspelled line never runs.
//go:genrate echo generated
