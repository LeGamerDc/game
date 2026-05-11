package combat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDelayUntilTickCeilsToSchedulerDelay(t *testing.T) {
	require.Equal(t, int64(0), DelayUntilTick(10, TickToSeconds(10)))
	require.Equal(t, int64(1), DelayUntilTick(10, TickToSeconds(10)+0.001))
	require.Equal(t, int64(2), DelayUntilTick(10, TickToSeconds(10)+0.25))
}
