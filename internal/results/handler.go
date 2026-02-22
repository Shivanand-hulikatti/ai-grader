package results

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "results-service",
	})
}

func (h *Handler) HandleListResults(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized", "User authentication required")
		return
	}

	limit := 20
	offset := 0
	if val := r.URL.Query().Get("limit"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			limit = parsed
		}
	}
	if val := r.URL.Query().Get("offset"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			offset = parsed
		}
	}

	results, err := h.service.ListResults(r.Context(), userID, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database_error", "Failed to fetch results")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

func (h *Handler) HandleGetResult(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized", "User authentication required")
		return
	}

	submissionID := strings.TrimPrefix(r.URL.Path, "/results/")
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" || submissionID == "results" {
		respondError(w, http.StatusBadRequest, "invalid_submission_id", "submission id is required")
		return
	}

	result, err := h.service.GetResultBySubmissionID(r.Context(), submissionID, userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database_error", "Failed to fetch result")
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "not_found", "Result not found")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func respondJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, code int, errorCode, message string) {
	respondJSON(w, code, map[string]string{
		"error":   errorCode,
		"message": message,
	})
}
