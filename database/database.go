package database

import (
	"database/sql"
	"math"
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

// SQLite connection settings. WAL lets reads run alongside a write, the busy timeout makes
// a writer wait for the lock instead of failing with "database is locked", and immediate
// transactions take the write lock at BEGIN so that wait applies to them too.
const sqliteConnParams = "_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"

// Database operation constants
const (
	DefaultDateString = "1970-01-01"
)

// AdjustedHalfLife is how long it takes the decayed play and skip counters to lose half
// their value, so a play from a year ago counts for about a quarter of one from today.
const AdjustedHalfLife = 180 * 24 * time.Hour

// decayFactor returns the share of a decayed counter's value as of from that is left at to
func decayFactor(from, to time.Time) float64 {
	if !to.After(from) {
		return 1
	}
	return math.Exp2(-float64(to.Sub(from)) / float64(AdjustedHalfLife))
}

// decayedAt brings counters stored as of adjustedAt (Unix seconds, 0 when the song has
// no events) forward to now
func decayedAt(plays, skips float64, adjustedAt int64, now time.Time) (float64, float64) {
	if adjustedAt == 0 {
		return plays, skips
	}
	f := decayFactor(time.Unix(adjustedAt, 0), now)
	return plays * f, skips * f
}

// songColumns selects the columns scanSong reads
const songColumns = `id, title, artist, album, duration,
		COALESCE(last_played, '1970-01-01') as last_played,
		COALESCE(last_skipped, '1970-01-01') as last_skipped,
		COALESCE(play_count, 0) as play_count,
		COALESCE(skip_count, 0) as skip_count,
		COALESCE(adjusted_plays, 0.0) as adjusted_plays,
		COALESCE(adjusted_skips, 0.0) as adjusted_skips,
		COALESCE(adjusted_at, 0) as adjusted_at,
		COALESCE(cover_art, '') as cover_art`

// scanSong reads a row selected with songColumns, with the decayed counters as of now
func scanSong(rows *sql.Rows, now time.Time) (models.Song, error) {
	var song models.Song
	var lastPlayedStr, lastSkippedStr string
	var adjustedAt int64
	err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album, &song.Duration,
		&lastPlayedStr, &lastSkippedStr, &song.PlayCount, &song.SkipCount,
		&song.AdjustedPlays, &song.AdjustedSkips, &adjustedAt, &song.CoverArt)
	if err != nil {
		return song, err
	}

	if lastPlayedStr != DefaultDateString {
		song.LastPlayed, _ = parseTimestamp(lastPlayedStr)
	}
	if lastSkippedStr != DefaultDateString {
		song.LastSkipped, _ = parseTimestamp(lastSkippedStr)
	}
	song.AdjustedPlays, song.AdjustedSkips = decayedAt(song.AdjustedPlays, song.AdjustedSkips, adjustedAt, now)

	return song, nil
}

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

// sqliteDSN appends the connection settings to the database path, keeping any
// parameters the path already carries.
func sqliteDSN(dbPath string) string {
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	return dbPath + separator + sqliteConnParams
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
}

func New(dbPath string, logger *logrus.Logger) (*DB, error) {
	return NewWithPool(dbPath, logger, DefaultPoolConfig())
}

