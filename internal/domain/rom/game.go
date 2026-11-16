package rom

import "time"

// File is one downloadable artifact of a game.
type File struct {
	// Name is the file name as the source publishes it.
	Name string
	// Size is the size in bytes, zero when the source does not report it.
	Size int64
	// MD5 and SHA1 are the checksums the source publishes, when available.
	MD5  string
	SHA1 string
	// SHA256 is published by some sources that serve plain HTTP.
	SHA256 string
	// URL is the direct download URL.
	URL string
	// Archive reports whether the file is an archive that must be extracted
	// before the ROM inside it reaches the library.
	Archive bool
}

// Game is one entry of the catalog.
type Game struct {
	// ID is unique across sources and stable for the source's item.
	ID string
	// Source is the name of the source that produced the game.
	Source string
	// Platform is the RomM platform slug.
	Platform string
	// Title is the display title.
	Title string
	// Region is the region tag when the source reports one.
	Region string
	// Size is the size of the main file in bytes.
	Size int64
	// Cover is the cover image URL, empty when there is none.
	Cover string
	// Screenshots are additional images.
	Screenshots []string
	// PageURL is the human readable page of the game on the source.
	PageURL string
	// Files are the downloadable artifacts, the first one being the main file.
	Files []File
}

// Main returns the main file of the game.
func (g Game) Main() (File, bool) {
	if len(g.Files) == 0 {
		return File{}, false
	}
	return g.Files[0], true
}

// Query asks a source for games.
type Query struct {
	// Platform is the RomM platform slug, empty for every platform.
	Platform string
	// Text is the free text search, empty to list everything.
	Text string
	// Limit caps the number of results.
	Limit int
	// Offset skips the first results.
	Offset int
}

// Listing is the games of one platform together with the time they were
// fetched from the sources.
type Listing struct {
	Fetched time.Time
	Games   []Game
}

// Job statuses.
const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

// Job is one running or finished download.
type Job struct {
	ID         string    `json:"id"`
	GameID     string    `json:"game_id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Downloaded int64     `json:"downloaded"`
	Total      int64     `json:"total"`
	Error      string    `json:"error,omitempty"`
	Path       string    `json:"path,omitempty"`
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished,omitempty"`
}

// InstallRequest describes one download into the library.
type InstallRequest struct {
	Game     Game
	File     File
	Platform Platform
}

// InstallResult reports what was installed.
type InstallResult struct {
	Path      string // final path of the ROM inside the library
	Size      int64
	Skipped   bool // true when the ROM was already present
	Extracted bool // true when the downloaded file was an archive
}

// Page returns the requested slice of results.
func Page(games []Game, offset, limit int) []Game {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(games) {
		return nil
	}
	games = games[offset:]
	if limit > 0 && len(games) > limit {
		games = games[:limit]
	}
	return games
}

// JobClaim is a queued job a worker took ownership of.
type JobClaim struct {
	Job     Job
	Request InstallRequest
	// Attempt counts the claims of the job, this one included.
	Attempt int
}
