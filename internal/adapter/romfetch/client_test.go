package romfetch

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientBoundsEveryPhaseButTheBodyWhenNoTimeoutIsGiven(t *testing.T) {
	client := NewClient(0)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Zero(t, client.Timeout, "a long download must only be bounded by its context")
	assert.Positive(t, transport.ResponseHeaderTimeout)
	assert.Positive(t, transport.TLSHandshakeTimeout)
	assert.NotNil(t, transport.DialContext)
}

func TestNewClientAppliesAnOverallTimeout(t *testing.T) {
	assert.Equal(t, 45*time.Second, NewClient(45*time.Second).Timeout)
}

func TestEachClientOwnsItsTransport(t *testing.T) {
	assert.NotSame(t, NewClient(0).Transport, NewClient(0).Transport)
}
