package bad

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// testifylint: comparing against a boolean constant has a dedicated assertion.
func TestTestify(t *testing.T) {
	assert.Equal(t, true, Simple(true) == 1)
}
