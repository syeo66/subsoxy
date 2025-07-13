package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSongStructure(t *testing.T) {
	song := Song{
		ID:          "123",
		Title:       "Test Song",
		Artist:      "Test Artist",
		Album:       "Test Album",
		Duration:    300,
		LastPlayed:  time.Now(),
		PlayCount:   5,
		SkipCount:   2,
	}

	if song.ID != "123" {
		t.Errorf("Song.ID = %s, want %s", song.ID, "123")
	}
	if song.Title != "Test Song" {
		t.Errorf("Song.Title = %s, want %s", song.Title, "Test Song")
	}
	if song.Artist != "Test Artist" {
		t.Errorf("Song.Artist = %s, want %s", song.Artist, "Test Artist")
	}
	if song.Album != "Test Album" {
		t.Errorf("Song.Album = %s, want %s", song.Album, "Test Album")
	}
	if song.Duration != 300 {
		t.Errorf("Song.Duration = %d, want %d", song.Duration, 300)
	}
	if song.PlayCount != 5 {
		t.Errorf("Song.PlayCount = %d, want %d", song.PlayCount, 5)
	}
	if song.SkipCount != 2 {
		t.Errorf("Song.SkipCount = %d, want %d", song.SkipCount, 2)
	}
}

func TestSongJSONSerialization(t *testing.T) {
	now := time.Now()
	song := Song{
		ID:          "123",
		Title:       "Test Song",
		Artist:      "Test Artist",
		Album:       "Test Album",
		Duration:    300,
		LastPlayed:  now,
		PlayCount:   5,
		SkipCount:   2,
	}

	jsonData, err := json.Marshal(song)
	if err != nil {
		t.Fatalf("Failed to marshal song to JSON: %v", err)
	}

	var unmarshaled Song
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal song from JSON: %v", err)
	}

	if unmarshaled.ID != song.ID {
		t.Errorf("Unmarshaled ID = %s, want %s", unmarshaled.ID, song.ID)
	}
	if unmarshaled.Title != song.Title {
		t.Errorf("Unmarshaled Title = %s, want %s", unmarshaled.Title, song.Title)
	}
	if unmarshaled.PlayCount != song.PlayCount {
		t.Errorf("Unmarshaled PlayCount = %d, want %d", unmarshaled.PlayCount, song.PlayCount)
	}
}

func TestPlayEventStructure(t *testing.T) {
	previousSong := "prev123"
	event := PlayEvent{
		ID:           1,
		SongID:       "123",
		EventType:    "play",
		Timestamp:    time.Now(),
		PreviousSong: &previousSong,
	}

	if event.ID != 1 {
		t.Errorf("PlayEvent.ID = %d, want %d", event.ID, 1)
	}
	if event.SongID != "123" {
		t.Errorf("PlayEvent.SongID = %s, want %s", event.SongID, "123")
	}
	if event.EventType != "play" {
		t.Errorf("PlayEvent.EventType = %s, want %s", event.EventType, "play")
	}
	if event.PreviousSong == nil || *event.PreviousSong != "prev123" {
		t.Errorf("PlayEvent.PreviousSong = %v, want %s", event.PreviousSong, "prev123")
	}
}

func TestPlayEventWithNilPreviousSong(t *testing.T) {
	event := PlayEvent{
		ID:           1,
		SongID:       "123",
		EventType:    "start",
		Timestamp:    time.Now(),
		PreviousSong: nil,
	}

	if event.PreviousSong != nil {
		t.Errorf("PlayEvent.PreviousSong = %v, want nil", event.PreviousSong)
	}
}

func TestSongTransitionStructure(t *testing.T) {
	transition := SongTransition{
		FromSongID:  "123",
		ToSongID:    "456",
		PlayCount:   10,
		SkipCount:   2,
		Probability: 0.85,
	}

	if transition.FromSongID != "123" {
		t.Errorf("SongTransition.FromSongID = %s, want %s", transition.FromSongID, "123")
	}
	if transition.ToSongID != "456" {
		t.Errorf("SongTransition.ToSongID = %s, want %s", transition.ToSongID, "456")
	}
	if transition.PlayCount != 10 {
		t.Errorf("SongTransition.PlayCount = %d, want %d", transition.PlayCount, 10)
	}
	if transition.SkipCount != 2 {
		t.Errorf("SongTransition.SkipCount = %d, want %d", transition.SkipCount, 2)
	}
	if transition.Probability != 0.85 {
		t.Errorf("SongTransition.Probability = %f, want %f", transition.Probability, 0.85)
	}
}

