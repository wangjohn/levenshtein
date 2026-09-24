package core

import (
	"net/http"
	"testing"
)

func TestName(t *testing.T) {
	if got := Name(" ada "); got != "ada" || http.StatusOK != 200 {
		t.Fatalf("Name = %q", got)
	}
}
