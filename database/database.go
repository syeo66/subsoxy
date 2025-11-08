package database

import (
	"database/sql"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"

	"github.com/syeo66/subsoxy/errors"
	"github.com/syeo66/subsoxy/models"
)

// Database connection pool constants
const (
	DefaultMaxOpenConns    = 25
	DefaultMaxIdleConns    = 5
	DefaultConnMaxLifetime = 30 * time.Minute
	DefaultConnMaxIdleTime = 5 * time.Minute
	DefaultHealthCheck     = true
	HealthCheckInterval    = 30 * time.Second
)

// Database operation constants
const (
	DefaultTransitionProbability = 0.5
	DefaultDateString            = "1970-01-01"
)

// parseTimestamp tries multiple datetime formats to parse SQLite timestamps
// Handles both Go's time.Time format (with timezone) and SQLite's datetime() format (without timezone)
func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05.999999-07:00", // Go's time.Time format with timezone and fractional seconds
		"2006-01-02 15:04:05",              // SQLite's datetime() format
		"2006-01-02T15:04:05.999999-07:00", // RFC3339 variant with T separator
		"2006-01-02T15:04:05Z07:00",        // RFC3339 without fractional seconds
	}

	var lastErr error
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}

	return time.Time{}, lastErr
}

type DB struct {
	conn         *sql.DB
	logger       *logrus.Logger
	mu           sync.RWMutex
	pool         *ConnectionPool
	shutdownChan chan struct{}
}

// ConnectionPool manages database connection pool settings
type ConnectionPool struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	HealthCheck     bool
	mu              sync.RWMutex
	stats           ConnectionStats
}

// ConnectionStats tracks connection pool statistics
type ConnectionStats struct {
	OpenConnections   int
	IdleConnections   int
	ConnectionsInUse  int
	TotalConnections  int
	FailedConnections int
	HealthChecks      int
	LastHealthCheck   time.Time
}

func New(dbPath string, logger *logrus.Logger) (*DB, error) {
	return NewWithPool(dbPath, logger, DefaultPoolConfig())
}

// NewWithPool creates a new database connection with custom pool configuration
func NewWithPool(dbPath string, logger *logrus.Logger, poolConfig *ConnectionPool) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "CONNECTION_FAILED", "failed to open database").
			WithContext("path", dbPath)
	}

	// Configure connection pool settings
	conn.SetMaxOpenConns(poolConfig.MaxOpenConns)
	conn.SetMaxIdleConns(poolConfig.MaxIdleConns)
	conn.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)
	conn.SetConnMaxIdleTime(poolConfig.ConnMaxIdleTime)

	if err := conn.Ping(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "CONNECTION_FAILED", "failed to ping database").
			WithContext("path", dbPath)
	}

	db := &DB{
		conn:         conn,
		logger:       logger,
		pool:         poolConfig,
		shutdownChan: make(chan struct{}),
	}

	if err := db.createTables(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to create database tables").
			WithContext("path", dbPath)
	}

	// Start health check goroutine if enabled
	if poolConfig.HealthCheck {
		go db.healthCheckLoop()
	}

	return db, nil
}

// DefaultPoolConfig returns default connection pool configuration
func DefaultPoolConfig() *ConnectionPool {
	return &ConnectionPool{
		MaxOpenConns:    DefaultMaxOpenConns,
		MaxIdleConns:    DefaultMaxIdleConns,
		ConnMaxLifetime: DefaultConnMaxLifetime,
		ConnMaxIdleTime: DefaultConnMaxIdleTime,
		HealthCheck:     DefaultHealthCheck,
	}
}

func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Signal health check goroutine to stop
	select {
	case <-db.shutdownChan:
		// Already closed
	default:
		close(db.shutdownChan)
	}

	if err := db.conn.Close(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "CLOSE_FAILED", "failed to close database connection")
	}
	return nil
}

