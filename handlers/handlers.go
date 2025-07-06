package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/sirupsen/logrus"
	
	"github.com/syeo66/subsoxy/shuffle"
)

type Handler struct {
	logger  *logrus.Logger
	shuffle *shuffle.Service
}

func New(logger *logrus.Logger, shuffleService *shuffle.Service) *Handler {
	return &Handler{
		logger:  logger,
		shuffle: shuffleService,
	}
}

func (h *Handler) HandleShuffle(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	sizeStr := r.URL.Query().Get("size")
	size := 50
	if sizeStr != "" {
		if parsedSize, err := strconv.Atoi(sizeStr); err == nil && parsedSize > 0 {
			size = parsedSize
		}
	}
	
	songs, err := h.shuffle.GetWeightedShuffledSongs(size)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get weighted shuffled songs")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return true
	}
	
	response := map[string]interface{}{
		"subsonic-response": map[string]interface{}{
			"status":  "ok",
			"version": "1.15.0",
			"songs": map[string]interface{}{
				"song": songs,
			},
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.WithError(err).Error("Failed to encode shuffle response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return true
	}
	
	h.logger.WithFields(logrus.Fields{
		"size": size,
		"returned": len(songs),
	}).Info("Served weighted shuffle request")
	
	return true
}

func (h *Handler) HandlePing(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	h.logger.Info("Ping endpoint accessed")
	return false
}

func (h *Handler) HandleGetLicense(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	h.logger.Info("License endpoint accessed")
	return false
}

func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, *string)) bool {
	songID := r.URL.Query().Get("id")
	if songID != "" {
		recordFunc(songID, "start", nil)
	}
	return false
}

func (h *Handler) HandleScrobble(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, *string), setLastPlayed func(string)) bool {
	songID := r.URL.Query().Get("id")
	submission := r.URL.Query().Get("submission")
	if songID != "" {
		if submission == "true" {
			recordFunc(songID, "play", nil)
			setLastPlayed(songID)
		} else {
			recordFunc(songID, "skip", nil)
		}
	}
	return false
}