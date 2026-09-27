package buildreceivers

// The build without tests and the build with tests both check this file. The
// build with tests sees the pointer receiver in receivers_test.go and reports
// the mix; the build without tests leaves the directive to it.

//lint:ignore recvcheck the test helper resets a Tally in place
type Tally struct { // want `is judged in the build with tests|use pointer receiver and non-pointer receiver`
	count int
}

func (t Tally) Count() int {
	return t.count
}

// Mixed mixes receivers in this file, so both builds report it.
type Mixed struct { // want `use pointer receiver and non-pointer receiver`
	count int
}

func (m Mixed) Count() int {
	return m.count
}

func (m *Mixed) Add() {
	m.count++
}

// Staticcheck applies no directive without a reason and reports it as
// malformed, so the build without tests does not match it either.
//
//lint:ignore recvcheck
type Unexplained struct { // want `use pointer receiver and non-pointer receiver`
	count int
}

func (u Unexplained) Count() int {
	return u.count
}

func (u *Unexplained) Add() {
	u.count++
}