func (db *DB) createTables() error {
	// Create tables with user_id columns
	queries := []string{
		`CREATE TABLE IF NOT EXISTS songs (
			id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			title TEXT NOT NULL,
			artist TEXT NOT NULL,
			album TEXT NOT NULL,
			duration INTEGER NOT NULL,
			last_played DATETIME,
			last_skipped DATETIME,
			play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0,
			cover_art TEXT,
			PRIMARY KEY (id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS play_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			song_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			previous_song TEXT,
			FOREIGN KEY (song_id, user_id) REFERENCES songs(id, user_id),
			FOREIGN KEY (previous_song, user_id) REFERENCES songs(id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS song_transitions (
			user_id TEXT NOT NULL,
			from_song_id TEXT NOT NULL,
			to_song_id TEXT NOT NULL,
			play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0,
			probability REAL DEFAULT 0.0,
			PRIMARY KEY (user_id, from_song_id, to_song_id),
			FOREIGN KEY (from_song_id, user_id) REFERENCES songs(id, user_id),
			FOREIGN KEY (to_song_id, user_id) REFERENCES songs(id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS artist_stats (
			user_id TEXT NOT NULL,
			artist TEXT NOT NULL,
			play_count INTEGER DEFAULT 0,
			skip_count INTEGER DEFAULT 0,
			ratio REAL DEFAULT 0.5,
			PRIMARY KEY (user_id, artist)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_songs_user_id ON songs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_user_id ON play_events(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_song_id ON play_events(song_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_timestamp ON play_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_song_transitions_user_id ON song_transitions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_song_transitions_from ON song_transitions(from_song_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artist_stats_user_id ON artist_stats(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artist_stats_artist ON artist_stats(artist)`,
	}

	for _, query := range queries {
		if _, err := db.conn.Exec(query); err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to execute table creation query").
				WithContext("query", query)
		}
	}

	// Check if we need to migrate existing data
	if err := db.migrateExistingData(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to migrate existing data")
	}

	// Add cover_art column if it doesn't exist
	if err := db.addCoverArtColumn(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add cover_art column")
	}

	// Add last_skipped column if it doesn't exist
	if err := db.addLastSkippedColumn(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add last_skipped column")
	}

	// Migrate artist statistics for existing users
	if err := db.MigrateArtistStats(); err != nil {
		db.logger.WithError(err).Warn("Failed to migrate artist statistics (non-fatal)")
		// Don't fail the entire initialization if artist stats migration fails
	}

	return nil
}

// migrateExistingData handles migration of existing data to the new schema
func (db *DB) migrateExistingData() error {
	// Check if the old songs table exists without user_id column
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('songs') WHERE name='user_id'`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for user_id column")
	}

	// If user_id column exists, no migration needed
	if count > 0 {
		return nil
	}

	// Check if there's existing data to migrate
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM songs`).Scan(&count)
	if err != nil {
		// If the table doesn't exist yet, that's fine - new install
		return nil
	}

	// If no existing data, no migration needed
	if count == 0 {
		return nil
	}

	db.logger.Info("Migrating existing data to multi-tenant schema")

	// Create temporary tables for backup
	backupQueries := []string{
		`CREATE TABLE IF NOT EXISTS songs_backup AS SELECT * FROM songs`,
		`CREATE TABLE IF NOT EXISTS play_events_backup AS SELECT * FROM play_events`,
		`CREATE TABLE IF NOT EXISTS song_transitions_backup AS SELECT * FROM song_transitions`,
	}

	for _, query := range backupQueries {
		if _, err := db.conn.Exec(query); err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "BACKUP_FAILED", "failed to create backup table").
				WithContext("query", query)
		}
	}

	// Drop existing tables
	dropQueries := []string{
		`DROP TABLE IF EXISTS songs`,
		`DROP TABLE IF EXISTS play_events`,
		`DROP TABLE IF EXISTS song_transitions`,
	}

	for _, query := range dropQueries {
		if _, err := db.conn.Exec(query); err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "DROP_FAILED", "failed to drop existing table").
				WithContext("query", query)
		}
	}

	// Recreate tables with new schema (this will be handled by the calling function)
	// No need to recreate here as createTables will handle it

	// Note: Since we're changing the schema significantly, existing data will be lost
	// This is acceptable for this migration as we're fundamentally changing the data model
	// Users will need to re-sync their data after the migration

	db.logger.Info("Migration completed - existing data backed up, new schema created")
	return nil
}

// addCoverArtColumn adds the cover_art column to the songs table if it doesn't exist
func (db *DB) addCoverArtColumn() error {
	// Check if cover_art column already exists
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('songs') WHERE name='cover_art'`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for cover_art column")
	}

	// If column already exists, no migration needed
	if count > 0 {
		return nil
	}

	// Add the cover_art column
	_, err = db.conn.Exec(`ALTER TABLE songs ADD COLUMN cover_art TEXT`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add cover_art column")
	}

	db.logger.Info("Added cover_art column to songs table")
	return nil
}

// addLastSkippedColumn adds the last_skipped column to the songs table if it doesn't exist
func (db *DB) addLastSkippedColumn() error {
	// Check if last_skipped column already exists
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('songs') WHERE name='last_skipped'`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for last_skipped column")
	}

	// If column already exists, no migration needed
	if count > 0 {
		return nil
	}

	// Add the last_skipped column
	_, err = db.conn.Exec(`ALTER TABLE songs ADD COLUMN last_skipped DATETIME`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add last_skipped column")
	}

	db.logger.Info("Added last_skipped column to songs table")
	return nil
}