// NewWithPool creates a new database connection with custom pool configuration
func NewWithPool(dbPath string, logger *logrus.Logger, poolConfig *ConnectionPool) (*DB, error) {
	conn, err := sql.Open("sqlite3", sqliteDSN(dbPath))
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
		// Serves lookups by user and lets the shuffle page through a user's songs in id order
		// without sorting them. It covers everything the older user_id-only index did.
		`CREATE INDEX IF NOT EXISTS idx_songs_user_id_id ON songs(user_id, id)`,
		`DROP INDEX IF EXISTS idx_songs_user_id`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_user_id ON play_events(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_song_id ON play_events(song_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_timestamp ON play_events(timestamp)`,
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

	// Add adjusted_plays and adjusted_skips columns if they don't exist
	if err := db.addAdjustedPlaySkipColumns(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add adjusted play/skip columns")
	}

	// Add adjusted_at column if it doesn't exist
	if err := db.addAdjustedAtColumn(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add adjusted_at column")
	}

	// Drop the obsolete song_transitions table if it still exists
	if err := db.dropSongTransitionsTable(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to drop song_transitions table")
	}

	// Drop the obsolete artist_stats table if it still exists
	if err := db.dropArtistStatsTable(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to drop artist_stats table")
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

// addAdjustedPlaySkipColumns adds the adjusted_plays and adjusted_skips columns to the songs table if they don't exist
func (db *DB) addAdjustedPlaySkipColumns() error {
	// Check if adjusted_plays column already exists
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('songs') WHERE name='adjusted_plays'`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for adjusted_plays column")
	}

	// If columns already exist, no migration needed
	if count > 0 {
		db.logger.Debug("adjusted_plays and adjusted_skips columns already exist, skipping migration")
		return nil
	}

	// Add the adjusted_plays and adjusted_skips columns
	_, err = db.conn.Exec(`ALTER TABLE songs ADD COLUMN adjusted_plays REAL DEFAULT 0.0`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add adjusted_plays column")
	}

	_, err = db.conn.Exec(`ALTER TABLE songs ADD COLUMN adjusted_skips REAL DEFAULT 0.0`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add adjusted_skips column")
	}

	// Initialize adjusted values from raw counts for existing songs
	_, err = db.conn.Exec(`UPDATE songs SET adjusted_plays = play_count, adjusted_skips = skip_count WHERE adjusted_plays = 0.0 AND adjusted_skips = 0.0`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to initialize adjusted values from raw counts")
	}

	db.logger.Info("Added adjusted_plays and adjusted_skips columns to songs table and initialized from raw counts")
	return nil
}

// addAdjustedAtColumn adds adjusted_at, the time (Unix seconds) the decayed counters were
// last brought up to date, so they can keep decaying between events
func (db *DB) addAdjustedAtColumn() error {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('songs') WHERE name='adjusted_at'`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for adjusted_at column")
	}
	if count > 0 {
		return nil
	}

	if _, err := db.conn.Exec(`ALTER TABLE songs ADD COLUMN adjusted_at INTEGER`); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to add adjusted_at column")
	}

	// Existing counters were last updated at the song's latest event. The stored timestamps are
	// local time, read here as UTC; off by a few hours at most, which a 180-day half-life ignores.
	_, err = db.conn.Exec(`UPDATE songs SET adjusted_at = CAST(strftime('%s', substr(MAX(COALESCE(last_played, ''), COALESCE(last_skipped, '')), 1, 19)) AS INTEGER)
		WHERE adjusted_plays > 0 OR adjusted_skips > 0`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to initialize adjusted_at")
	}

	db.logger.Info("Added adjusted_at column to songs table")
	return nil
}

// dropSongTransitionsTable removes the song_transitions table (and its indexes), plus any backup
// copy made by migrateExistingData, left over from databases created before transition tracking
// was removed, then reclaims the freed disk space.
func (db *DB) dropSongTransitionsTable() error {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('song_transitions', 'song_transitions_backup')`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for song_transitions table")
	}

	// If neither table exists, no migration needed
	if count == 0 {
		return nil
	}

	// Dropping the table also drops its indexes
	if _, err := db.conn.Exec(`DROP TABLE IF EXISTS song_transitions`); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to drop song_transitions table")
	}
	if _, err := db.conn.Exec(`DROP TABLE IF EXISTS song_transitions_backup`); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to drop song_transitions_backup table")
	}

	db.logger.Info("Dropped obsolete song_transitions table")

	// The table can be large; VACUUM returns the freed pages to the filesystem.
	// Failure only means the file stays larger than necessary, so it is non-fatal.
	if _, err := db.conn.Exec(`VACUUM`); err != nil {
		db.logger.WithError(err).Warn("Failed to vacuum database after dropping song_transitions (non-fatal)")
	}

	return nil
}

