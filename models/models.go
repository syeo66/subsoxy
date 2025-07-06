package models

import (
	"net/http"
	"time"
)

type Song struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	Album       string    `json:"album"`
	Duration    int       `json:"duration"`
	LastPlayed  time.Time `json:"lastPlayed"`
	PlayCount   int       `json:"playCount"`
	SkipCount   int       `json:"skipCount"`
}

type PlayEvent struct {
	ID          int       `json:"id"`
	SongID      string    `json:"songId"`
	EventType   string    `json:"eventType"` // "play", "skip", "start"
	Timestamp   time.Time `json:"timestamp"`
	PreviousSong *string  `json:"previousSong,omitempty"`
}

type SongTransition struct {
	FromSongID string  `json:"fromSongId"`
	ToSongID   string  `json:"toSongId"`
	PlayCount  int     `json:"playCount"`
	SkipCount  int     `json:"skipCount"`
	Probability float64 `json:"probability"`
}

type WeightedSong struct {
	Song   Song    `json:"song"`
	Weight float64 `json:"weight"`
}

type SubsonicResponse struct {
	SubsonicResponse struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Songs   struct {
			Song []Song `json:"song"`
		} `json:"songs,omitempty"`
	} `json:"subsonic-response"`
}

type Hook func(w http.ResponseWriter, r *http.Request, endpoint string) bool