func (db *DB) StoreSongs(userID string, songs []models.Song) error {
	if userID == "" {
		return errors.ErrValidationFailed.WithContext("field", "userID")
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to begin transaction")
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO songs (id, user_id, title, artist, album, duration, cover_art, play_count, skip_count, last_played, last_skipped) 
		VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT play_count FROM songs WHERE id = ? AND user_id = ?), 0), COALESCE((SELECT skip_count FROM songs WHERE id = ? AND user_id = ?), 0), COALESCE((SELECT last_played FROM songs WHERE id = ? AND user_id = ?), NULL), COALESCE((SELECT last_skipped FROM songs WHERE id = ? AND user_id = ?), NULL))`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to prepare song insert statement")
	}
	defer stmt.Close()

	var failedSongs []string
	for _, song := range songs {
		_, err := stmt.Exec(song.ID, userID, song.Title, song.Artist, song.Album, song.Duration, song.CoverArt, song.ID, userID, song.ID, userID, song.ID, userID, song.ID, userID)
		if err != nil {
			db.logger.WithError(err).WithFields(logrus.Fields{
				"songId": song.ID,
				"userID": userID,
			}).Error("Failed to insert song")
			failedSongs = append(failedSongs, song.ID)
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to commit transaction").
			WithContext("failed_songs", failedSongs).
			WithContext("userID", userID)
	}

	return nil
}

func (db *DB) GetAllSongs(userID string) ([]models.Song, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}

	rows, err := db.conn.Query(`SELECT id, title, artist, album, duration, 
		COALESCE(last_played, '1970-01-01') as last_played, 
		COALESCE(last_skipped, '1970-01-01') as last_skipped,
		COALESCE(play_count, 0) as play_count, 
		COALESCE(skip_count, 0) as skip_count,
		COALESCE(cover_art, '') as cover_art 
		FROM songs WHERE user_id = ?`, userID)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query songs").
			WithContext("userID", userID)
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var song models.Song
		var lastPlayedStr, lastSkippedStr string
		err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album,
			&song.Duration, &lastPlayedStr, &lastSkippedStr, &song.PlayCount, &song.SkipCount, &song.CoverArt)
		if err != nil {
			db.logger.WithError(err).WithField("userID", userID).Error("Failed to scan song")
			continue
		}

		if lastPlayedStr != DefaultDateString {
			song.LastPlayed, _ = parseTimestamp(lastPlayedStr)
		}

		if lastSkippedStr != DefaultDateString {
			song.LastSkipped, _ = parseTimestamp(lastSkippedStr)
		}

		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during song iteration").
			WithContext("userID", userID)
	}

	return songs, nil
}

// GetSongCount returns the total number of songs for a user
func (db *DB) GetSongCount(userID string) (int, error) {
	if userID == "" {
		return 0, errors.ErrValidationFailed.WithContext("field", "userID")
	}

	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM songs WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get song count").
			WithContext("userID", userID)
	}

	return count, nil
}

// GetSongsBatch returns a batch of songs for a user with pagination
func (db *DB) GetSongsBatch(userID string, limit, offset int) ([]models.Song, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if limit <= 0 {
		return nil, errors.ErrValidationFailed.WithContext("field", "limit")
	}
	if offset < 0 {
		return nil, errors.ErrValidationFailed.WithContext("field", "offset")
	}

	rows, err := db.conn.Query(`SELECT id, title, artist, album, duration, 
		COALESCE(last_played, '1970-01-01') as last_played, 
		COALESCE(last_skipped, '1970-01-01') as last_skipped,
		COALESCE(play_count, 0) as play_count, 
		COALESCE(skip_count, 0) as skip_count,
		COALESCE(cover_art, '') as cover_art 
		FROM songs WHERE user_id = ? 
		ORDER BY id LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query songs batch").
			WithContext("userID", userID).
			WithContext("limit", limit).
			WithContext("offset", offset)
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var song models.Song
		var lastPlayedStr, lastSkippedStr string
		err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album,
			&song.Duration, &lastPlayedStr, &lastSkippedStr, &song.PlayCount, &song.SkipCount, &song.CoverArt)
		if err != nil {
			db.logger.WithError(err).WithFields(logrus.Fields{
				"userID": userID,
				"limit":  limit,
				"offset": offset,
			}).Error("Failed to scan song in batch")
			continue
		}

		if lastPlayedStr != DefaultDateString {
			song.LastPlayed, _ = parseTimestamp(lastPlayedStr)
		}

		if lastSkippedStr != DefaultDateString {
			song.LastSkipped, _ = parseTimestamp(lastSkippedStr)
		}

		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during song batch iteration").
			WithContext("userID", userID).
			WithContext("limit", limit).
			WithContext("offset", offset)
	}

	return songs, nil
}

