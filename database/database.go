package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	_ "github.com/mattn/go-sqlite3"
	
	"github.com/syeo66/subsoxy/models"
)

type DB struct {
	conn   *sql.DB
	logger *logrus.Logger
}

func New(dbPath string, logger *logrus.Logger) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(); err != nil {
		return nil, err
	}

	db := &DB{
		conn:   conn,
		logger: logger,
	}

	if err := db.createTables(); err != nil {
		return nil, err
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) createTables() error {
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
		if _, err := db.conn.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (db *DB) StoreSongs(songs []models.Song) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO songs (id, title, artist, album, duration, play_count, skip_count) 
		VALUES (?, ?, ?, ?, ?, COALESCE((SELECT play_count FROM songs WHERE id = ?), 0), COALESCE((SELECT skip_count FROM songs WHERE id = ?), 0))`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, song := range songs {
		_, err := stmt.Exec(song.ID, song.Title, song.Artist, song.Album, song.Duration, song.ID, song.ID)
		if err != nil {
			db.logger.WithError(err).WithField("songId", song.ID).Error("Failed to insert song")
			continue
		}
	}

	return tx.Commit()
}

func (db *DB) GetAllSongs() ([]models.Song, error) {
	rows, err := db.conn.Query(`SELECT id, title, artist, album, duration, 
		COALESCE(last_played, '1970-01-01') as last_played, 
		COALESCE(play_count, 0) as play_count, 
		COALESCE(skip_count, 0) as skip_count 
		FROM songs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var song models.Song
		var lastPlayedStr string
		err := rows.Scan(&song.ID, &song.Title, &song.Artist, &song.Album, 
			&song.Duration, &lastPlayedStr, &song.PlayCount, &song.SkipCount)
		if err != nil {
			db.logger.WithError(err).Error("Failed to scan song")
			continue
		}
		
		if lastPlayedStr != "1970-01-01" {
			song.LastPlayed, _ = time.Parse("2006-01-02 15:04:05", lastPlayedStr)
		}
		
		songs = append(songs, song)
	}
	
	return songs, nil
}

func (db *DB) RecordPlayEvent(songID, eventType string, previousSong *string) error {
	now := time.Now()
	
	_, err := db.conn.Exec(`INSERT INTO play_events (song_id, event_type, timestamp, previous_song) VALUES (?, ?, ?, ?)`,
		songID, eventType, now, previousSong)
	if err != nil {
		return err
	}

	if eventType == "play" {
		_, err := db.conn.Exec(`UPDATE songs SET play_count = play_count + 1, last_played = ? WHERE id = ?`, now, songID)
		if err != nil {
			return err
		}
	} else if eventType == "skip" {
		_, err := db.conn.Exec(`UPDATE songs SET skip_count = skip_count + 1 WHERE id = ?`, songID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) RecordTransition(fromSongID, toSongID, eventType string) error {
	if eventType == "play" {
		_, err := db.conn.Exec(`INSERT OR REPLACE INTO song_transitions (from_song_id, to_song_id, play_count, skip_count)
			VALUES (?, ?, COALESCE((SELECT play_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0) + 1,
			COALESCE((SELECT skip_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0))`,
			fromSongID, toSongID, fromSongID, toSongID, fromSongID, toSongID)
		if err != nil {
			return err
		}
	} else if eventType == "skip" {
		_, err := db.conn.Exec(`INSERT OR REPLACE INTO song_transitions (from_song_id, to_song_id, play_count, skip_count)
			VALUES (?, ?, COALESCE((SELECT play_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0),
			COALESCE((SELECT skip_count FROM song_transitions WHERE from_song_id = ? AND to_song_id = ?), 0) + 1)`,
			fromSongID, toSongID, fromSongID, toSongID, fromSongID, toSongID)
		if err != nil {
			return err
		}
	}

	return db.updateTransitionProbabilities(fromSongID, toSongID)
}

func (db *DB) updateTransitionProbabilities(fromSongID, toSongID string) error {
	_, err := db.conn.Exec(`UPDATE song_transitions 
		SET probability = CAST(play_count AS REAL) / CAST((play_count + skip_count) AS REAL)
		WHERE from_song_id = ? AND to_song_id = ? AND (play_count + skip_count) > 0`,
		fromSongID, toSongID)
	return err
}

func (db *DB) GetTransitionProbability(fromSongID, toSongID string) (float64, error) {
	var probability float64
	err := db.conn.QueryRow(`SELECT COALESCE(probability, 0.5) FROM song_transitions 
		WHERE from_song_id = ? AND to_song_id = ?`, fromSongID, toSongID).Scan(&probability)
	
	if err != nil {
		return 0.5, err
	}
	
	return probability, nil
}