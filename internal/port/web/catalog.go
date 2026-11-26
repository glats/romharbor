package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"app.ddd/internal/app/command"
	"app.ddd/internal/app/query"
	"app.ddd/internal/domain/rom"
)

var catalogFuncs = template.FuncMap{
	"encode": encodeID,
	"size":   humanSize,
	"join":   strings.Join,
	"compat": compatibilityLabel,
	"hint":   labelHint,
}

const (
	defaultCatalogLimit = 50
	maxCatalogLimit     = 200
)

type catalogPageData struct {
	Title         string
	Platforms     []rom.Platform
	Platform      *rom.Platform
	Listing       bool
	Searching     bool
	Groups        []searchGroupView
	Games         []gameView
	FailedSources []string
	Total         int
	Query         string
	Game          *gameView
	Jobs          []jobView
	ActiveJobs    int
	Error         string
	Destination   string
	Limit         int
	Offset        int
	HasPrev       bool
	HasNext       bool
	PrevOffset    int
	NextOffset    int
}

type catalogPlatformCount struct {
	rom.Platform
	Count int `json:"count"`
}

type catalogGamesResponse struct {
	Games         []rom.Game `json:"games"`
	FailedSources []string   `json:"failedSources,omitempty"`
}

func (h *Handler) mountCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.catalogHome)
	mux.HandleFunc("GET /platform/{slug}", h.catalogPlatform)
	mux.HandleFunc("GET /search", h.catalogSearch)
	mux.HandleFunc("GET /cover", h.catalogCover)
	mux.HandleFunc("GET /game/{id}", h.catalogGame)
	mux.HandleFunc("GET /api/platforms", h.catalogAPIPlatforms)
	mux.HandleFunc("GET /api/games", h.catalogAPIGames)
	mux.HandleFunc("GET /api/game", h.catalogAPIGame)
	mux.HandleFunc("GET /api/jobs", h.catalogAPIJobs)
	mux.HandleFunc("GET /api/job", h.catalogAPIJob)
	if h.app.Query.ObtainGameReadiness != nil {
		mux.HandleFunc("GET /api/job/readiness", h.catalogAPIJobReadiness)
	}
	if h.app.Query.ObtainGameBundle != nil {
		mux.HandleFunc("GET /api/job/bundle", h.catalogAPIJobBundle)
	}
	mux.HandleFunc("POST /api/download", h.catalogAPIDownload)
}

func (h *Handler) catalogHome(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: "ROM catalog"})
		return
	}
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:      "ROM catalog",
		Platforms:  rom.Platforms(),
		Jobs:       newJobViews(jobs),
		ActiveJobs: activeJobs(jobs),
	})
}

type searchGroupView struct {
	Platform rom.Platform
	Games    []gameView
}

const searchGroupLimit = 24

func (h *Handler) catalogSearch(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("q"))
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: "Search"})
		return
	}
	data := catalogPageData{Title: "Search", Searching: true, Query: text, Jobs: newJobViews(jobs), ActiveJobs: activeJobs(jobs)}
	if text != "" {
		result, err := h.app.Query.SearchAllGames.Handle(r.Context(), query.SearchAllGames{Text: text, Limit: searchGroupLimit})
		if err != nil {
			h.catalogFail(w, r, err, http.StatusBadGateway, data)
			return
		}
		data.FailedSources = append(data.FailedSources, result.FailedPlatforms...)
		for _, group := range result.Platforms {
			data.Groups = append(data.Groups, searchGroupView{Platform: group.Platform, Games: newGameViews(group.Games)})
			data.FailedSources = append(data.FailedSources, group.FailedSources...)
		}
	}
	h.catalogRender(w, http.StatusOK, data)
}