// GetSongsBatchFiltered returns a batch of songs filtered by last played/skipped dates.
// Songs played or skipped after cutoffTime are excluded from results for 2-week replay prevention.
// Uses consistent COALESCE-based filtering with single cutoff time for robust NULL handling.
func (db *DB) GetSongsBatchFiltered(userID string, limit, offset int, cutoffTime time.Time) ([]models.Song, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if limit <= 0 {
		return nil, errors.ErrValidationFailed.WithContext("field", "limit")
	}
	if offset < 0 {
		return nil, errors.ErrValidationFailed.WithContext("field", "offset")
	}

	// Format the cutoff time for database comparison
	cutoffStr := cutoffTime.Format("2006-01-02 15:04:05")

	rows, err := db.conn.Query(`SELECT id, title, artist, album, duration, 
		COALESCE(last_played, '1970-01-01') as last_played, 
		COALESCE(last_skipped, '1970-01-01') as last_skipped,
		COALESCE(play_count, 0) as play_count, 
		COALESCE(skip_count, 0) as skip_count,
		COALESCE(cover_art, '') as cover_art 
		FROM songs WHERE user_id = ? AND (COALESCE(last_played, '1970-01-01') < ?) AND (COALESCE(last_skipped, '1970-01-01') < ?)
		ORDER BY id LIMIT ? OFFSET ?`, userID, cutoffStr, cutoffStr, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query filtered songs batch").
			WithContext("userID", userID).
			WithContext("limit", limit).
			WithContext("offset", offset).
			WithContext("cutoffTime", cutoffTime.Format("2006-01-02 15:04:05"))
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var song models.Song
		var lastPlayedStr, lastSkippedStr string
		err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album,
			&song.Duration, &lastPlayedStr, &lastSkippedStr, &song.PlayCount, &song.SkipCount, &song.CoverArt)
		if err != nil {
			db.logger.WithError(err).WithFields(logrus.Fields{
				"userID":     userID,
				"limit":      limit,
				"offset":     offset,
				"cutoffTime": cutoffTime.Format("2006-01-02 15:04:05"),
			}).Error("Failed to scan song in filtered batch")
			continue
		}

		if lastPlayedStr != DefaultDateString {
			song.LastPlayed, _ = parseTimestamp(lastPlayedStr)
		}

		if lastSkippedStr != DefaultDateString {
			song.LastSkipped, _ = parseTimestamp(lastSkippedStr)
		}

		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during filtered song batch iteration").
			WithContext("userID", userID).
			WithContext("limit", limit).
			WithContext("offset", offset).
			WithContext("cutoffTime", cutoffTime.Format("2006-01-02 15:04:05"))
	}

	return songs, nil
}

