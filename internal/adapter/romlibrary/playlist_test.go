package romlibrary

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/domain/rom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
}

func readPlaylist(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func TestTheSecondDiscCreatesAPlaylistInDiscOrder(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Driver 2 (USA) (Disc 2)", "Driver 2 (USA) (Disc 2).cue"))
	touch(t, filepath.Join(dir, "Driver 2 (USA) (Disc 2)", "Driver 2 (USA) (Disc 2).bin"))
	touch(t, filepath.Join(dir, "Driver 2 (USA) (Disc 1)", "Driver 2 (USA) (Disc 1).bin"))
	touch(t, filepath.Join(dir, "Driver 2 (USA) (Disc 1)", "Driver 2 (USA) (Disc 1).cue"))

	require.NoError(t, writePlaylist(filepath.Join(dir, "Driver 2 (USA) (Disc 2)")))

	assert.Equal(t,
		"Driver 2 (USA) (Disc 1)/Driver 2 (USA) (Disc 1).cue\nDriver 2 (USA) (Disc 2)/Driver 2 (USA) (Disc 2).cue\n",
		readPlaylist(t, filepath.Join(dir, "Driver 2 (USA).m3u")))
}

func TestASingleInstalledDiscGetsNoPlaylist(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Driver 2 (USA) (Disc 1)", "Driver 2 (USA) (Disc 1).cue"))

	require.NoError(t, writePlaylist(filepath.Join(dir, "Driver 2 (USA) (Disc 1)")))

	assert.NoFileExists(t, filepath.Join(dir, "Driver 2 (USA).m3u"))
}

func TestAGameWithoutADiscTagGetsNoPlaylist(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Gran Turismo (USA).chd"))

	require.NoError(t, writePlaylist(filepath.Join(dir, "Gran Turismo (USA).chd")))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestPlainDiscFilesAreListedAndOtherRevisionsAreLeftOut(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Xenogears (USA) (Disc 1).chd"))
	touch(t, filepath.Join(dir, "Xenogears (USA) (Disc 2).chd"))
	touch(t, filepath.Join(dir, "Xenogears (USA) (Disc 1) (Rev 1).chd"))
	touch(t, filepath.Join(dir, "Other Game (USA) (Disc 1).chd"))

	require.NoError(t, writePlaylist(filepath.Join(dir, "Xenogears (USA) (Disc 2).chd")))

	assert.Equal(t, "Xenogears (USA) (Disc 1).chd\nXenogears (USA) (Disc 2).chd\n",
		readPlaylist(t, filepath.Join(dir, "Xenogears (USA).m3u")))
	assert.NoFileExists(t, filepath.Join(dir, "Xenogears (USA) (Rev 1).m3u"))
}

func TestARefreshAddsTheLaterDiscAndKeepsTheDiscsInPlace(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "Game (Europe) (Disc 1).chd"))
	touch(t, filepath.Join(dir, "Game (Europe) (Disc 2).chd"))
	require.NoError(t, writePlaylist(filepath.Join(dir, "Game (Europe) (Disc 2).chd")))
	touch(t, filepath.Join(dir, "Game (Europe) (Disc 3).chd"))

	require.NoError(t, writePlaylist(filepath.Join(dir, "Game (Europe) (Disc 3).chd")))

	assert.Equal(t, "Game (Europe) (Disc 1).chd\nGame (Europe) (Disc 2).chd\nGame (Europe) (Disc 3).chd\n",
		readPlaylist(t, filepath.Join(dir, "Game (Europe).m3u")))
	assert.FileExists(t, filepath.Join(dir, "Game (Europe) (Disc 1).chd"))
}

func TestInstallingBothDiscsOfAGameLeavesAPlaylistThatSwapsThem(t *testing.T) {
	psx, ok := rom.PlatformBySlug("psx")
	require.True(t, ok)
	l, root := newLibrary(t)

	for _, disc := range []string{"Disc 2", "Disc 1"} {
		base := "Driver 2 (USA) (" + disc + ")"
		archive := zipArchive(t, []zipEntry{
			{name: base + ".cue", body: "FILE \"" + base + ".bin\" BINARY\n"},
			{name: base + ".bin", body: "data " + disc},
		})
		srv := serve(t, servedBody(archive))
		result, err := l.Install(context.Background(), rom.InstallRequest{
			Platform: psx,
			File:     rom.File{Name: base + ".zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, "psx/"+base, result.Path)
	}

	assert.Equal(t,
		"Driver 2 (USA) (Disc 1)/Driver 2 (USA) (Disc 1).cue\nDriver 2 (USA) (Disc 2)/Driver 2 (USA) (Disc 2).cue\n",
		readPlaylist(t, filepath.Join(root, "psx", "Driver 2 (USA).m3u")))
	assert.FileExists(t, filepath.Join(root, "psx", "Driver 2 (USA) (Disc 1)", "Driver 2 (USA) (Disc 1).bin"))
}