// dropArtistStatsTable removes the artist_stats table (and its indexes) left over from
// databases created before artist weights were computed from the songs table.
func (db *DB) dropArtistStatsTable() error {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='artist_stats'`).Scan(&count)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_CHECK_FAILED", "failed to check for artist_stats table")
	}
	if count == 0 {
		return nil
	}

	// Dropping the table also drops its indexes
	if _, err := db.conn.Exec(`DROP TABLE IF EXISTS artist_stats`); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "MIGRATION_FAILED", "failed to drop artist_stats table")
	}

	db.logger.Info("Dropped obsolete artist_stats table")
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

	// Only the metadata is updated for existing songs, so play history is kept
	stmt, err := tx.Prepare(`INSERT INTO songs (id, user_id, title, artist, album, duration, cover_art)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, user_id) DO UPDATE SET
			title = excluded.title,
			artist = excluded.artist,
			album = excluded.album,
			duration = excluded.duration,
			cover_art = excluded.cover_art`)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to prepare song insert statement")
	}
	defer stmt.Close()

	var failedSongs []string
	for _, song := range songs {
		_, err := stmt.Exec(song.ID, userID, song.Title, song.Artist, song.Album, song.Duration, song.CoverArt)
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

	rows, err := db.conn.Query(`SELECT `+songColumns+`
		FROM songs WHERE user_id = ?`, userID)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query songs").
			WithContext("userID", userID)
	}
	defer rows.Close()

	now := time.Now()
	var songs []models.Song
	for rows.Next() {
		song, err := scanSong(rows, now)
		if err != nil {
			db.logger.WithError(err).WithField("userID", userID).Error("Failed to scan song")
			continue
		}

		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during song iteration").
			WithContext("userID", userID)
	}

	return songs, nil
}

// GetSongsBatchFiltered returns up to limit songs with an id greater than afterID, in id
// order, leaving out songs played or skipped after cutoffTime (2-week replay prevention).
// Pass "" for the first batch and the last song's id for each following one. Paging by id
// rather than offset keeps each batch an index range scan, and songs added or removed by a
// concurrent library sync can't shift the window so that a song is returned twice or missed.
func (db *DB) GetSongsBatchFiltered(userID string, limit int, afterID string, cutoffTime time.Time) ([]models.Song, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if limit <= 0 {
		return nil, errors.ErrValidationFailed.WithContext("field", "limit")
	}

	// Format the cutoff time for database comparison
	cutoffStr := cutoffTime.Format("2006-01-02 15:04:05")

	rows, err := db.conn.Query(`SELECT `+songColumns+`
		FROM songs WHERE user_id = ? AND (COALESCE(last_played, '1970-01-01') < ?) AND (COALESCE(last_skipped, '1970-01-01') < ?)
		AND id > ? ORDER BY id LIMIT ?`, userID, cutoffStr, cutoffStr, afterID, limit)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query filtered songs batch").
			WithContext("userID", userID).
			WithContext("limit", limit).
			WithContext("afterID", afterID).
			WithContext("cutoffTime", cutoffTime.Format("2006-01-02 15:04:05"))
	}
	defer rows.Close()

	now := time.Now()
	var songs []models.Song
	for rows.Next() {
		song, err := scanSong(rows, now)
		if err != nil {
			db.logger.WithError(err).WithFields(logrus.Fields{
				"userID":     userID,
				"limit":      limit,
				"afterID":    afterID,
				"cutoffTime": cutoffTime.Format("2006-01-02 15:04:05"),
			}).Error("Failed to scan song in filtered batch")
			continue
		}

		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during filtered song batch iteration").
			WithContext("userID", userID).
			WithContext("limit", limit).
			WithContext("afterID", afterID).
			WithContext("cutoffTime", cutoffTime.Format("2006-01-02 15:04:05"))
	}

	return songs, nil
}

// GetEligibleSongsByIDs returns the given songs for a user, leaving out any played or skipped
// after cutoffTime, with the same filtering as GetSongsBatchFiltered. IDs that are unknown
// or recently heard are simply missing from the result.
func (db *DB) GetEligibleSongsByIDs(userID string, songIDs []string, cutoffTime time.Time) ([]models.Song, error) {
	if userID == "" {
		return nil, errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if len(songIDs) == 0 {
		return nil, nil
	}

	cutoffStr := cutoffTime.Format("2006-01-02 15:04:05")

	placeholders := make([]string, len(songIDs))
	args := make([]interface{}, 0, len(songIDs)+3)
	args = append(args, userID, cutoffStr, cutoffStr)
	for i, songID := range songIDs {
		placeholders[i] = "?"
		args = append(args, songID)
	}

	rows, err := db.conn.Query(`SELECT `+songColumns+`
		FROM songs WHERE user_id = ? AND (COALESCE(last_played, '1970-01-01') < ?) AND (COALESCE(last_skipped, '1970-01-01') < ?)
		AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to query eligible songs by IDs").
			WithContext("userID", userID).
			WithContext("songCount", len(songIDs))
	}
	defer rows.Close()

	now := time.Now()
	var songs []models.Song
	for rows.Next() {
		song, err := scanSong(rows, now)
		if err != nil {
			db.logger.WithError(err).WithField("userID", userID).Error("Failed to scan eligible song")
			continue
		}

		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "error occurred during eligible songs by IDs iteration").
			WithContext("userID", userID)
	}

	return songs, nil
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

	// Note: We intentionally preserve play_events as historical data
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
	return db.RecordPlayEventAt(userID, songID, eventType, previousSong, time.Now())
}