// GetSongCountFiltered returns the count of songs filtered by last played/skipped dates.
// Songs played or skipped after cutoffTime are excluded from the count for 2-week replay prevention.
// Uses consistent COALESCE-based filtering with single cutoff time for robust NULL handling.
func (db *DB) GetSongCountFiltered(userID string, cutoffTime time.Time) (int, error) {
	if userID == "" {
		return 0, errors.ErrValidationFailed.WithContext("field", "userID")
	}

	// Format the cutoff time for database comparison
	cutoffStr := cutoffTime.Format("2006-01-02 15:04:05")

	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM songs WHERE user_id = ? AND (COALESCE(last_played, '1970-01-01') < ?) AND (COALESCE(last_skipped, '1970-01-01') < ?)`,
		userID, cutoffStr, cutoffStr).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get filtered song count").
			WithContext("userID", userID).
			WithContext("cutoffTime", cutoffTime.Format("2006-01-02 15:04:05"))
	}

	return count, nil
}

// GetExistingSongIDs returns a map of all existing song IDs for a user
func (db *DB) GetExistingSongIDs(userID string) (map[string]bool, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}

	rows, err := db.conn.Query(`SELECT id FROM songs WHERE user_id = ?`, userID)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query existing song IDs").
			WithContext("userID", userID)
	}
	defer rows.Close()

	songIDs := make(map[string]bool)
	for rows.Next() {
		var songID string
		if err := rows.Scan(&songID); err != nil {
			db.logger.WithError(err).WithField("userID", userID).Error("Failed to scan song ID")
			continue
		}
		songIDs[songID] = true
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during song ID iteration").
			WithContext("userID", userID)
	}

	return songIDs, nil
}

// GetSongsByIDs returns songs by their IDs for a specific user
func (db *DB) GetSongsByIDs(userID string, songIDs []string) (map[string]models.Song, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if len(songIDs) == 0 {
		return make(map[string]models.Song), nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(songIDs))
	args := make([]interface{}, 0, len(songIDs)+1)
	args = append(args, userID)

	for i, songID := range songIDs {
		placeholders[i] = "?"
		args = append(args, songID)
	}

	query := `SELECT id, title, artist, album, duration,
		COALESCE(cover_art, '') as cover_art
		FROM songs WHERE user_id = ? AND id IN (` +
		strings.Join(placeholders, ",") + `)`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query songs by IDs").
			WithContext("userID", userID).
			WithContext("songCount", len(songIDs))
	}
	defer rows.Close()

	songs := make(map[string]models.Song)
	for rows.Next() {
		var song models.Song
		err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album, &song.Duration, &song.CoverArt)
		if err != nil {
			db.logger.WithError(err).WithField("userID", userID).Error("Failed to scan song")
			continue
		}
		songs[song.ID] = song
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during songs by IDs iteration").
			WithContext("userID", userID)
	}

	return songs, nil
}

// DeleteSongs removes songs by ID for a user while preserving user data integrity
func (db *DB) DeleteSongs(userID string, songIDs []string) error {
	if userID == "" {
		return errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if len(songIDs) == 0 {
		return nil // Nothing to delete
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to begin transaction")
	}
	defer tx.Rollback()

	// Delete from songs table
	stmt, err := tx.Prepare(`DELETE FROM songs WHERE user_id = ? AND id = ?`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to prepare song delete statement")
	}
	defer stmt.Close()

	var failedSongs []string
	for _, songID := range songIDs {
		_, err := stmt.Exec(userID, songID)
		if err != nil {
			db.logger.WithError(err).WithFields(logrus.Fields{
				"songID": songID,
				"userID": userID,
			}).Error("Failed to delete song")
			failedSongs = append(failedSongs, songID)
			continue
		}
	}

	// Note: We intentionally preserve play_events and song_transitions as historical data
	// This maintains user listening history even if songs are removed from the library

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to commit transaction").
			WithContext("failed_songs", failedSongs).
			WithContext("userID", userID)
	}

	db.logger.WithFields(logrus.Fields{
		"userID":  userID,
		"deleted": len(songIDs) - len(failedSongs),
		"failed":  len(failedSongs),
		"total":   len(songIDs),
	}).Info("Completed song deletion")

	return nil
}

func (db *DB) RecordPlayEvent(userID, songID, eventType string, previousSong *string) error {
	if userID == "" {
		return errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if songID == "" {
		return errors.ErrValidationFailed.WithContext("field", "songID")
	}
	if eventType == "" {
		return errors.ErrValidationFailed.WithContext("field", "eventType")
	}

	now := time.Now()

	// Use a transaction to ensure atomicity
	tx, err := db.conn.Begin()
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to begin transaction")
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO play_events (user_id, song_id, event_type, timestamp, previous_song) VALUES (?, ?, ?, ?, ?)`,
		userID, songID, eventType, now, previousSong)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to record play event").
			WithContext("user_id", userID).
			WithContext("song_id", songID).
			WithContext("event_type", eventType)
	}

	// Get the artist name for this song to update artist stats
	var artist string
	err = tx.QueryRow(`SELECT artist FROM songs WHERE id = ? AND user_id = ?`, songID, userID).Scan(&artist)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get artist for song").
			WithContext("user_id", userID).
			WithContext("song_id", songID)
	}

	if eventType == "play" {
		_, err := tx.Exec(`UPDATE songs SET play_count = play_count + 1, last_played = ? WHERE id = ? AND user_id = ?`, now, songID, userID)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to update song play count").
				WithContext("user_id", userID).
				WithContext("song_id", songID)
		}

		// Update artist stats for play
		_, err = tx.Exec(`
			INSERT INTO artist_stats (user_id, artist, play_count, skip_count, ratio)
			VALUES (?, ?, 1, 0, 1.0)
			ON CONFLICT(user_id, artist) DO UPDATE SET
				play_count = play_count + 1,
				ratio = CAST(play_count + 1 AS REAL) / CAST(play_count + 1 + skip_count AS REAL)
		`, userID, artist)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to update artist stats for play").
				WithContext("user_id", userID).
				WithContext("artist", artist)
		}
	} else if eventType == "skip" {
		_, err := tx.Exec(`UPDATE songs SET skip_count = skip_count + 1, last_skipped = ? WHERE id = ? AND user_id = ?`, now, songID, userID)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to update song skip count").
				WithContext("user_id", userID).
				WithContext("song_id", songID)
		}

		// Update artist stats for skip
		_, err = tx.Exec(`
			INSERT INTO artist_stats (user_id, artist, play_count, skip_count, ratio)
			VALUES (?, ?, 0, 1, 0.0)
			ON CONFLICT(user_id, artist) DO UPDATE SET
				skip_count = skip_count + 1,
				ratio = CAST(play_count AS REAL) / CAST(play_count + skip_count + 1 AS REAL)
		`, userID, artist)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to update artist stats for skip").
				WithContext("user_id", userID).
				WithContext("artist", artist)
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to commit transaction")
	}

	return nil
}

