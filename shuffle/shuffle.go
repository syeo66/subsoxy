package shuffle

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	
	"github.com/syeo66/subsoxy/models"
	"github.com/syeo66/subsoxy/database"
)

type Service struct {
	db         *database.DB
	logger     *logrus.Logger
	lastPlayed map[string]*models.Song  // Map userID to last played song
	mu         sync.RWMutex             // Protects lastPlayed map
}

func New(db *database.DB, logger *logrus.Logger) *Service {
	return &Service{
		db:         db,
		logger:     logger,
		lastPlayed: make(map[string]*models.Song),
	}
}

func (s *Service) SetLastPlayed(userID string, song *models.Song) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPlayed[userID] = song
}

func (s *Service) GetWeightedShuffledSongs(userID string, count int) ([]models.Song, error) {
	songs, err := s.db.GetAllSongs(userID)
	if err != nil {
		return nil, err
	}

	weightedSongs := make([]models.WeightedSong, 0, len(songs))
	for _, song := range songs {
		weight := s.calculateSongWeight(userID, song)
		weightedSongs = append(weightedSongs, models.WeightedSong{
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

	result := make([]models.Song, 0, count)
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

func (s *Service) calculateSongWeight(userID string, song models.Song) float64 {
	baseWeight := 1.0
	
	timeWeight := s.calculateTimeDecayWeight(song.LastPlayed)
	playSkipWeight := s.calculatePlaySkipWeight(song.PlayCount, song.SkipCount)
	transitionWeight := s.calculateTransitionWeight(userID, song.ID)
	
	finalWeight := baseWeight * timeWeight * playSkipWeight * transitionWeight
	
	s.logger.WithFields(logrus.Fields{
		"userID":           userID,
		"songId":           song.ID,
		"timeWeight":       timeWeight,
		"playSkipWeight":   playSkipWeight,
		"transitionWeight": transitionWeight,
		"finalWeight":      finalWeight,
	}).Debug("Calculated song weight")
	
	return finalWeight
}

func (s *Service) calculateTimeDecayWeight(lastPlayed time.Time) float64 {
	if lastPlayed.IsZero() {
		return 2.0
	}
	
	daysSinceLastPlayed := time.Since(lastPlayed).Hours() / 24.0
	
	if daysSinceLastPlayed < 30 {
		return 0.1 + (daysSinceLastPlayed/30.0)*0.9
	}
	
	return 1.0 + math.Min(daysSinceLastPlayed/365.0, 1.0)
}

func (s *Service) calculatePlaySkipWeight(playCount, skipCount int) float64 {
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

func (s *Service) calculateTransitionWeight(userID, songID string) float64 {
	s.mu.RLock()
	lastPlayed, exists := s.lastPlayed[userID]
	s.mu.RUnlock()
	
	if !exists || lastPlayed == nil {
		return 1.0
	}
	
	probability, err := s.db.GetTransitionProbability(userID, lastPlayed.ID, songID)
	if err != nil {
		return 1.0
	}
	
	return 0.5 + probability
}