package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/sirupsen/logrus"
	
	"github.com/syeo66/subsoxy/errors"
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
		if parsedSize, err := strconv.Atoi(sizeStr); err == nil {
			if parsedSize > 10000 { // Prevent extremely large requests
				validationErr := errors.ErrValidationFailed.WithContext("field", "size").
					WithContext("value", parsedSize).
					WithContext("max_allowed", 10000)
				h.logger.WithError(validationErr).Warn("Size parameter too large")
				http.Error(w, "Size parameter too large (max: 10000)", http.StatusBadRequest)
				return true
			}
			if parsedSize > 0 { // Only use valid positive sizes, otherwise keep default
				size = parsedSize
			}
		} else {
			validationErr := errors.ErrInvalidInput.WithContext("field", "size").
				WithContext("value", sizeStr)
			h.logger.WithError(validationErr).Warn("Invalid size parameter")
			http.Error(w, "Invalid size parameter", http.StatusBadRequest)
			return true
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
		encodeErr := errors.Wrap(err, errors.CategoryServer, "RESPONSE_ENCODING_FAILED", "failed to encode shuffle response").
			WithContext("size", size).
			WithContext("song_count", len(songs))
		h.logger.WithError(encodeErr).Error("Failed to encode shuffle response")
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
		h.logger.WithField("song_id", songID).Debug("Recorded stream start event")
	} else {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "id")).
			Warn("Stream request missing song ID")
	}
	return false
}

func (h *Handler) HandleScrobble(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, *string), setLastPlayed func(string)) bool {
	songID := r.URL.Query().Get("id")
	submission := r.URL.Query().Get("submission")
	
	if songID == "" {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "id")).
			Warn("Scrobble request missing song ID")
		return false
	}
	
	if submission == "true" {
		recordFunc(songID, "play", nil)
		setLastPlayed(songID)
		h.logger.WithField("song_id", songID).Debug("Recorded play event")
	} else {
		// Treat missing or non-"true" submission as skip
		recordFunc(songID, "skip", nil)
		h.logger.WithField("song_id", songID).Debug("Recorded skip event")
		
		if submission == "" {
			h.logger.WithField("song_id", songID).Debug("Scrobble request missing submission parameter, treating as skip")
		}
	}
	
	return false
}