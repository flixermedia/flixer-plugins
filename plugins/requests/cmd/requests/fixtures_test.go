package main

import (
	"strings"
	"testing"
)

const (
	adminID   = "11111111-1111-4111-8111-111111111111"
	userID    = "22222222-2222-4222-8222-222222222222"
	otherID   = "33333333-3333-4333-8333-333333333333"
	radarrID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sonarrID  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	lidarrID  = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	radarrKey = "radarr-api-key-value"
	tmdbKey   = "tmdb-secret-value"
	discogs   = "discogs-secret-value"
)

var baseSettings = map[string]any{"tmdbApiKey": tmdbKey, "discogsToken": discogs}

func withSettings(extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range baseSettings {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func service(id, kind, name string) serviceRow {
	min := "released"
	s := serviceRow{
		ID: id, Kind: kind, Name: name, URL: "https://" + kind + ".example.com", IsDefault: true, Enabled: true,
		RootFolder: "/media", QualityProfileID: 4, SeasonFolders: true, Tags: []int{}, CreatedAt: "2026-01-01T00:00:00.000Z",
	}
	if kind == "radarr" {
		s.MinimumAvailability = &min
	}
	if kind == "lidarr" {
		meta := 1
		s.MetadataProfileID = &meta
	}
	return s
}

// fakeNoTVDB makes the fake TMDB report no TVDB id for the show.
var fakeNoTVDB bool

// fakeMovieExists makes the fake Radarr already have the movie.
var fakeMovieExists bool

func fixture(t *testing.T) *fakeHost {
	h := newFakeHost(t)
	h.users = []hostUser{
		{ID: adminID, Username: "root", Role: "admin"},
		{ID: userID, Username: "alice", Role: "user"},
		{ID: otherID, Username: "bob", Role: "user"},
	}
	h.seed(tableServices, service(radarrID, "radarr", "Movies"), service(sonarrID, "sonarr", "Shows"), service(lidarrID, "lidarr", "Music"))
	h.secrets["arr."+radarrID] = radarrKey
	h.secrets["arr."+sonarrID] = "sonarr-key"
	h.secrets["arr."+lidarrID] = "lidarr-key"
	h.http = defaultRouter(h)
	return h
}

const matrixJSON = `{"id":603,"title":"The Matrix","release_date":"1999-03-30","poster_path":"/matrix.jpg","overview":"A hacker learns the truth.","runtime":136,"vote_average":8.2,"genres":[{"name":"Action"}],"credits":{"cast":[{"name":"Keanu Reeves"}]}}`

const thronesJSON = `{"id":1399,"name":"Game of Thrones","first_air_date":"2011-04-17","poster_path":"/got.jpg","overview":"Noble families.","episode_run_time":[60],"seasons":[{"season_number":0,"name":"Specials","episode_count":3},{"season_number":1,"name":"Season 1","episode_count":10},{"season_number":2,"name":"Season 2","episode_count":10},{"season_number":3,"name":"Season 3","episode_count":10}]}`

const albumJSON = `{"id":5,"title":"Discovery","year":2001,"artists":[{"name":"Daft Punk"}],"images":[{"uri":"https://img.discogs.com/discovery.jpg"}],"genres":["Electronic"],"tracklist":[{"title":"One More Time"}]}`

// defaultRouter fakes TMDB, Discogs, Radarr, Sonarr and Lidarr.
func defaultRouter(h *fakeHost) func(method, rawURL string, headers map[string]string, body string) (int, string) {
	return func(method, rawURL string, headers map[string]string, body string) (int, string) {
		u := mustParseURL(h.t, rawURL)
		path := u.Path
		switch u.Host {
		case "api.themoviedb.org":
			switch {
			case path == "/3/movie/603":
				return 200, matrixJSON
			case path == "/3/tv/1399":
				return 200, thronesJSON
			case path == "/3/tv/1399/external_ids":
				if fakeNoTVDB {
					return 200, `{"tvdb_id":null}`
				}
				return 200, `{"tvdb_id":121361}`
			case path == "/3/search/multi":
				return 200, `{"results":[{"id":603,"media_type":"movie","title":"The Matrix","release_date":"1999-03-30","poster_path":"/matrix.jpg"},{"id":1399,"media_type":"tv","name":"Game of Thrones","first_air_date":"2011-04-17"},{"id":7,"media_type":"person","name":"Someone"}]}`
			case strings.HasPrefix(path, "/3/trending"), strings.HasSuffix(path, "/popular"), strings.HasSuffix(path, "/upcoming"):
				return 200, `{"results":[{"id":603,"media_type":"movie","title":"The Matrix","release_date":"1999-03-30","poster_path":"/matrix.jpg"},{"id":1399,"media_type":"tv","name":"Game of Thrones","first_air_date":"2011-04-17","poster_path":"/got.jpg"}]}`
			}
			return 404, `{}`
		case "api.discogs.com":
			switch {
			case path == "/masters/5":
				return 200, albumJSON
			case path == "/database/search":
				return 200, `{"results":[{"master_id":5,"title":"Daft Punk - Discovery","year":"2001","cover_image":"https://img.discogs.com/discovery.jpg"}]}`
			}
			return 404, `{}`
		case "radarr.example.com":
			switch {
			case method == "GET" && path == "/api/v3/system/status":
				return 200, `{"version":"5.0.0"}`
			case method == "GET" && path == "/api/v3/qualityprofile":
				return 200, `[{"id":4,"name":"HD"}]`
			case method == "GET" && path == "/api/v3/rootfolder":
				return 200, `[{"path":"/media","freeSpace":10}]`
			case method == "GET" && path == "/api/v3/movie":
				if fakeMovieExists {
					return 200, `[{"id":1,"hasFile":false}]`
				}
				return 200, `[]`
			case method == "GET" && path == "/api/v3/movie/lookup/tmdb":
				return 200, `{"title":"The Matrix","tmdbId":603,"year":1999}`
			case method == "POST" && path == "/api/v3/movie":
				return 201, `{"id":1}`
			}
		case "sonarr.example.com":
			switch {
			case method == "GET" && path == "/api/v3/system/status":
				return 200, `{"version":"4.0.0"}`
			case method == "GET" && path == "/api/v3/qualityprofile":
				return 200, `[{"id":4,"name":"HD"}]`
			case method == "GET" && path == "/api/v3/rootfolder":
				return 200, `[{"path":"/tv","freeSpace":10}]`
			case method == "GET" && path == "/api/v3/languageprofile":
				return 404, `{}`
			case method == "GET" && path == "/api/v3/series":
				return 200, `[]`
			case method == "GET" && path == "/api/v3/series/lookup":
				return 200, `[{"title":"Game of Thrones","tvdbId":121361,"seasons":[{"seasonNumber":0},{"seasonNumber":1},{"seasonNumber":2},{"seasonNumber":3}]}]`
			case method == "POST" && path == "/api/v3/series":
				return 201, `{"id":2}`
			}
		case "lidarr.example.com":
			switch {
			case method == "GET" && path == "/api/v1/album/lookup":
				return 200, `[{"title":"Discovery","foreignAlbumId":"abc","artist":{"artistName":"Daft Punk"}}]`
			case method == "POST" && path == "/api/v1/album":
				return 201, `{"id":3}`
			}
		}
		return 404, `{}`
	}
}
