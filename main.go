package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
)

type Config struct {
	ProxyPort    string
	UpstreamURL  string
	LogLevel     string
	DatabasePath string
}

type ProxyServer struct {
	config *Config
	logger *logrus.Logger
	proxy  *httputil.ReverseProxy
	hooks  map[string][]Hook
	db     *sql.DB
	lastPlayed *Song
	validCredentials map[string]string // username -> password
	credentialsMutex sync.RWMutex
}

type Hook func(w http.ResponseWriter, r *http.Request, endpoint string) bool

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

func NewProxyServer(config *Config) *ProxyServer {
	logger := logrus.New()
	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	upstreamURL, err := url.Parse(config.UpstreamURL)
	if err != nil {
		logger.Fatal("Invalid upstream URL:", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)
	
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = upstreamURL.Host
		req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
	}

	db, err := initDatabase(config.DatabasePath)
	if err != nil {
		logger.Fatal("Failed to initialize database:", err)
	}

	server := &ProxyServer{
		config: config,
		logger: logger,
		proxy:  proxy,
		hooks:  make(map[string][]Hook),
		db:     db,
		validCredentials: make(map[string]string),
	}

	go server.syncSongs()

	return server
}

func (ps *ProxyServer) AddHook(endpoint string, hook Hook) {
	ps.hooks[endpoint] = append(ps.hooks[endpoint], hook)
}

func (ps *ProxyServer) proxyHandler(w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	
	ps.logger.WithFields(logrus.Fields{
		"method":   r.Method,
		"endpoint": endpoint,
		"remote":   r.RemoteAddr,
	}).Info("Incoming request")

	// Capture and validate credentials from Subsonic API requests
	if strings.HasPrefix(endpoint, "/rest/") {
		username := r.URL.Query().Get("u")
		password := r.URL.Query().Get("p")
		if username != "" && password != "" && len(username) > 0 && len(password) > 0 {
			go ps.validateAndStoreCredentials(username, password) // Validate asynchronously
		}
	}

	if hooks, exists := ps.hooks[endpoint]; exists {
		for _, hook := range hooks {
			if hook(w, r, endpoint) {
				return
			}
		}
	}

	if strings.HasPrefix(endpoint, "/rest/") {
		ps.logger.WithField("endpoint", endpoint).Debug("Subsonic API endpoint")
	}

	ps.proxy.ServeHTTP(w, r)
}

func (ps *ProxyServer) Start() {
	router := mux.NewRouter()
	router.PathPrefix("/").HandlerFunc(ps.proxyHandler)

	ps.logger.WithFields(logrus.Fields{
		"port":     ps.config.ProxyPort,
		"upstream": ps.config.UpstreamURL,
		"url":      fmt.Sprintf("http://localhost:%s", ps.config.ProxyPort),
	}).Info("Starting proxy server")

	log.Fatal(http.ListenAndServe(":"+ps.config.ProxyPort, router))
}

func initDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if err := createTables(db); err != nil {
		return nil, err
	}

	return db, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS songs (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			artist TEXT NOT NULL,
			album TEXT NOT NULL,
			duration INTEGER NOT NULL,
			last_played DATETIME,
			play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS play_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			song_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			previous_song TEXT,
			FOREIGN KEY (song_id) REFERENCES songs(id),
			FOREIGN KEY (previous_song) REFERENCES songs(id)
		)`,
		`CREATE TABLE IF NOT EXISTS song_transitions (
			from_song_id TEXT NOT NULL,
			to_song_id TEXT NOT NULL,
			play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0,
			probability REAL DEFAULT 0.0,
			PRIMARY KEY (from_song_id, to_song_id),
			FOREIGN KEY (from_song_id) REFERENCES songs(id),
			FOREIGN KEY (to_song_id) REFERENCES songs(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_song_id ON play_events(song_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_timestamp ON play_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_song_transitions_from ON song_transitions(from_song_id)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (ps *ProxyServer) syncSongs() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	ps.fetchAndStoreSongs()

	for range ticker.C {
		ps.fetchAndStoreSongs()
	}
}

func (ps *ProxyServer) fetchAndStoreSongs() {
	ps.logger.Info("Syncing songs from Subsonic API")
	
	// Get valid credentials for background operations
	username, password := ps.getValidCredentials()
	if username == "" || password == "" {
		ps.logger.Warn("No valid credentials available for song syncing")
		return
	}
	
	url := fmt.Sprintf("%s/rest/search3?query=*&songCount=10000&f=json&v=1.15.0&c=subsoxy&u=%s&p=%s", 
		ps.config.UpstreamURL, username, password)
	
	resp, err := http.Get(url)
	if err != nil {
		ps.logger.WithError(err).Error("Failed to fetch songs from Subsonic API")
		return
	}
	defer resp.Body.Close()

	var subsonicResp SubsonicResponse
	if err := json.NewDecoder(resp.Body).Decode(&subsonicResp); err != nil {
		ps.logger.WithError(err).Error("Failed to decode Subsonic response")
		return
	}

	if subsonicResp.SubsonicResponse.Status != "ok" {
		ps.logger.Error("Subsonic API returned error status - possibly authentication failed")
		// Clear invalid credentials if authentication failed
		ps.credentialsMutex.Lock()
		if len(ps.validCredentials) > 0 {
			ps.logger.Warn("Clearing potentially invalid credentials")
			ps.validCredentials = make(map[string]string)
		}
		ps.credentialsMutex.Unlock()
		return
	}

	tx, err := ps.db.Begin()
	if err != nil {
		ps.logger.WithError(err).Error("Failed to start transaction")
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO songs (id, title, artist, album, duration, play_count, skip_count) 
		VALUES (?, ?, ?, ?, ?, COALESCE((SELECT play_count FROM songs WHERE id = ?), 0), COALESCE((SELECT skip_count FROM songs WHERE id = ?), 0))`)
	if err != nil {
		ps.logger.WithError(err).Error("Failed to prepare statement")
		return
	}
	defer stmt.Close()

	for _, song := range subsonicResp.SubsonicResponse.Songs.Song {
		_, err := stmt.Exec(song.ID, song.Title, song.Artist, song.Album, song.Duration, song.ID, song.ID)
		if err != nil {
			ps.logger.WithError(err).WithField("songId", song.ID).Error("Failed to insert song")
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		ps.logger.WithError(err).Error("Failed to commit transaction")
		return
	}

	ps.logger.WithField("count", len(subsonicResp.SubsonicResponse.Songs.Song)).Info("Successfully synced songs")
}

func (ps *ProxyServer) recordPlayEvent(songID, eventType string, previousSong *string) {
	now := time.Now()
	
	_, err := ps.db.Exec(`INSERT INTO play_events (song_id, event_type, timestamp, previous_song) VALUES (?, ?, ?, ?)`,
		songID, eventType, now, previousSong)
	if err != nil {
		ps.logger.WithError(err).Error("Failed to record play event")
		return
	}

	if eventType == "play" {
		_, err := ps.db.Exec(`UPDATE songs SET play_count = play_count + 1, last_played = ? WHERE id = ?`, now, songID)
		if err != nil {
			ps.logger.WithError(err).Error("Failed to update song play count")
		}
	} else if eventType == "skip" {
		_, err := ps.db.Exec(`UPDATE songs SET skip_count = skip_count + 1 WHERE id = ?`, songID)
		if err != nil {
			ps.logger.WithError(err).Error("Failed to update song skip count")
		}
	}

	if previousSong != nil {
		ps.recordTransition(*previousSong, songID, eventType)
	}

	ps.logger.WithFields(logrus.Fields{
		"songId":       songID,
		"eventType":    eventType,
		"previousSong": previousSong,
	}).Debug("Recorded play event")
}

func (ps *ProxyServer) recordTransition(fromSongID, toSongID, eventType string) {
	if eventType == "play" {
		_, err := ps.db.Exec(`INSERT OR REPLACE INTO song_transitions (from_song_id, to_song_id, play_count, skip_count)
			VALUES (?, ?, COALESCE((SELECT play_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0) + 1,
			COALESCE((SELECT skip_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0))`,
			fromSongID, toSongID, fromSongID, toSongID, fromSongID, toSongID)
		if err != nil {
			ps.logger.WithError(err).Error("Failed to record play transition")
		}
	} else if eventType == "skip" {
		_, err := ps.db.Exec(`INSERT OR REPLACE INTO song_transitions (from_song_id, to_song_id, play_count, skip_count)
			VALUES (?, ?, COALESCE((SELECT play_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0),
			COALESCE((SELECT skip_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0) + 1)`,
			fromSongID, toSongID, fromSongID, toSongID, fromSongID, toSongID)
		if err != nil {
			ps.logger.WithError(err).Error("Failed to record skip transition")
		}
	}

	ps.updateTransitionProbabilities(fromSongID, toSongID)
}

func (ps *ProxyServer) updateTransitionProbabilities(fromSongID, toSongID string) {
	_, err := ps.db.Exec(`UPDATE song_transitions 
		SET probability = CAST(play_count AS REAL) / CAST((play_count + skip_count) AS REAL)
		WHERE from_song_id = ? AND to_song_id = ? AND (play_count + skip_count) > 0`,
		fromSongID, toSongID)
	if err != nil {
		ps.logger.WithError(err).Error("Failed to update transition probabilities")
	}
}

func (ps *ProxyServer) getWeightedShuffledSongs(count int) ([]Song, error) {
	songs, err := ps.getAllSongs()
	if err != nil {
		return nil, err
	}

	weightedSongs := make([]WeightedSong, 0, len(songs))
	for _, song := range songs {
		weight := ps.calculateSongWeight(song)
		weightedSongs = append(weightedSongs, WeightedSong{
			Song:   song,
			Weight: weight,
		})
	}

	sort.Slice(weightedSongs, func(i, j int) bool {
		return weightedSongs[i].Weight > weightedSongs[j].Weight
	})

	totalWeight := 0.0
	for _, ws := range weightedSongs {
		totalWeight += ws.Weight
	}

	result := make([]Song, 0, count)
	used := make(map[string]bool)

	for len(result) < count && len(result) < len(songs) {
		target := rand.Float64() * totalWeight
		current := 0.0
		
		for _, ws := range weightedSongs {
			if used[ws.Song.ID] {
				continue
			}
			current += ws.Weight
			if current >= target {
				result = append(result, ws.Song)
				used[ws.Song.ID] = true
				totalWeight -= ws.Weight
				break
			}
		}
	}

	return result, nil
}

func (ps *ProxyServer) getAllSongs() ([]Song, error) {
	rows, err := ps.db.Query(`SELECT id, title, artist, album, duration, 
		COALESCE(last_played, '1970-01-01') as last_played, 
		COALESCE(play_count, 0) as play_count, 
		COALESCE(skip_count, 0) as skip_count 
		FROM songs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []Song
	for rows.Next() {
		var song Song
		var lastPlayedStr string
		err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album, 
			&song.Duration, &lastPlayedStr, &song.PlayCount, &song.SkipCount)
		if err != nil {
			ps.logger.WithError(err).Error("Failed to scan song")
			continue
		}
		
		if lastPlayedStr != "1970-01-01" {
			song.LastPlayed, _ = time.Parse("2006-01-02 15:04:05", lastPlayedStr)
		}
		
		songs = append(songs, song)
	}
	
	return songs, nil
}

func (ps *ProxyServer) calculateSongWeight(song Song) float64 {
	baseWeight := 1.0
	
	timeWeight := ps.calculateTimeDecayWeight(song.LastPlayed)
	playSkipWeight := ps.calculatePlaySkipWeight(song.PlayCount, song.SkipCount)
	transitionWeight := ps.calculateTransitionWeight(song.ID)
	
	finalWeight := baseWeight * timeWeight * playSkipWeight * transitionWeight
	
	ps.logger.WithFields(logrus.Fields{
		"songId":        song.ID,
		"timeWeight":    timeWeight,
		"playSkipWeight": playSkipWeight,
		"transitionWeight":  transitionWeight,
		"finalWeight":   finalWeight,
	}).Debug("Calculated song weight")
	
	return finalWeight
}

func (ps *ProxyServer) calculateTimeDecayWeight(lastPlayed time.Time) float64 {
	if lastPlayed.IsZero() {
		return 2.0
	}
	
	daysSinceLastPlayed := time.Since(lastPlayed).Hours() / 24.0
	
	if daysSinceLastPlayed < 30 {
		return 0.1 + (daysSinceLastPlayed/30.0)*0.9
	}
	
	return 1.0 + math.Min(daysSinceLastPlayed/365.0, 1.0)
}

func (ps *ProxyServer) calculatePlaySkipWeight(playCount, skipCount int) float64 {
	if playCount == 0 && skipCount == 0 {
		return 1.5
	}
	
	totalEvents := playCount + skipCount
	if totalEvents == 0 {
		return 1.0
	}
	
	playRatio := float64(playCount) / float64(totalEvents)
	return 0.2 + (playRatio * 1.8)
}

func (ps *ProxyServer) calculateTransitionWeight(songID string) float64 {
	if ps.lastPlayed == nil {
		return 1.0
	}
	
	var probability float64
	err := ps.db.QueryRow(`SELECT COALESCE(probability, 0.5) FROM song_transitions 
		WHERE from_song_id = ? AND to_song_id = ?`, ps.lastPlayed.ID, songID).Scan(&probability)
	
	if err != nil {
		return 1.0
	}
	
	return 0.5 + probability
}

func (ps *ProxyServer) validateAndStoreCredentials(username, password string) {
	// Check if we already have these credentials stored
	ps.credentialsMutex.RLock()
	if storedPassword, exists := ps.validCredentials[username]; exists && storedPassword == password {
		ps.credentialsMutex.RUnlock()
		return // Already validated and stored
	}
	ps.credentialsMutex.RUnlock()
	
	// Validate credentials against upstream server
	if ps.validateCredentials(username, password) {
		ps.credentialsMutex.Lock()
		ps.validCredentials[username] = password
		ps.credentialsMutex.Unlock()
		
		ps.logger.WithField("username", username).Info("Credentials validated and stored")
	} else {
		ps.logger.WithField("username", username).Warn("Invalid credentials provided")
	}
}

func (ps *ProxyServer) validateCredentials(username, password string) bool {
	// Test credentials by making a simple ping request to upstream server
	url := fmt.Sprintf("%s/rest/ping?u=%s&p=%s&v=1.15.0&c=subsoxy&f=json", 
		ps.config.UpstreamURL, username, password)
	
	// Create client with timeout to prevent hanging
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	resp, err := client.Get(url)
	if err != nil {
		ps.logger.WithError(err).WithField("username", username).Error("Failed to validate credentials")
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		ps.logger.WithFields(logrus.Fields{
			"username": username,
			"status_code": resp.StatusCode,
		}).Warn("Non-200 response when validating credentials")
		return false
	}
	
	var pingResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
		ps.logger.WithError(err).WithField("username", username).Error("Failed to decode ping response")
		return false
	}
	
	if subsonicResp, ok := pingResp["subsonic-response"].(map[string]interface{}); ok {
		if status, ok := subsonicResp["status"].(string); ok {
			if status == "ok" {
				ps.logger.WithField("username", username).Info("Successfully validated credentials")
				return true
			} else {
				ps.logger.WithField("username", username).Warn("Credentials validation failed - invalid username/password")
				return false
			}
		}
	}
	
	ps.logger.WithField("username", username).Error("Invalid response format from upstream server")
	return false
}

func (ps *ProxyServer) getValidCredentials() (string, string) {
	ps.credentialsMutex.RLock()
	defer ps.credentialsMutex.RUnlock()
	
	// Return the first valid credential pair we have
	for username, password := range ps.validCredentials {
		return username, password
	}
	
	return "", ""
}

func (ps *ProxyServer) handleShuffleEndpoint(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	sizeStr := r.URL.Query().Get("size")
	size := 50
	if sizeStr != "" {
		if parsedSize, err := strconv.Atoi(sizeStr); err == nil && parsedSize > 0 {
			size = parsedSize
		}
	}
	
	songs, err := ps.getWeightedShuffledSongs(size)
	if err != nil {
		ps.logger.WithError(err).Error("Failed to get weighted shuffled songs")
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
		ps.logger.WithError(err).Error("Failed to encode shuffle response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return true
	}
	
	ps.logger.WithFields(logrus.Fields{
		"size": size,
		"returned": len(songs),
	}).Info("Served weighted shuffle request")
	
	return true
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	var (
		port     = flag.String("port", getEnvOrDefault("PORT", "8080"), "Proxy server port")
		upstream = flag.String("upstream", getEnvOrDefault("UPSTREAM_URL", "http://localhost:4533"), "Upstream Subsonic server URL")
		logLevel = flag.String("log-level", getEnvOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
		dbPath   = flag.String("db-path", getEnvOrDefault("DB_PATH", "subsoxy.db"), "Database file path")
	)
	flag.Parse()

	config := &Config{
		ProxyPort:    *port,
		UpstreamURL:  *upstream,
		LogLevel:     *logLevel,
		DatabasePath: *dbPath,
	}

	server := NewProxyServer(config)

	server.AddHook("/rest/ping", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		server.logger.Info("Ping endpoint accessed")
		return false
	})

	server.AddHook("/rest/getLicense", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		server.logger.Info("License endpoint accessed")
		return false
	})

	server.AddHook("/rest/stream", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		songID := r.URL.Query().Get("id")
		if songID != "" {
			var previousSong *string
			if server.lastPlayed != nil {
				previousSong = &server.lastPlayed.ID
			}
			server.recordPlayEvent(songID, "start", previousSong)
		}
		return false
	})

	server.AddHook("/rest/scrobble", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		songID := r.URL.Query().Get("id")
		submission := r.URL.Query().Get("submission")
		if songID != "" {
			var previousSong *string
			if server.lastPlayed != nil {
				previousSong = &server.lastPlayed.ID
			}
			
			if submission == "true" {
				server.recordPlayEvent(songID, "play", previousSong)
				server.lastPlayed = &Song{ID: songID}
			} else {
				server.recordPlayEvent(songID, "skip", previousSong)
			}
		}
		return false
	})

	server.AddHook("/rest/getRandomSongs", server.handleShuffleEndpoint)

	server.Start()
}