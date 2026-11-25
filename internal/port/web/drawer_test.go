package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheClosedDownloadsDrawerIsInertSoTabNeverReachesIt(t *testing.T) {
	body := uiGet(t, newCoverHandler(nil), "/").Body.String()

	assert.Contains(t, body, `id="downloads-panel"`)
	assert.Contains(t, body, `data-open="false" inert`)
}
