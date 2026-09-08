package grid2

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeLifecycleOptions_keepWithoutRecover(t *testing.T) {
	s := &Strategy{
		Symbol:                 "HYPEUSDT",
		KeepOrdersWhenShutdown: true,
		RecoverOrdersWhenStart: false,
		logger:                 logrus.New().WithField("test", true),
	}
	s.sanitizeLifecycleOptions()
	assert.True(t, s.RecoverOrdersWhenStart)
	assert.True(t, s.ClearDuplicatedPriceOpenOrders)
}

func TestSanitizeLifecycleOptions_keepWithClearStart(t *testing.T) {
	s := &Strategy{
		Symbol:                   "HYPEUSDT",
		KeepOrdersWhenShutdown:   true,
		RecoverOrdersWhenStart:   true,
		ClearOpenOrdersWhenStart: true,
		logger:                   logrus.New().WithField("test", true),
	}
	s.sanitizeLifecycleOptions()
	assert.True(t, s.RecoverOrdersWhenStart)
	assert.False(t, s.ClearOpenOrdersWhenStart)
}

func TestSanitizeLifecycleOptions_noopWhenConsistent(t *testing.T) {
	s := &Strategy{
		Symbol:                 "HYPEUSDT",
		KeepOrdersWhenShutdown: true,
		RecoverOrdersWhenStart: true,
		logger:                 logrus.New().WithField("test", true),
	}
	s.sanitizeLifecycleOptions()
	assert.True(t, s.RecoverOrdersWhenStart)
	assert.False(t, s.ClearDuplicatedPriceOpenOrders)
}