func (db *DB) RecordTransition(userID, fromSongID, toSongID, eventType string) error {
	if userID == "" {
		return errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if fromSongID == "" || toSongID == "" {
		return errors.ErrValidationFailed.WithContext("missing_fields", []string{"fromSongID", "toSongID"})
	}
	if eventType == "" {
		return errors.ErrValidationFailed.WithContext("field", "eventType")
	}

	if eventType == "play" {
		_, err := db.conn.Exec(`INSERT OR REPLACE INTO song_transitions (user_id, from_song_id, to_song_id, play_count, skip_count)
			VALUES (?, ?, ?, COALESCE((SELECT play_count FROM song_transitions WHERE user_id = ? AND from_song_id = ? AND to_song_id = ?), 0) + 1,
			COALESCE((SELECT skip_count FROM song_transitions WHERE user_id = ? AND from_song_id = ? AND to_song_id = ?), 0))`,
			userID, fromSongID, toSongID, userID, fromSongID, toSongID, userID, fromSongID, toSongID)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to record play transition").
				WithContext("user_id", userID).
				WithContext("from_song_id", fromSongID).
				WithContext("to_song_id", toSongID)
		}
	} else if eventType == "skip" {
		_, err := db.conn.Exec(`INSERT OR REPLACE INTO song_transitions (user_id, from_song_id, to_song_id, play_count, skip_count)
			VALUES (?, ?, ?, COALESCE((SELECT play_count FROM song_transitions WHERE user_id = ? AND from_song_id = ? AND to_song_id = ?), 0),
			COALESCE((SELECT skip_count FROM song_transitions WHERE user_id = ? AND from_song_id = ? AND to_song_id = ?), 0) + 1)`,
			userID, fromSongID, toSongID, userID, fromSongID, toSongID, userID, fromSongID, toSongID)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to record skip transition").
				WithContext("user_id", userID).
				WithContext("from_song_id", fromSongID).
				WithContext("to_song_id", toSongID)
		}
	}

	return db.updateTransitionProbabilities(userID, fromSongID, toSongID)
}

func (db *DB) updateTransitionProbabilities(userID, fromSongID, toSongID string) error {
	_, err := db.conn.Exec(`UPDATE song_transitions 
		SET probability = CAST(play_count AS REAL) / CAST((play_count + skip_count) AS REAL)
		WHERE user_id = ? AND from_song_id = ? AND to_song_id = ? AND (play_count + skip_count) > 0`,
		userID, fromSongID, toSongID)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to update transition probabilities").
			WithContext("user_id", userID).
			WithContext("from_song_id", fromSongID).
			WithContext("to_song_id", toSongID)
	}
	return nil
}

func (db *DB) GetTransitionProbability(userID, fromSongID, toSongID string) (float64, error) {
	if userID == "" {
		return DefaultTransitionProbability, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if fromSongID == "" || toSongID == "" {
		return DefaultTransitionProbability, errors.ErrValidationFailed.WithContext("missing_fields", []string{"fromSongID", "toSongID"})
	}

	var probability float64
	err := db.conn.QueryRow(`SELECT COALESCE(probability, 0.5) FROM song_transitions 
		WHERE user_id = ? AND from_song_id = ? AND to_song_id = ?`, userID, fromSongID, toSongID).Scan(&probability)

	if err != nil {
		if err == sql.ErrNoRows {
			return DefaultTransitionProbability, nil // Default probability when no transition data exists
		}
		return DefaultTransitionProbability, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get transition probability").
			WithContext("user_id", userID).
			WithContext("from_song_id", fromSongID).
			WithContext("to_song_id", toSongID)
	}

	return probability, nil
}

// GetTransitionProbabilities returns transition probabilities for multiple songs in a batch
// to avoid N+1 queries when calculating weights for many songs
func (db *DB) GetTransitionProbabilities(userID, fromSongID string, toSongIDs []string) (map[string]float64, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if fromSongID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "fromSongID")
	}
	if len(toSongIDs) == 0 {
		return make(map[string]float64), nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(toSongIDs))
	args := make([]interface{}, 0, len(toSongIDs)+2)
	args = append(args, userID, fromSongID)

	for i, toSongID := range toSongIDs {
		placeholders[i] = "?"
		args = append(args, toSongID)
	}

	query := `SELECT to_song_id, COALESCE(probability, 0.5) as probability 
		FROM song_transitions 
		WHERE user_id = ? AND from_song_id = ? AND to_song_id IN (` +
		strings.Join(placeholders, ",") + `)`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get transition probabilities").
			WithContext("userID", userID).
			WithContext("fromSongID", fromSongID).
			WithContext("toSongCount", len(toSongIDs))
	}
	defer rows.Close()

	probabilities := make(map[string]float64)
	for rows.Next() {
		var toSongID string
		var probability float64
		if err := rows.Scan(&toSongID, &probability); err != nil {
			db.logger.WithError(err).WithFields(logrus.Fields{
				"userID":     userID,
				"fromSongID": fromSongID,
			}).Error("Failed to scan transition probability")
			continue
		}
		probabilities[toSongID] = probability
	}

	// Fill in default probabilities for songs not found
	for _, toSongID := range toSongIDs {
		if _, exists := probabilities[toSongID]; !exists {
			probabilities[toSongID] = DefaultTransitionProbability
		}
	}

	return probabilities, nil
}

// healthCheckLoop runs periodic health checks on the database connection
func (db *DB) healthCheckLoop() {
	ticker := time.NewTicker(HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			db.performHealthCheck()
		case <-db.shutdownChan:
			db.logger.Debug("Database health check loop shutting down")
			return
		}
	}
}

