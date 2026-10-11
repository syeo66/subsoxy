package models

import (
	"encoding/xml"
	"net/http"
	"time"
)

type Song struct {
	ID            string    `json:"id" xml:"id,attr"`
	Title         string    `json:"title" xml:"title,attr"`
	Artist        string    `json:"artist" xml:"artist,attr"`
	Album         string    `json:"album" xml:"album,attr"`
	Duration      int       `json:"duration" xml:"duration,attr"`
	LastPlayed    time.Time `json:"lastPlayed" xml:"lastPlayed,attr"`
	LastSkipped   time.Time `json:"lastSkipped" xml:"lastSkipped,attr"`
	PlayCount     int       `json:"playCount" xml:"playCount,attr"`
	SkipCount     int       `json:"skipCount" xml:"skipCount,attr"`
	AdjustedPlays float64   `json:"adjustedPlays" xml:"adjustedPlays,attr"`
	AdjustedSkips float64   `json:"adjustedSkips" xml:"adjustedSkips,attr"`
	IsDir         bool      `json:"isDir" xml:"isDir,attr"`
	Name          string    `json:"name" xml:"name,attr"`
	CoverArt      string    `json:"coverArt,omitempty" xml:"coverArt,attr,omitempty"`
}

type PlayEvent struct {
	ID           int       `json:"id"`
	SongID       string    `json:"songId"`
	EventType    string    `json:"eventType"` // "play", "skip", "start"
	Timestamp    time.Time `json:"timestamp"`
	PreviousSong *string   `json:"previousSong,omitempty"`
}

type WeightedSong struct {
	Song   Song    `json:"song"`
	Weight float64 `json:"weight"`
}

type MusicFolder struct {
	ID   interface{} `json:"id"`
	Name string      `json:"name"`
}

type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Index struct {
	Name    string   `json:"name"`
	Artists []Artist `json:"artist"`
}

type SubsonicResponse struct {
	SubsonicResponse struct {
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
	} `json:"subsonic-response"`
}

// ResponseStatus returns the response's status, "ok" on success
func (r *SubsonicResponse) ResponseStatus() string { return r.SubsonicResponse.Status }

// SearchResult3Response is the response to search3, of which library sync only reads songs
type SearchResult3Response struct {
	SubsonicResponse struct {
		Status        string `json:"status"`
		SearchResult3 struct {
			Song []Song `json:"song"`
		} `json:"searchResult3"`
	} `json:"subsonic-response"`
}

// ResponseStatus returns the response's status, "ok" on success
func (r *SearchResult3Response) ResponseStatus() string { return r.SubsonicResponse.Status }

type Hook func(w http.ResponseWriter, r *http.Request, endpoint string) bool

// SonicSimilarityResponse is the OpenSubsonic response for getSonicSimilarTracks / findSonicPath.
// Each sonicMatch entry contains a Child (entry) and a normalized similarity score (0.0–1.0).
type SonicSimilarityResponse struct {
	SubsonicResponse struct {
		Status     string `json:"status"`
		SonicMatch []struct {
			Entry struct {
				ID string `json:"id"`
			} `json:"entry"`
			Similarity float64 `json:"similarity"`
		} `json:"sonicMatch"`
	} `json:"subsonic-response"`
}

// XML response structures for Subsonic API
type XMLSubsonicResponse struct {
	XMLName xml.Name  `xml:"subsonic-response"`
	Status  string    `xml:"status,attr"`
	Version string    `xml:"version,attr"`
	Songs   *XMLSongs `xml:"songs,omitempty"`
	Error   *XMLError `xml:"error,omitempty"`
}

// XMLError is the error element of a failed Subsonic response
type XMLError struct {
	Code    int    `xml:"code,attr"`
	Message string `xml:"message,attr"`
}

type XMLSongs struct {
	XMLName xml.Name `xml:"songs"`
	Song    []Song   `xml:"song"`
}
