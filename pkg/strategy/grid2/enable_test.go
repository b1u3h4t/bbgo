package grid2

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStrategy_isEnabled(t *testing.T) {
	s := &Strategy{Symbol: "ENAUSDT"}
	assert.True(t, s.isEnabled(), "omitted enable defaults to true")

	off := false
	s.Enable = &off
	assert.False(t, s.isEnabled())

	on := true
	s.Enable = &on
	assert.True(t, s.isEnabled())
}