// performHealthCheck checks database connection health and updates statistics
func (db *DB) performHealthCheck() {
	db.pool.mu.Lock()
	defer db.pool.mu.Unlock()

	db.pool.stats.HealthChecks++
	db.pool.stats.LastHealthCheck = time.Now()

	if err := db.conn.Ping(); err != nil {
		db.pool.stats.FailedConnections++
		db.logger.WithError(err).Error("Database health check failed")
		return
	}

	// Update connection statistics
	stats := db.conn.Stats()
	db.pool.stats.OpenConnections = stats.OpenConnections
	db.pool.stats.IdleConnections = stats.Idle
	db.pool.stats.ConnectionsInUse = stats.InUse
	db.pool.stats.TotalConnections = int(stats.MaxOpenConnections)

	db.logger.WithFields(logrus.Fields{
		"open_connections":     stats.OpenConnections,
		"idle_connections":     stats.Idle,
		"connections_in_use":   stats.InUse,
		"max_open_connections": stats.MaxOpenConnections,
	}).Debug("Database health check completed")
}

// GetConnectionStats returns current connection pool statistics
func (db *DB) GetConnectionStats() ConnectionStats {
	db.pool.mu.RLock()
	defer db.pool.mu.RUnlock()

	// Update current stats from sql.DB
	stats := db.conn.Stats()
	db.pool.stats.OpenConnections = stats.OpenConnections
	db.pool.stats.IdleConnections = stats.Idle
	db.pool.stats.ConnectionsInUse = stats.InUse

	return db.pool.stats
}

// UpdatePoolConfig updates connection pool configuration
func (db *DB) UpdatePoolConfig(config *ConnectionPool) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if config.MaxOpenConns < 1 {
		return errors.New(errors.CategoryDatabase, "INVALID_POOL_CONFIG", "max open connections must be at least 1").
			WithContext("max_open_conns", config.MaxOpenConns)
	}

	if config.MaxIdleConns < 0 {
		return errors.New(errors.CategoryDatabase, "INVALID_POOL_CONFIG", "max idle connections cannot be negative").
			WithContext("max_idle_conns", config.MaxIdleConns)
	}

	if config.MaxIdleConns > config.MaxOpenConns {
		return errors.New(errors.CategoryDatabase, "INVALID_POOL_CONFIG", "max idle connections cannot exceed max open connections").
			WithContext("max_idle_conns", config.MaxIdleConns).
			WithContext("max_open_conns", config.MaxOpenConns)
	}

	// Apply new configuration
	db.conn.SetMaxOpenConns(config.MaxOpenConns)
	db.conn.SetMaxIdleConns(config.MaxIdleConns)
	db.conn.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.conn.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	db.pool.MaxOpenConns = config.MaxOpenConns
	db.pool.MaxIdleConns = config.MaxIdleConns
	db.pool.ConnMaxLifetime = config.ConnMaxLifetime
	db.pool.ConnMaxIdleTime = config.ConnMaxIdleTime
	db.pool.HealthCheck = config.HealthCheck

	db.logger.WithFields(logrus.Fields{
		"max_open_conns":     config.MaxOpenConns,
		"max_idle_conns":     config.MaxIdleConns,
		"conn_max_lifetime":  config.ConnMaxLifetime,
		"conn_max_idle_time": config.ConnMaxIdleTime,
		"health_check":       config.HealthCheck,
	}).Info("Database connection pool configuration updated")

	return nil
}

// GetArtistStats retrieves the play/skip statistics for a specific artist and user
func (db *DB) GetArtistStats(userID, artist string) (*models.ArtistStats, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var stats models.ArtistStats
	err := db.conn.QueryRow(`
		SELECT user_id, artist, play_count, skip_count, ratio
		FROM artist_stats
		WHERE user_id = ? AND artist = ?
	`, userID, artist).Scan(&stats.UserID, &stats.Artist, &stats.PlayCount, &stats.SkipCount, &stats.Ratio)

	if err == sql.ErrNoRows {
		// Return default stats if artist not found
		return &models.ArtistStats{
			UserID:    userID,
			Artist:    artist,
			PlayCount: 0,
			SkipCount: 0,
			Ratio:     0.5,
		}, nil
	}

	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get artist stats").
			WithContext("user_id", userID).
			WithContext("artist", artist)
	}

	return &stats, nil
}

// UpdateArtistStats updates the play/skip statistics for an artist
// This should be called when a song by this artist is played or skipped
func (db *DB) UpdateArtistStats(userID, artist, eventType string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tx, err := db.conn.Begin()
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to begin transaction for artist stats update").
			WithContext("user_id", userID).
			WithContext("artist", artist)
	}
	defer tx.Rollback()

	// Insert or update the artist stats
	if eventType == "play" {
		_, err = tx.Exec(`
			INSERT INTO artist_stats (user_id, artist, play_count, skip_count, ratio)
			VALUES (?, ?, 1, 0, 1.0)
			ON CONFLICT(user_id, artist) DO UPDATE SET
				play_count = play_count + 1,
				ratio = CAST(play_count AS REAL) / CAST(play_count + skip_count AS REAL)
		`, userID, artist)
	} else if eventType == "skip" {
		_, err = tx.Exec(`
			INSERT INTO artist_stats (user_id, artist, play_count, skip_count, ratio)
			VALUES (?, ?, 0, 1, 0.0)
			ON CONFLICT(user_id, artist) DO UPDATE SET
				skip_count = skip_count + 1,
				ratio = CAST(play_count AS REAL) / CAST(play_count + skip_count AS REAL)
		`, userID, artist)
	}

	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "UPDATE_FAILED", "failed to update artist stats").
			WithContext("user_id", userID).
			WithContext("artist", artist).
			WithContext("event_type", eventType)
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "COMMIT_FAILED", "failed to commit artist stats update").
			WithContext("user_id", userID).
			WithContext("artist", artist)
	}

	return nil
}