func TestWeightedSongStructure(t *testing.T) {
	song := Song{
		ID:     "123",
		Title:  "Test Song",
		Artist: "Test Artist",
	}

	weightedSong := WeightedSong{
		Song:   song,
		Weight: 0.75,
	}

	if weightedSong.Song.ID != "123" {
		t.Errorf("WeightedSong.Song.ID = %s, want %s", weightedSong.Song.ID, "123")
	}
	if weightedSong.Weight != 0.75 {
		t.Errorf("WeightedSong.Weight = %f, want %f", weightedSong.Weight, 0.75)
	}
}

func TestSubsonicResponseStructure(t *testing.T) {
	songs := []Song{
		{ID: "123", Title: "Song 1"},
		{ID: "456", Title: "Song 2"},
	}

	response := SubsonicResponse{
		SubsonicResponse: struct {
			Status  string `json:"status"`
			Version string `json:"version"`
			Songs   struct {
				Song []Song `json:"song"`
			} `json:"songs,omitempty"`
			MusicFolders struct {
				MusicFolder []MusicFolder `json:"musicFolder"`
			} `json:"musicFolders,omitempty"`
			Indexes struct {
				Index []Index `json:"index"`
			} `json:"indexes,omitempty"`
			Directory struct {
				Child []Song `json:"child"`
			} `json:"directory,omitempty"`
		}{
			Status:  "ok",
			Version: "1.15.0",
			Songs: struct {
				Song []Song `json:"song"`
			}{
				Song: songs,
			},
		},
	}

	if response.SubsonicResponse.Status != "ok" {
		t.Errorf("SubsonicResponse.Status = %s, want %s", response.SubsonicResponse.Status, "ok")
	}
	if response.SubsonicResponse.Version != "1.15.0" {
		t.Errorf("SubsonicResponse.Version = %s, want %s", response.SubsonicResponse.Version, "1.15.0")
	}
	if len(response.SubsonicResponse.Songs.Song) != 2 {
		t.Errorf("SubsonicResponse.Songs.Song length = %d, want %d", len(response.SubsonicResponse.Songs.Song), 2)
	}
}

func TestSubsonicResponseJSONSerialization(t *testing.T) {
	response := SubsonicResponse{
		SubsonicResponse: struct {
			Status  string `json:"status"`
			Version string `json:"version"`
			Songs   struct {
				Song []Song `json:"song"`
			} `json:"songs,omitempty"`
			MusicFolders struct {
				MusicFolder []MusicFolder `json:"musicFolder"`
			} `json:"musicFolders,omitempty"`
			Indexes struct {
				Index []Index `json:"index"`
			} `json:"indexes,omitempty"`
			Directory struct {
				Child []Song `json:"child"`
			} `json:"directory,omitempty"`
		}{
			Status:  "ok",
			Version: "1.15.0",
			Songs: struct {
				Song []Song `json:"song"`
			}{
				Song: []Song{
					{ID: "123", Title: "Song 1"},
				},
			},
		},
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal SubsonicResponse to JSON: %v", err)
	}

	var unmarshaled SubsonicResponse
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal SubsonicResponse from JSON: %v", err)
	}

	if unmarshaled.SubsonicResponse.Status != "ok" {
		t.Errorf("Unmarshaled Status = %s, want %s", unmarshaled.SubsonicResponse.Status, "ok")
	}
}

func TestHookFunction(t *testing.T) {
	// Test hook that returns true (blocks forwarding)
	blockingHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return true
	}

	// Test hook that returns false (allows forwarding)
	allowingHook := func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return false
	}

	// Create test request
	req := httptest.NewRequest("GET", "/rest/ping", nil)
	w := httptest.NewRecorder()

	// Test blocking hook
	if !blockingHook(w, req, "/rest/ping") {
		t.Error("Blocking hook should return true")
	}

	// Test allowing hook
	if allowingHook(w, req, "/rest/ping") {
		t.Error("Allowing hook should return false")
	}
}

func TestEventTypeValues(t *testing.T) {
	validEventTypes := []string{"play", "skip", "start"}
	
	for _, eventType := range validEventTypes {
		event := PlayEvent{
			ID:        1,
			SongID:    "123",
			EventType: eventType,
			Timestamp: time.Now(),
		}
		
		if event.EventType != eventType {
			t.Errorf("PlayEvent.EventType = %s, want %s", event.EventType, eventType)
		}
	}
}