func (h *Handler) catalogPlatform(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	platform, ok := rom.PlatformBySlug(slug)
	if !ok {
		h.catalogRender(w, http.StatusNotFound, catalogPageData{Title: "Unknown platform", Error: "unknown platform " + slug})
		return
	}
	text := r.URL.Query().Get("q")
	offset, _ := catalogPagingInt(r.URL.Query().Get("offset"), 0)
	limit, ok := catalogPagingInt(r.URL.Query().Get("limit"), defaultCatalogLimit)
	if !ok {
		limit = defaultCatalogLimit
	}
	if limit > maxCatalogLimit {
		limit = maxCatalogLimit
	}
	destination, err := platform.Destination()
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: platform.Name})
		return
	}
	games, err := h.catalogGames(r.Context(), platform.Slug, text, offset, limit+1)
	if err != nil {
		h.catalogFail(w, r, err, http.StatusBadGateway, catalogPageData{Title: platform.Name, Platform: &platform})
		return
	}
	hasNext := len(games.Games) > limit
	if hasNext {
		games.Games = games.Games[:limit]
	}
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: platform.Name})
		return
	}
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:         platform.Name,
		Platform:      &platform,
		Listing:       true,
		Games:         newGameViews(games.Games),
		FailedSources: games.FailedSources,
		Query:         text,
		Total:         len(games.Games),
		Jobs:          newJobViews(jobs),
		ActiveJobs:    activeJobs(jobs),
		Destination:   destination,
		Limit:         limit,
		Offset:        offset,
		HasPrev:       offset > 0,
		HasNext:       hasNext,
		PrevOffset:    max(0, offset-limit),
		NextOffset:    offset + limit,
	})
}

func (h *Handler) catalogGame(w http.ResponseWriter, r *http.Request) {
	id, err := decodeID(r.PathValue("id"))
	if err != nil {
		h.catalogRender(w, http.StatusBadRequest, catalogPageData{Title: "Bad game", Error: "bad game id"})
		return
	}
	game, err := h.app.Query.ObtainGame.Handle(r.Context(), query.ObtainGame{ID: id})
	if errors.Is(err, rom.ErrGameNotFound) {
		h.catalogRender(w, http.StatusNotFound, catalogPageData{Title: "Unknown game", Error: "game not found"})
		return
	}
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: "Game"})
		return
	}
	platform, found := rom.PlatformBySlug(game.Platform)
	destination, err := platform.Destination()
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: game.Title})
		return
	}
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.catalogFail(w, r, err, http.StatusInternalServerError, catalogPageData{Title: game.Title})
		return
	}
	var platformView *rom.Platform
	if found {
		platformView = &platform
	}
	view := newGameView(game)
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:       game.Title,
		Platform:    platformView,
		Game:        &view,
		Jobs:        newJobViews(jobs),
		ActiveJobs:  activeJobs(jobs),
		Destination: destination,
	})
}

func (h *Handler) catalogRender(w http.ResponseWriter, status int, data catalogPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.catalogPages.ExecuteTemplate(w, "catalog.html", data); err != nil {
		h.logger.Error("render", "error", err)
	}
}