// CalculateInitialArtistStats calculates artist statistics from existing song data
// This should be called on application startup to populate the artist_stats table
func (db *DB) CalculateInitialArtistStats(userID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.logger.WithField("user_id", userID).Info("Calculating initial artist statistics")

	tx, err := db.conn.Begin()
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to begin transaction for artist stats calculation").
			WithContext("user_id", userID)
	}
	defer tx.Rollback()

	// Aggregate play/skip counts by artist from songs table
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO artist_stats (user_id, artist, play_count, skip_count, ratio)
		SELECT
			user_id,
			artist,
			SUM(play_count) as total_plays,
			SUM(skip_count) as total_skips,
			CASE
				WHEN (SUM(play_count) + SUM(skip_count)) = 0 THEN 0.5
				ELSE CAST(SUM(play_count) AS REAL) / CAST(SUM(play_count) + SUM(skip_count) AS REAL)
			END as ratio
		FROM songs
		WHERE user_id = ?
		GROUP BY user_id, artist
	`, userID)

	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "INSERT_FAILED", "failed to calculate artist stats").
			WithContext("user_id", userID)
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "COMMIT_FAILED", "failed to commit artist stats calculation").
			WithContext("user_id", userID)
	}

	// Log the number of artists processed
	var count int
	db.conn.QueryRow("SELECT COUNT(*) FROM artist_stats WHERE user_id = ?", userID).Scan(&count)
	db.logger.WithFields(logrus.Fields{
		"user_id":       userID,
		"artist_count":  count,
	}).Info("Artist statistics calculated successfully")

	return nil
}

// MigrateArtistStats populates artist statistics for all users in the database
// This is used for migrating existing databases when upgrading to a version with artist stats
func (db *DB) MigrateArtistStats() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.logger.Info("Starting artist statistics migration for all users")

	// Get all distinct users from the songs table
	rows, err := db.conn.Query(`SELECT DISTINCT user_id FROM songs`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get users for migration")
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			db.logger.WithError(err).Warn("Failed to scan user ID during migration")
			continue
		}
		users = append(users, userID)
	}

	if len(users) == 0 {
		db.logger.Info("No users found for artist stats migration")
		return nil
	}

	// Calculate artist stats for each user
	migratedCount := 0
	for _, userID := range users {
		// Check if artist stats already exist for this user
		var count int
		err := db.conn.QueryRow(`SELECT COUNT(*) FROM artist_stats WHERE user_id = ?`, userID).Scan(&count)
		if err != nil {
			db.logger.WithError(err).WithField("user_id", userID).Warn("Failed to check artist stats")
			continue
		}

		// Only migrate if no artist stats exist yet
		if count == 0 {
			tx, err := db.conn.Begin()
			if err != nil {
				db.logger.WithError(err).WithField("user_id", userID).Warn("Failed to begin transaction for migration")
				continue
			}

			_, err = tx.Exec(`
				INSERT INTO artist_stats (user_id, artist, play_count, skip_count, ratio)
				SELECT
					user_id,
					artist,
					SUM(play_count) as total_plays,
					SUM(skip_count) as total_skips,
					CASE
						WHEN (SUM(play_count) + SUM(skip_count)) = 0 THEN 0.5
						ELSE CAST(SUM(play_count) AS REAL) / CAST(SUM(play_count) + SUM(skip_count) AS REAL)
					END as ratio
				FROM songs
				WHERE user_id = ?
				GROUP BY user_id, artist
			`, userID)

			if err != nil {
				tx.Rollback()
				db.logger.WithError(err).WithField("user_id", userID).Warn("Failed to migrate artist stats for user")
				continue
			}

			if err := tx.Commit(); err != nil {
				db.logger.WithError(err).WithField("user_id", userID).Warn("Failed to commit artist stats migration")
				continue
			}

			// Count migrated artists
			var artistCount int
			db.conn.QueryRow("SELECT COUNT(*) FROM artist_stats WHERE user_id = ?", userID).Scan(&artistCount)

			db.logger.WithFields(logrus.Fields{
				"user_id":      userID,
				"artist_count": artistCount,
			}).Info("Migrated artist statistics for user")

			migratedCount++
		}
	}

	db.logger.WithFields(logrus.Fields{
		"total_users":    len(users),
		"migrated_users": migratedCount,
	}).Info("Completed artist statistics migration")

	return nil
}