// RecordPlayEventAt records a play or skip that happened at the given time, which may be in
// the past when a client delivers scrobbles it queued while offline
func (db *DB) RecordPlayEventAt(userID, songID, eventType string, previousSong *string, at time.Time) error {
	if userID == "" {
		return errors.ErrValidationFailed.WithContext("field", "userID")
	}
	if songID == "" {
		return errors.ErrValidationFailed.WithContext("field", "songID")
	}
	if eventType == "" {
		return errors.ErrValidationFailed.WithContext("field", "eventType")
	}

	// Use a transaction to ensure atomicity
	tx, err := db.conn.Begin()
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to begin transaction")
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO play_events (user_id, song_id, event_type, timestamp, previous_song) VALUES (?, ?, ?, ?, ?)`,
		userID, songID, eventType, at, previousSong)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to record play event").
			WithContext("user_id", userID).
			WithContext("song_id", songID).
			WithContext("event_type", eventType)
	}

	// Get the current adjusted values for this song to update stats
	// If the song doesn't exist in the database, we skip updating stats but still record the event
	var adjustedPlays, adjustedSkips float64
	var adjustedAt int64
	err = tx.QueryRow(`SELECT COALESCE(adjusted_plays, 0.0), COALESCE(adjusted_skips, 0.0), COALESCE(adjusted_at, 0) FROM songs WHERE id = ? AND user_id = ?`,
		songID, userID).Scan(&adjustedPlays, &adjustedSkips, &adjustedAt)
	if err == nil && (eventType == "play" || eventType == "skip") {
		// Decay the stored counters and the new event to whichever is later, so an event
		// delivered late counts for what it would have been worth had it arrived on time
		ref := at
		if stored := time.Unix(adjustedAt, 0); adjustedAt != 0 && stored.After(at) {
			ref = stored
		}
		adjustedPlays, adjustedSkips = decayedAt(adjustedPlays, adjustedSkips, adjustedAt, ref)
		eventWeight := decayFactor(at, ref)

		countColumn, timeColumn := "play_count", "last_played"
		if eventType == "play" {
			adjustedPlays += eventWeight
		} else {
			adjustedSkips += eventWeight
			countColumn, timeColumn = "skip_count", "last_skipped"
		}

		// The last played/skipped time only moves forward, even for a late event
		_, err := tx.Exec(`UPDATE songs SET `+countColumn+` = `+countColumn+` + 1,
			`+timeColumn+` = CASE WHEN `+timeColumn+` IS NULL OR `+timeColumn+` < ? THEN ? ELSE `+timeColumn+` END,
			adjusted_plays = ?, adjusted_skips = ?, adjusted_at = ?
			WHERE id = ? AND user_id = ?`,
			at, at, adjustedPlays, adjustedSkips, ref.Unix(), songID, userID)
		if err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to update song "+countColumn).
				WithContext("user_id", userID).
				WithContext("song_id", songID)
		}
	}
	// If song doesn't exist (err != nil), we skip stats updates but continue to record the event

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "TRANSACTION_FAILED", "failed to commit transaction")
	}

	return nil
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

// performHealthCheck pings the database and logs connection pool statistics
func (db *DB) performHealthCheck() {
	if err := db.conn.Ping(); err != nil {
		db.logger.WithError(err).Error("Database health check failed")
		return
	}

	stats := db.conn.Stats()

	db.logger.WithFields(logrus.Fields{
		"open_connections":     stats.OpenConnections,
		"idle_connections":     stats.Idle,
		"connections_in_use":   stats.InUse,
		"max_open_connections": stats.MaxOpenConnections,
	}).Debug("Database health check completed")
}

// AdjustedStats holds decayed play and skip totals.
type AdjustedStats struct {
	Plays float64
	Skips float64
}

// forEachHeardSong calls fn with the artist and decayed counters (as of now) of every song
// the user has played or skipped
func (db *DB) forEachHeardSong(userID string, fn func(artist string, plays, skips float64)) error {
	rows, err := db.conn.Query(`
		SELECT artist, adjusted_plays, adjusted_skips, COALESCE(adjusted_at, 0)
		FROM songs
		WHERE user_id = ? AND (adjusted_plays > 0 OR adjusted_skips > 0)
	`, userID)
	if err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to get adjusted stats").
			WithContext("user_id", userID)
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var artist string
		var plays, skips float64
		var adjustedAt int64
		if err := rows.Scan(&artist, &plays, &skips, &adjustedAt); err != nil {
			return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to scan adjusted stats").
				WithContext("user_id", userID)
		}
		plays, skips = decayedAt(plays, skips, adjustedAt, now)
		fn(artist, plays, skips)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, errors.CategoryDatabase, "QUERY_FAILED", "failed to iterate adjusted stats").
			WithContext("user_id", userID)
	}
	return nil
}

// GetAllArtistAdjustedStats returns the decayed play/skip totals of every artist the user
// has played or skipped, keyed by artist name. Artists without history are left out.
func (db *DB) GetAllArtistAdjustedStats(userID string) (map[string]AdjustedStats, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]AdjustedStats)
	err := db.forEachHeardSong(userID, func(artist string, plays, skips float64) {
		s := stats[artist]
		s.Plays += plays
		s.Skips += skips
		stats[artist] = s
	})
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// AdjustedTotals summarizes a user's listening history for the empirical priors
type AdjustedTotals struct {
	AdjustedStats     // Decayed play and skip totals across all songs
	HeardSongs    int // Songs with at least one play or skip
	HeardArtists  int // Artists with at least one play or skip
}

// GetAdjustedTotals returns the user's decayed play/skip totals and how many songs and
// artists they come from
func (db *DB) GetAdjustedTotals(userID string) (AdjustedTotals, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var totals AdjustedTotals
	artists := make(map[string]struct{})
	err := db.forEachHeardSong(userID, func(artist string, plays, skips float64) {
		totals.Plays += plays
		totals.Skips += skips
		totals.HeardSongs++
		artists[artist] = struct{}{}
	})
	if err != nil {
		return AdjustedTotals{}, err
	}
	totals.HeardArtists = len(artists)
	return totals, nil
}
