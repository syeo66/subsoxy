package models

import (
	"encoding/xml"
	"net/http"
	"time"
)

type Song struct {
	ID         string    `json:"id" xml:"id,attr"`
	Title      string    `json:"title" xml:"title,attr"`
	Artist     string    `json:"artist" xml:"artist,attr"`
	Album      string    `json:"album" xml:"album,attr"`
	Duration   int       `json:"duration" xml:"duration,attr"`
	LastPlayed time.Time `json:"lastPlayed" xml:"lastPlayed,attr"`
	PlayCount  int       `json:"playCount" xml:"playCount,attr"`
	SkipCount  int       `json:"skipCount" xml:"skipCount,attr"`
	IsDir      bool      `json:"isDir" xml:"isDir,attr"`
	Name       string    `json:"name" xml:"name,attr"`
	CoverArt   string    `json:"coverArt,omitempty" xml:"coverArt,attr,omitempty"`
}

type PlayEvent struct {
	ID           int       `json:"id"`
	SongID       string    `json:"songId"`
	EventType    string    `json:"eventType"` // "play", "skip", "start"
	Timestamp    time.Time `json:"timestamp"`
	PreviousSong *string   `json:"previousSong,omitempty"`
}

type SongTransition struct {
	FromSongID  string  `json:"fromSongId"`
	ToSongID    string  `json:"toSongId"`
	PlayCount   int     `json:"playCount"`
	SkipCount   int     `json:"skipCount"`
	Probability float64 `json:"probability"`
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

type Hook func(w http.ResponseWriter, r *http.Request, endpoint string) bool

// XML response structures for Subsonic API
type XMLSubsonicResponse struct {
	XMLName xml.Name  `xml:"subsonic-response"`
	Status  string    `xml:"status,attr"`
	Version string    `xml:"version,attr"`
	Songs   *XMLSongs `xml:"songs,omitempty"`
}

type XMLSongs struct {
	XMLName xml.Name `xml:"songs"`
	Song    []Song   `xml:"song"`
}
