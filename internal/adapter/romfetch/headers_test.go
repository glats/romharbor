package romfetch

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeadersSetsEveryValueAndReplacesExistingOnes(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://provider.invalid/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "*/*")

	err = Headers(map[string]string{"Accept": "text/html", "User-Agent": "ROMHarbor"})(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "text/html", req.Header.Get("Accept"))
	assert.Equal(t, "ROMHarbor", req.Header.Get("User-Agent"))
}

func TestHeadersWithoutValuesLeavesTheRequestUntouched(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://provider.invalid/", nil)

	require.NoError(t, Headers(nil)(context.Background(), req))

	assert.Empty(t, req.Header)
}