func (h *Handler) catalogAPIPlatforms(w http.ResponseWriter, r *http.Request) {
	counts, err := h.app.Query.ListPlatforms.Handle(r.Context(), query.ListPlatforms{})
	if err != nil {
		h.failJSON(w, r, http.StatusInternalServerError, err)
		return
	}
	out := []catalogPlatformCount{}
	for _, p := range counts {
		out = append(out, catalogPlatformCount{Platform: p.Platform, Count: p.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": out})
}

func (h *Handler) catalogGames(ctx context.Context, platform, text string, offset, limit int) (rom.GamePage, error) {
	if text == "" {
		return h.app.Query.BrowsePlatformGames.Handle(ctx, query.BrowsePlatformGames{Platform: platform, Offset: offset, Limit: limit})
	}
	return h.app.Query.SearchGames.Handle(ctx, query.SearchGames{Text: text, Platform: platform, Offset: offset, Limit: limit})
}

func (h *Handler) catalogAPIGames(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	offset, ok := catalogPagingInt(params.Get("offset"), 0)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid offset"})
		return
	}
	limit, ok := catalogPagingInt(params.Get("limit"), defaultCatalogLimit)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
		return
	}
	if limit > maxCatalogLimit {
		limit = maxCatalogLimit
	}
	games, err := h.catalogGames(r.Context(), params.Get("platform"), params.Get("q"), offset, limit)
	if errors.Is(err, rom.ErrUnknownPlatform) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown platform"})
		return
	}
	if err != nil {
		h.failJSON(w, r, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, catalogGamesResponse{Games: games.Games, FailedSources: games.FailedSources})
}

func (h *Handler) catalogAPIGame(w http.ResponseWriter, r *http.Request) {
	game, err := h.app.Query.ObtainGame.Handle(r.Context(), query.ObtainGame{ID: r.URL.Query().Get("id")})
	if errors.Is(err, rom.ErrGameNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
		return
	}
	if err != nil {
		h.failJSON(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": game})
}

func (h *Handler) catalogAPIJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.failJSON(w, r, http.StatusInternalServerError, err)
		return
	}
	writeConditionalJSON(w, r, map[string]any{"jobs": jobs})
}

func (h *Handler) catalogAPIJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.app.Query.ObtainJob.Handle(r.Context(), query.ObtainJob{ID: r.URL.Query().Get("id")})
	if errors.Is(err, rom.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	if err != nil {
		h.failJSON(w, r, http.StatusInternalServerError, err)
		return
	}
	writeConditionalJSON(w, r, job)
}

func (h *Handler) catalogAPIJobReadiness(w http.ResponseWriter, r *http.Request) {
	report, err := h.app.Query.ObtainGameReadiness.Handle(r.Context(), query.ObtainGameReadiness{ID: r.URL.Query().Get("id")})
	if errors.Is(err, rom.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	if err != nil {
		h.failJSON(w, r, http.StatusInternalServerError, err)
		return
	}
	writeConditionalJSON(w, r, report)
}

func (h *Handler) catalogAPIJobBundle(w http.ResponseWriter, r *http.Request) {
	bundle, err := h.app.Query.ObtainGameBundle.Handle(r.Context(), query.ObtainGameBundle{ID: r.URL.Query().Get("id")})
	switch {
	case errors.Is(err, rom.ErrJobNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
	case errors.Is(err, rom.ErrBundleUnavailable):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "portable bundle unavailable; inspect job readiness"})
	case err != nil:
		h.failJSON(w, r, http.StatusInternalServerError, err)
	default:
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": bundle.Name}))
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodHead {
			return
		}
		if err := bundle.WriteZIP(r.Context(), w); err != nil {
			h.logFailure(r, err)
			panic(http.ErrAbortHandler)
		}
	}
}

func (h *Handler) catalogAPIDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.logFailure(r, err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	job, err := h.app.Command.StartDownload.Handle(r.Context(), command.StartDownload{
		GameID: body.ID,
	})
	switch {
	case errors.Is(err, rom.ErrGameNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
	case errors.Is(err, rom.ErrNoDownloadFile):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "game has no file"})
	case errors.Is(err, rom.ErrUnknownPlatform):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown platform"})
	case errors.Is(err, rom.ErrShuttingDown):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "service is shutting down"})
	case err != nil:
		h.failJSON(w, r, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusAccepted, job)
	}
}

func (h *Handler) catalogFail(w http.ResponseWriter, r *http.Request, err error, status int, data catalogPageData) {
	h.logFailure(r, err)
	data.Error = genericErrorMessage
	h.catalogRender(w, status, data)
}

func catalogPagingInt(raw string, fallback int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, false
	}
	if value == 0 {
		return fallback, true
	}
	return value, true
}

func parseCatalogPages() *template.Template {
	return template.Must(template.New("catalog").Funcs(catalogFuncs).ParseFS(templateFiles, "templates/catalog.html"))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json", "error", err)
	}
}

// writeConditionalJSON answers 304 when the client already holds this exact
// body, so polling an unchanged job costs no payload.
func writeConditionalJSON(w http.ResponseWriter, r *http.Request, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		slog.Error("write json", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "encode response"})
		return
	}
	sum := sha256.Sum256(payload)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(append(payload, '\n')); err != nil {
		slog.Error("write json", "error", err)
	}
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// humanSize renders a byte count for the gallery.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// compatibilityLabel names the compatibility state of a game, empty when the
// view model carries none.
func labelHint(label string) string {
	switch label {
	case rom.LabelHeavy:
		return "Demanding on a handheld. On Knulli set the core to mupen64plus-next and keep the ROM as .z64; if a menu ignores the D-pad, map the D-pad to the analog stick."
	}
	return ""
}

