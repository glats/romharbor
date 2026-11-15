package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"app.ddd/internal/app/catalog"
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
	data := catalogPageData{Title: "ROM catalog", Platforms: rom.Platforms()}
	for _, p := range h.app.Catalog.PlatformCounts(r.Context()) {
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
	query := r.URL.Query().Get("q")
	destination, err := h.app.Catalog.Destination(platform)
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: platform.Name, Error: err.Error()})
		return
	}
	games, err := h.app.Catalog.Games(r.Context(), platform.Slug, query, 0, 200)
	if err != nil {
		h.catalogRender(w, http.StatusBadGateway, catalogPageData{Title: platform.Name, Platform: platform, Error: err.Error()})
		return
	}
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:       platform.Name,
		Platform:    platform,
		Games:       games,
		Query:       query,
		Total:       len(games),
		Jobs:        h.app.Catalog.Jobs(),
		Destination: destination,
	})
}

func (h *Handler) catalogGame(w http.ResponseWriter, r *http.Request) {
	id, err := decodeID(r.PathValue("id"))
	if err != nil {
		h.catalogRender(w, http.StatusBadRequest, catalogPageData{Title: "Bad game", Error: err.Error()})
		return
	}
	game, ok, err := h.app.Catalog.Game(r.Context(), id)
	if err != nil || !ok {
		h.catalogRender(w, http.StatusNotFound, catalogPageData{Title: "Unknown game", Error: "game not found"})
		return
	}
	platform, _ := rom.PlatformBySlug(game.Platform)
	destination, err := h.app.Catalog.Destination(platform)
	if err != nil {
		h.catalogRender(w, http.StatusInternalServerError, catalogPageData{Title: game.Title, Error: err.Error()})
		return
	}
	h.catalogRender(w, http.StatusOK, catalogPageData{
		Title:       game.Title,
		Platform:    platform,
		Game:        &game,
		Cover:       game.Cover,
		Jobs:        h.app.Catalog.Jobs(),
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
	out := []catalogPlatformCount{}
	for _, p := range h.app.Catalog.PlatformCounts(r.Context()) {
		out = append(out, catalogPlatformCount{Platform: p.Platform, Count: p.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": out})
}

func (h *Handler) catalogAPIGames(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	games, err := h.app.Catalog.Games(r.Context(), query.Get("platform"), query.Get("q"), offset, limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": games})
}

func (h *Handler) catalogAPIGame(w http.ResponseWriter, r *http.Request) {
	game, ok, err := h.app.Catalog.Game(r.Context(), r.URL.Query().Get("id"))
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": game})
}

func (h *Handler) catalogAPIJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": h.app.Catalog.Jobs()})
}

func (h *Handler) catalogAPIJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.app.Catalog.Job(r.URL.Query().Get("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) catalogAPIDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	job, err := h.app.Catalog.StartDownload(r.Context(), catalog.StartDownload{
		NewJobID: func() string { return strconv.FormatUint(h.jobSeq.Add(1), 10) },
		GameID:   body.ID,
	})
	switch {
	case errors.Is(err, catalog.ErrGameNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
	case errors.Is(err, catalog.ErrNoDownloadFile):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "game has no file"})
	case errors.Is(err, catalog.ErrUnknownPlatform):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown platform"})
	case errors.Is(err, catalog.ErrShuttingDown):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "service is shutting down"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusAccepted, job)
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
