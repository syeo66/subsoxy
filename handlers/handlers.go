package handlers

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/syeo66/subsoxy/errors"
	"github.com/syeo66/subsoxy/models"
	"github.com/syeo66/subsoxy/shuffle"
)

const (
	MaxSongIDLength = 255
	MaxInputLength  = 1000
)

// Shuffle constants
const (
	DefaultShuffleSize = 50
	MaxShuffleSize     = 10000
	SubsonicAPIVersion = "1.15.0"
)

// ASCII control character constants
const (
	ASCIIControlCharMin = 32
	ASCIIControlCharMax = 127
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

// SanitizeForLogging removes control characters and limits length to prevent log injection
func SanitizeForLogging(input string) string {
	// Remove control characters (ASCII 0-31 and 127)
	sanitized := strings.Map(func(r rune) rune {
		if r < ASCIIControlCharMin || r == ASCIIControlCharMax {
			return -1
		}
		return r
	}, input)

	// Limit length to prevent resource exhaustion
	if len(sanitized) > MaxInputLength {
		sanitized = sanitized[:MaxInputLength] + "..."
	}

	return sanitized
}

// ValidateSongID validates song ID format and length
func ValidateSongID(songID string) error {
	if len(songID) == 0 {
		return errors.ErrMissingParameter.WithContext("parameter", "songID")
	}
	if len(songID) > MaxSongIDLength {
		return errors.ErrInvalidInput.WithContext("field", "songID").WithContext("length", len(songID)).WithContext("max_length", MaxSongIDLength)
	}
	return nil
}

func (h *Handler) HandleShuffle(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	// Extract user ID from request
	userID := r.URL.Query().Get("u")
	if userID == "" {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "u")).
			Warn("Shuffle request missing user ID")
		http.Error(w, "Missing user parameter", http.StatusBadRequest)
		return true
	}

	sizeStr := r.URL.Query().Get("size")
	size := DefaultShuffleSize
	if sizeStr != "" {
		if parsedSize, err := strconv.Atoi(sizeStr); err == nil {
			if parsedSize > MaxShuffleSize { // Prevent extremely large requests
				validationErr := errors.ErrValidationFailed.WithContext("field", "size").
					WithContext("value", parsedSize).
					WithContext("max_allowed", MaxShuffleSize)
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

	songs, err := h.shuffle.GetWeightedShuffledSongs(userID, size)
	if err != nil {
		h.logger.WithError(err).WithField("userID", SanitizeForLogging(userID)).Error("Failed to get weighted shuffled songs")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return true
	}

	// Check format parameter (defaults to json if not specified)
	format := r.URL.Query().Get("f")
	if format == "" {
		format = "json"
	}

	if format == "xml" {
		// XML response
		xmlResponse := models.XMLSubsonicResponse{
			Status:  "ok",
			Version: "1.15.0",
			Songs: &models.XMLSongs{
				Song: songs,
			},
		}

		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>`))
		if err := xml.NewEncoder(w).Encode(xmlResponse); err != nil {
			encodeErr := errors.Wrap(err, errors.CategoryServer, "RESPONSE_ENCODING_FAILED", "failed to encode XML shuffle response").
				WithContext("size", size).
				WithContext("song_count", len(songs)).
				WithContext("userID", userID)
			h.logger.WithError(encodeErr).Error("Failed to encode XML shuffle response")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return true
		}
	} else {
		// JSON response (default)
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
			encodeErr := errors.Wrap(err, errors.CategoryServer, "RESPONSE_ENCODING_FAILED", "failed to encode JSON shuffle response").
				WithContext("size", size).
				WithContext("song_count", len(songs)).
				WithContext("userID", userID)
			h.logger.WithError(encodeErr).Error("Failed to encode JSON shuffle response")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return true
		}
	}

	h.logger.WithFields(logrus.Fields{
		"size":     size,
		"returned": len(songs),
		"userID":   SanitizeForLogging(userID),
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

func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, string, *string)) bool {
	userID := r.URL.Query().Get("u")
	songID := r.URL.Query().Get("id")

	if userID == "" {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "u")).
			Warn("Stream request missing user ID")
		return false
	}

	if songID != "" {
		if err := ValidateSongID(songID); err != nil {
			h.logger.WithError(err).Warn("Invalid song ID in stream request")
			return false
		}
		recordFunc(userID, songID, "start", nil)
		h.logger.WithFields(logrus.Fields{
			"song_id": SanitizeForLogging(songID),
			"user_id": SanitizeForLogging(userID),
		}).Debug("Recorded stream start event")
	} else {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "id")).
			Warn("Stream request missing song ID")
	}
	return false
}

func (h *Handler) HandleScrobble(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, string, *string), setLastPlayed func(string, string)) bool {
	userID := r.URL.Query().Get("u")
	songID := r.URL.Query().Get("id")
	submission := r.URL.Query().Get("submission")

	if userID == "" {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "u")).
			Warn("Scrobble request missing user ID")
		return false
	}

	if songID == "" {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "id")).
			Warn("Scrobble request missing song ID")
		return false
	}

	if err := ValidateSongID(songID); err != nil {
		h.logger.WithError(err).Warn("Invalid song ID in scrobble request")
		return false
	}

	sanitizedSongID := SanitizeForLogging(songID)
	sanitizedUserID := SanitizeForLogging(userID)

	if submission == "true" {
		recordFunc(userID, songID, "play", nil)
		setLastPlayed(userID, songID)
		h.logger.WithFields(logrus.Fields{
			"song_id": sanitizedSongID,
			"user_id": sanitizedUserID,
		}).Debug("Recorded play event")
	} else {
		// Treat missing or non-"true" submission as skip
		recordFunc(userID, songID, "skip", nil)
		h.logger.WithFields(logrus.Fields{
			"song_id": sanitizedSongID,
			"user_id": sanitizedUserID,
		}).Debug("Recorded skip event")

		if submission == "" {
			h.logger.WithFields(logrus.Fields{
				"song_id": sanitizedSongID,
				"user_id": sanitizedUserID,
			}).Debug("Scrobble request missing submission parameter, treating as skip")
		}
	}

	return false
}
