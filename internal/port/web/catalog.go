package web

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
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
}

type catalogPageData struct {
	Title       string
	Platforms   []rom.Platform
	Platform    rom.Platform
	Games       []rom.Game
	Cover       string
	Total       int
	Query       string
	Game        *rom.Game
	Jobs        []rom.Job
	Error       string
	Destination string
}

type catalogPlatformCount struct {
	rom.Platform
	Count int `json:"count"`
}

func (h *Handler) mountCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.catalogHome)
	mux.HandleFunc("GET /platform/{slug}", h.catalogPlatform)
	mux.HandleFunc("GET /game/{id}", h.catalogGame)
	mux.HandleFunc("GET /api/platforms", h.catalogAPIPlatforms)
	mux.HandleFunc("GET /api/games", h.catalogAPIGames)
	mux.HandleFunc("GET /api/game", h.catalogAPIGame)
	mux.HandleFunc("GET /api/jobs", h.catalogAPIJobs)
	mux.HandleFunc("GET /api/job", h.catalogAPIJob)
	mux.HandleFunc("POST /api/download", h.catalogAPIDownload)
}

func (h *Handler) catalogHome(w http.ResponseWriter, r *http.Request) {
	counts, err := h.app.Query.ListPlatforms.Handle(r.Context(), query.ListPlatforms{})
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: "ROM catalog", Error: err.Error()})
		return
	}
	data := catalogPageData{Title: "ROM catalog", Platforms: rom.Platforms()}
	for _, p := range counts {
		data.Total += p.Count
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
	destination, err := h.app.Query.ObtainDestination.Handle(r.Context(), query.ObtainDestination{Platform: platform})
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: platform.Name, Error: err.Error()})
		return
	}
	games, err := h.app.Query.ListGames.Handle(r.Context(), query.ListGames{Platform: platform.Slug, Text: text, Limit: 200})
	if err != nil {
		h.catalogRender(w, http.StatusBadGateway, catalogPageData{Title: platform.Name, Platform: platform, Error: err.Error()})
		return
	}
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: platform.Name, Error: err.Error()})
		return
	}
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:       platform.Name,
		Platform:    platform,
		Games:       games,
		Query:       text,
		Total:       len(games),
		Jobs:        jobs,
		Destination: destination,
	})
}

func (h *Handler) catalogGame(w http.ResponseWriter, r *http.Request) {
	id, err := decodeID(r.PathValue("id"))
	if err != nil {
		h.catalogRender(w, http.StatusBadRequest, catalogPageData{Title: "Bad game", Error: err.Error()})
		return
	}
	game, err := h.app.Query.ObtainGame.Handle(r.Context(), query.ObtainGame{ID: id})
	if err != nil {
		h.catalogRender(w, http.StatusNotFound, catalogPageData{Title: "Unknown game", Error: "game not found"})
		return
	}
	platform, _ := rom.PlatformBySlug(game.Platform)
	destination, err := h.app.Query.ObtainDestination.Handle(r.Context(), query.ObtainDestination{Platform: platform})
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: game.Title, Error: err.Error()})
		return
	}
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: game.Title, Error: err.Error()})
		return
	}
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:       game.Title,
		Platform:    platform,
		Game:        &game,
		Cover:       game.Cover,
		Jobs:        jobs,
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := []catalogPlatformCount{}
	for _, p := range counts {
		out = append(out, catalogPlatformCount{Platform: p.Platform, Count: p.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": out})
}

func (h *Handler) catalogAPIGames(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	offset, _ := strconv.Atoi(params.Get("offset"))
	limit, _ := strconv.Atoi(params.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	games, err := h.app.Query.ListGames.Handle(r.Context(), query.ListGames{
		Platform: params.Get("platform"),
		Text:     params.Get("q"),
		Offset:   offset,
		Limit:    limit,
	})
	if errors.Is(err, rom.ErrUnknownPlatform) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown platform"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": games})
}

func (h *Handler) catalogAPIGame(w http.ResponseWriter, r *http.Request) {
	game, err := h.app.Query.ObtainGame.Handle(r.Context(), query.ObtainGame{ID: r.URL.Query().Get("id")})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": game})
}

func (h *Handler) catalogAPIJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.app.Query.ListJobs.Handle(r.Context(), query.ListJobs{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeConditionalJSON(w, r, job)
}

func (h *Handler) catalogAPIDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var queued rom.Job
	err := h.app.Command.StartDownload.Handle(r.Context(), command.StartDownload{
		GameID: body.ID,
		Queued: func(job rom.Job) { queued = job },
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusAccepted, queued)
	}
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