func compatibilityLabel(compatible *bool) string {
	if compatible == nil {
		return ""
	}
	if *compatible {
		return "compatible"
	}
	return "incompatible"
}

// newGameView turns a catalog game into the view the pages render.
func newGameView(g rom.Game) gameView {
	var labels []string
	var compatible *bool
	if platform, ok := rom.PlatformBySlug(g.Platform); ok {
		result := rom.KnulliCompatibility(g, platform)
		labels, compatible = result.Labels, &result.Compatible
	}
	return gameView{
		Labels:     labels,
		Compatible: compatible,
		ID:         g.ID,
		Title:      g.Title,
		Region:     g.Region,
		Size:       g.Size,
		Cover:      coverURL(g),
		Source:     g.Source,
		Platform:   g.Platform,
		PageURL:    g.PageURL,
		Files:      g.Files,
	}
}

// newGameViews turns the games of a listing into the views the pages render.
func newGameViews(games []rom.Game) []gameView {
	views := make([]gameView, 0, len(games))
	for _, g := range games {
		views = append(views, newGameView(g))
	}
	return views
}

// newJobView turns a download job into the view the panel renders.
func newJobView(j rom.Job) jobView {
	return jobView{
		ID:         j.ID,
		Title:      j.Title,
		Status:     j.Status,
		Platform:   jobPlatform(j.Path),
		Downloaded: j.Downloaded,
		Total:      j.Total,
		Percent:    jobPercent(j),
		Error:      j.Error,
		Path:       j.Path,
	}
}

// newJobViews turns the download jobs into the views the panel renders, newest
// job first.
func newJobViews(jobs []rom.Job) []jobView {
	views := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		views = append(views, newJobView(j))
	}
	sort.SliceStable(views, func(i, k int) bool {
		return jobNumber(views[i].ID) > jobNumber(views[k].ID)
	})
	return views
}

// activeJobs counts the downloads that are still queued or running.
func activeJobs(jobs []rom.Job) int {
	count := 0
	for _, j := range jobs {
		if j.Status == rom.JobQueued || j.Status == rom.JobRunning {
			count++
		}
	}
	return count
}

// jobPlatform names the platform of a finished download through the library
// folder of its path, empty while the path is unknown.
func jobPlatform(path string) string {
	folder, _, found := strings.Cut(path, "/")
	if !found || folder == "" {
		return ""
	}
	return folder
}

// jobPercent reports how much of a download is done, in percent.
func jobPercent(j rom.Job) int {
	if j.Status == rom.JobDone {
		return 100
	}
	if j.Total <= 0 {
		return 0
	}
	percent := int(math.Round(float64(j.Downloaded) / float64(j.Total) * 100))
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// jobNumber reads the creation order of a job from its identifier.
func jobNumber(id string) int64 {
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0
	}
	return number
}

func encodeID(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeID(encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("bad game id: %w", err)
	}
	return string(raw), nil
}

const maxCoverTitleBytes = 300

func coverURL(g rom.Game) string {
	if g.Cover == "" || g.Platform == "" || g.Title == "" {
		return ""
	}
	return "/cover?" + url.Values{"platform": {g.Platform}, "title": {g.Title}}.Encode()
}

func (h *Handler) catalogCover(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	platform, title := params.Get("platform"), params.Get("title")
	if platform == "" || title == "" || len(title) > maxCoverTitleBytes {
		http.Error(w, "bad cover request", http.StatusBadRequest)
		return
	}
	if h.app.Query.ObtainCover == nil {
		http.NotFound(w, r)
		return
	}
	cover, err := h.app.Query.ObtainCover.Handle(r.Context(), query.ObtainCover{Platform: platform, Title: title})
	switch {
	case errors.Is(err, rom.ErrCoverNotFound), errors.Is(err, rom.ErrUnknownPlatform):
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.NotFound(w, r)
		return
	case err != nil:
		h.logger.Warn("cover unavailable", "platform", platform, "title", title, "error", err)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "cover unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", cover.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(cover.Data)
}
