package core

import "testing"

func TestName(t *testing.T) {
	if got := Name(" ada "); got != "ada" {
		t.Fatalf("Name = %q", got)
	}
}
