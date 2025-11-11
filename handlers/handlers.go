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

func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request, endpoint string) bool {
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

		h.logger.WithFields(logrus.Fields{
			"song_id": SanitizeForLogging(songID),
			"user_id": SanitizeForLogging(userID),
		}).Debug("Stream request logged (no tracking for skip detection)")
	} else {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "id")).
			Warn("Stream request missing song ID")
	}
	return false
}

func (h *Handler) HandleScrobble(w http.ResponseWriter, r *http.Request, endpoint string, recordFunc func(string, string, string, *string), setLastPlayed func(string, string), processScrobbleFunc func(string, string, bool) bool) bool {
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

	isSubmission := submission == "true"

	// Process pending songs first (may mark earlier songs as skipped)
	// Returns true if this is a new play event, false if it's a duplicate
	shouldRecord := processScrobbleFunc(userID, songID, isSubmission)

	if isSubmission && shouldRecord {
		recordFunc(userID, songID, "play", nil)
		setLastPlayed(userID, songID)
		h.logger.WithFields(logrus.Fields{
			"song_id": sanitizedSongID,
			"user_id": sanitizedUserID,
		}).Debug("Recorded play event and processed pending songs")
	} else if isSubmission && !shouldRecord {
		h.logger.WithFields(logrus.Fields{
			"song_id": sanitizedSongID,
			"user_id": sanitizedUserID,
		}).Debug("Skipped duplicate play event for same song")
	} else {
		h.logger.WithFields(logrus.Fields{
			"song_id": sanitizedSongID,
			"user_id": sanitizedUserID,
		}).Debug("Processed scrobble without submission and handled pending songs")
	}

	return false
}

func (h *Handler) HandleDebug(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	userID := r.URL.Query().Get("u")
	if userID == "" {
		h.logger.WithError(errors.ErrMissingParameter.WithContext("parameter", "u")).
			Warn("Debug request missing user ID")
		http.Error(w, "Missing user parameter", http.StatusBadRequest)
		return true
	}

	songs, err := h.shuffle.GetAllSongsWithWeights(userID)
	if err != nil {
		h.logger.WithError(err).WithField("userID", SanitizeForLogging(userID)).Error("Failed to get songs for debug")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<title>Subsoxy Debug - User: ` + SanitizeForLogging(userID) + `</title>
	<style>
		body { font-family: Arial, sans-serif; margin: 20px; background-color: #f5f5f5; }
		h1 { color: #333; }
		table { border-collapse: collapse; width: 100%; background-color: white; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
		th, td { border: 1px solid #ddd; padding: 12px; text-align: left; }
		th { background-color: #4CAF50; color: white; position: sticky; top: 0; }
		tr:nth-child(even) { background-color: #f2f2f2; }
		tr:hover { background-color: #ddd; }
		.num { text-align: right; font-family: monospace; }
		.date { font-family: monospace; font-size: 0.9em; }
		.never { color: #999; font-style: italic; }
		.high-weight { background-color: #c8e6c9; }
		.medium-weight { background-color: #fff9c4; }
		.low-weight { background-color: #ffccbc; }
		.info { margin: 20px 0; padding: 15px; background-color: #e3f2fd; border-left: 4px solid #2196F3; }
	</style>
</head>
<body>
	<h1>Subsoxy Debug - User: ` + SanitizeForLogging(userID) + `</h1>
	<div class="info">
		<strong>Total Songs:</strong> ` + strconv.Itoa(len(songs)) + `<br>
		<strong>Weight Calculation:</strong> Base Weight × Time Weight × Play/Skip Weight × Transition Weight × Artist Weight<br>
		<strong>Color Legend:</strong>
		<span style="background-color: #c8e6c9; padding: 2px 6px;">High (≥2.0)</span>
		<span style="background-color: #fff9c4; padding: 2px 6px;">Medium (1.0-2.0)</span>
		<span style="background-color: #ffccbc; padding: 2px 6px;">Low (&lt;1.0)</span>
	</div>
	<table>
		<thead>
			<tr>
				<th>Song ID</th>
				<th>Title</th>
				<th>Artist</th>
				<th>Album</th>
				<th class="num">Duration (s)</th>
				<th class="num">Play Count</th>
				<th class="num">Skip Count</th>
				<th>Last Played</th>
				<th>Last Skipped</th>
				<th class="num">Time Weight</th>
				<th class="num">Play/Skip Weight</th>
				<th class="num">Transition Weight</th>
				<th class="num">Artist Weight</th>
				<th class="num">Final Weight</th>
			</tr>
		</thead>
		<tbody>
`

	for _, songWeight := range songs {
		song := songWeight.Song

		// Format dates
		lastPlayed := `<span class="never">Never</span>`
		if !song.LastPlayed.IsZero() {
			lastPlayed = `<span class="date">` + song.LastPlayed.Format("2006-01-02 15:04:05") + `</span>`
		}

		lastSkipped := `<span class="never">Never</span>`
		if !song.LastSkipped.IsZero() {
			lastSkipped = `<span class="date">` + song.LastSkipped.Format("2006-01-02 15:04:05") + `</span>`
		}

		// Calculate individual weight components
		timeWeight, playSkipWeight, transitionWeight, artistWeight := h.shuffle.GetWeightComponents(userID, song)

		// Determine row class based on final weight
		rowClass := ""
		if songWeight.Weight >= 2.0 {
			rowClass = " class=\"high-weight\""
		} else if songWeight.Weight >= 1.0 {
			rowClass = " class=\"medium-weight\""
		} else {
			rowClass = " class=\"low-weight\""
		}

		html += `<tr` + rowClass + `>
				<td>` + song.ID + `</td>
				<td>` + song.Title + `</td>
				<td>` + song.Artist + `</td>
				<td>` + song.Album + `</td>
				<td class="num">` + strconv.Itoa(song.Duration) + `</td>
				<td class="num">` + strconv.Itoa(song.PlayCount) + `</td>
				<td class="num">` + strconv.Itoa(song.SkipCount) + `</td>
				<td>` + lastPlayed + `</td>
				<td>` + lastSkipped + `</td>
				<td class="num">` + strconv.FormatFloat(timeWeight, 'f', 4, 64) + `</td>
				<td class="num">` + strconv.FormatFloat(playSkipWeight, 'f', 4, 64) + `</td>
				<td class="num">` + strconv.FormatFloat(transitionWeight, 'f', 4, 64) + `</td>
				<td class="num">` + strconv.FormatFloat(artistWeight, 'f', 4, 64) + `</td>
				<td class="num">` + strconv.FormatFloat(songWeight.Weight, 'f', 4, 64) + `</td>
			</tr>
`
	}

	html += `		</tbody>
	</table>
</body>
</html>`

	w.Write([]byte(html))

	h.logger.WithFields(logrus.Fields{
		"userID":    SanitizeForLogging(userID),
		"songCount": len(songs),
	}).Info("Served debug request")

	return true
}
