package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
)

const (
	tmdbAPI     = "https://api.themoviedb.org/3"
	tmdbImages  = "https://image.tmdb.org/t/p"
	discogsAPI  = "https://api.discogs.com"
	maxSearch   = 24
	noTMDBText  = "TMDB is not configured. Add a TMDB API key on the Settings tab."
	noDiscogs   = "Discogs is not configured. Add a Discogs token on the Settings tab."
	errNotFound = "Not found at the metadata provider."
)

var discoverSections = []string{"trending", "movies", "tv", "upcoming", "music"}

type card struct {
	Source    string `json:"source"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Poster    string `json:"poster"`
	Overview  string `json:"overview"`
	Status    string `json:"status"`
	RequestID string `json:"requestId"`
}

type seasonInfo struct {
	Number       int    `json:"number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episodeCount"`
	Status       string `json:"status"`
}

type detail struct {
	card
	Backdrop  string       `json:"backdrop"`
	Tagline   string       `json:"tagline"`
	Genres    []string     `json:"genres"`
	Runtime   int          `json:"runtime"`
	Rating    float64      `json:"rating"`
	Cast      []string     `json:"cast"`
	Artist    string       `json:"artist"`
	Seasons   []seasonInfo `json:"seasons"`
	Tracklist []string     `json:"tracklist"`
}

type obj = map[string]any

func asObj(value any) obj {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return obj{}
}

func asList(value any) []obj {
	list, _ := value.([]any)
	out := make([]obj, 0, len(list))
	for _, entry := range list {
		out = append(out, asObj(entry))
	}
	return out
}

func text(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func number(value any) (float64, bool) {
	n, ok := value.(float64)
	return n, ok
}

func tmdbImage(path any, size string) string {
	if s, ok := path.(string); ok && strings.HasPrefix(s, "/") {
		return tmdbImages + "/" + size + s
	}
	return ""
}

func idString(value any) string {
	switch v := value.(type) {
	case float64:
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0f", v)
		}
	case string:
		return v
	}
	return ""
}

func (s settings) tmdbCall(path string, params map[string]string) hostCall {
	query := url.Values{}
	query.Set("api_key", s.TMDBKey)
	query.Set("language", s.Language)
	for name, value := range params {
		query.Set(name, value)
	}
	return httpRequest("GET", tmdbAPI+path+"?"+query.Encode(), map[string]string{"accept": "application/json"}, "")
}

func (s settings) discogsCall(path string, params map[string]string) hostCall {
	query := url.Values{}
	for name, value := range params {
		query.Set(name, value)
	}
	target := discogsAPI + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return httpRequest("GET", target, map[string]string{
		"accept":        "application/json",
		"authorization": "Discogs token=" + s.DiscogsToken,
	}, "")
}

// decodeProvider parses a provider answer, or explains why it is unusable.
func decodeProvider(result hostResult) (obj, string) {
	if !result.OK {
		switch {
		case result.Status == 404:
			return nil, errNotFound
		case result.Status == 0:
			return nil, "The metadata provider could not be reached."
		default:
			return nil, fmt.Sprintf("The metadata provider answered with HTTP %d. Check its API key on the Settings tab.", result.Status)
		}
	}
	var body any
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		return nil, "The metadata provider sent an unreadable answer."
	}
	return asObj(body), ""
}

func tmdbCard(entry obj, mediaType string) (card, bool) {
	id, ok := number(entry["id"])
	titleKey, dateKey := "title", "release_date"
	if mediaType == "tv" {
		titleKey, dateKey = "name", "first_air_date"
	}
	title := text(entry[titleKey])
	if !ok || title == "" {
		return card{}, false
	}
	return card{
		Source: "tmdb", Type: mediaType, ID: fmt.Sprintf("%.0f", id), Title: title,
		Year: yearOf(entry[dateKey]), Poster: tmdbImage(entry["poster_path"], "w342"),
		Overview: text(entry["overview"]), Status: "none",
	}, true
}

func discogsCard(entry obj) (card, bool) {
	id := idString(entry["master_id"])
	if masterID, ok := number(entry["master_id"]); id == "" || (ok && masterID == 0) {
		id = idString(entry["id"])
	}
	label := text(entry["title"])
	if id == "" || label == "" {
		return card{}, false
	}
	artist, title := splitDiscogsTitle(label)
	cover := text(entry["cover_image"])
	if cover == "" {
		cover = text(entry["thumb"])
	}
	if strings.Contains(cover, "spacer.gif") {
		cover = ""
	}
	return card{Source: "discogs", Type: "music", ID: id, Title: title, Year: yearOf(entry["year"]), Poster: cover, Overview: artist, Status: "none"}, true
}

func tmdbCards(body obj, filter func(entry obj) string) []card {
	out := []card{}
	for _, entry := range asList(body["results"]) {
		mediaType := filter(entry)
		if mediaType == "" {
			continue
		}
		if c, ok := tmdbCard(entry, mediaType); ok {
			out = append(out, c)
		}
	}
	return out
}

func discogsCards(body obj) []card {
	out := []card{}
	for _, entry := range asList(body["results"]) {
		if c, ok := discogsCard(entry); ok {
			out = append(out, c)
		}
	}
	return out
}

// searchCalls builds the provider calls for a search; kinds says which providers each call belongs to.
func searchCalls(s settings, term, mediaType string) (calls []hostCall, kinds []string) {
	wanted := func(kind string) bool { return (mediaType == "" || mediaType == kind) && s.typeEnabled(kind) }
	if s.TMDBKey != "" && (wanted("movie") || wanted("tv")) {
		calls = append(calls, s.tmdbCall("/search/multi", map[string]string{"query": term, "include_adult": "false"}))
		kinds = append(kinds, "tmdb")
	}
	if s.DiscogsToken != "" && wanted("music") {
		calls = append(calls, s.discogsCall("/database/search", map[string]string{"q": term, "type": "master", "per_page": fmt.Sprint(maxSearch)}))
		kinds = append(kinds, "discogs")
	}
	return calls, kinds
}

type cardsOut struct {
	Cards []card `json:"cards"`
	Error string `json:"error"`
}

func parseSearch(s settings, mediaType string, kinds []string, results []hostResult) cardsOut {
	wanted := func(kind string) bool { return (mediaType == "" || mediaType == kind) && s.typeEnabled(kind) }
	out := cardsOut{Cards: []card{}}
	failures := 0
	firstError := ""
	for i, kind := range kinds {
		body, problem := decodeProvider(results[i])
		if problem != "" {
			failures++
			if firstError == "" {
				firstError = problem
			}
			continue
		}
		if kind == "tmdb" {
			cards := tmdbCards(body, func(entry obj) string {
				mt := text(entry["media_type"])
				if (mt == "movie" || mt == "tv") && wanted(mt) {
					return mt
				}
				return ""
			})
			if len(cards) > maxSearch {
				cards = cards[:maxSearch]
			}
			out.Cards = append(out.Cards, cards...)
		} else {
			out.Cards = append(out.Cards, discogsCards(body)...)
		}
	}
	if len(kinds) > 0 && failures == len(kinds) {
		out.Error = firstError
	}
	return out
}

// discoverCall builds the provider call for a discover section, or false when its provider is not configured.
func discoverCall(s settings, section string, year int) (hostCall, bool) {
	switch section {
	case "trending":
		return s.tmdbCall("/trending/all/week", nil), s.TMDBKey != "" && (s.Movies || s.TV)
	case "movies":
		return s.tmdbCall("/movie/popular", nil), s.TMDBKey != "" && s.Movies
	case "tv":
		return s.tmdbCall("/tv/popular", nil), s.TMDBKey != "" && s.TV
	case "upcoming":
		return s.tmdbCall("/movie/upcoming", nil), s.TMDBKey != "" && s.Movies
	case "music":
		return s.discogsCall("/database/search", map[string]string{
			"type": "master", "sort": "have", "sort_order": "desc", "year": fmt.Sprint(year - 1), "per_page": fmt.Sprint(maxSearch),
		}), s.DiscogsToken != "" && s.Music
	}
	return nil, false
}

func parseDiscover(s settings, section string, result hostResult) []card {
	body, problem := decodeProvider(result)
	if problem != "" {
		return []card{}
	}
	var cards []card
	switch section {
	case "trending":
		cards = tmdbCards(body, func(entry obj) string {
			mt := text(entry["media_type"])
			if (mt == "movie" || mt == "tv") && s.typeEnabled(mt) {
				return mt
			}
			return ""
		})
	case "movies", "upcoming":
		cards = tmdbCards(body, func(obj) string { return "movie" })
	case "tv":
		cards = tmdbCards(body, func(obj) string { return "tv" })
	case "music":
		cards = discogsCards(body)
	}
	if cards == nil {
		cards = []card{}
	}
	return cards
}

// detailsCall builds the provider call for a title's details.
func detailsCall(s settings, mediaType, id string) (hostCall, error) {
	if !numericID.MatchString(id) {
		return nil, fail("Invalid id.")
	}
	if mediaType == "music" {
		if s.DiscogsToken == "" {
			return nil, fail("%s", noDiscogs)
		}
		return s.discogsCall("/masters/"+id, nil), nil
	}
	if s.TMDBKey == "" {
		return nil, fail("%s", noTMDBText)
	}
	return s.tmdbCall("/"+mediaType+"/"+id, map[string]string{"append_to_response": "credits"}), nil
}

type detailOut struct {
	Detail *detail `json:"detail"`
	Error  string  `json:"error"`
}

func parseDetails(mediaType, id string, result hostResult) detailOut {
	body, problem := decodeProvider(result)
	if problem != "" {
		return detailOut{Error: problem}
	}
	if mediaType == "music" {
		return parseAlbum(id, body)
	}
	c, ok := tmdbCard(body, mediaType)
	if !ok {
		return detailOut{Error: "Not found at TMDB."}
	}
	d := detail{card: c, Backdrop: tmdbImage(body["backdrop_path"], "w1280"), Tagline: text(body["tagline"]), Genres: []string{}, Cast: []string{}}
	for _, genre := range asList(body["genres"]) {
		if name := text(genre["name"]); name != "" {
			d.Genres = append(d.Genres, name)
		}
	}
	if runtime, ok := number(body["runtime"]); ok {
		d.Runtime = int(runtime)
	} else if runtimes, _ := body["episode_run_time"].([]any); len(runtimes) > 0 {
		if first, ok := number(runtimes[0]); ok {
			d.Runtime = int(first)
		}
	}
	if d.Runtime < 0 {
		d.Runtime = 0
	}
	if rating, ok := number(body["vote_average"]); ok && rating > 0 {
		d.Rating = math.Round(rating*10) / 10
	}
	for _, person := range asList(asObj(body["credits"])["cast"]) {
		if name := text(person["name"]); name != "" && len(d.Cast) < 12 {
			d.Cast = append(d.Cast, name)
		}
	}
	if mediaType == "tv" {
		d.Seasons = []seasonInfo{}
		for _, season := range asList(body["seasons"]) {
			n, ok := number(season["season_number"])
			if !ok || n <= 0 {
				continue
			}
			name := text(season["name"])
			if name == "" {
				name = fmt.Sprintf("Season %.0f", n)
			}
			episodes, _ := number(season["episode_count"])
			d.Seasons = append(d.Seasons, seasonInfo{Number: int(n), Name: name, EpisodeCount: int(episodes), Status: "none"})
		}
	}
	return detailOut{Detail: &d}
}

func parseAlbum(id string, body obj) detailOut {
	label := text(body["title"])
	if label == "" {
		return detailOut{Error: "Not found at Discogs."}
	}
	artists := []string{}
	for _, artist := range asList(body["artists"]) {
		if name := strings.TrimSpace(discogsSuffix.ReplaceAllString(text(artist["name"]), "")); name != "" {
			artists = append(artists, name)
		}
	}
	images := asList(body["images"])
	cover := ""
	if len(images) > 0 {
		cover = text(images[0]["uri"])
		if cover == "" {
			cover = text(images[0]["resource_url"])
		}
	}
	d := detail{
		card:   card{Source: "discogs", Type: "music", ID: id, Title: label, Year: yearOf(body["year"]), Poster: cover, Overview: text(body["notes"]), Status: "none"},
		Genres: []string{}, Cast: []string{}, Artist: strings.Join(artists, ", "), Tracklist: []string{},
	}
	for _, key := range []string{"genres", "styles"} {
		list, _ := body[key].([]any)
		for _, entry := range list {
			if name, ok := entry.(string); ok {
				d.Genres = append(d.Genres, name)
			}
		}
	}
	for _, track := range asList(body["tracklist"]) {
		if title := text(track["title"]); title != "" {
			d.Tracklist = append(d.Tracklist, title)
		}
	}
	return detailOut{Detail: &d}
}

// tvdbCall builds the call that finds the TVDB id Sonarr needs for a TMDB show.
func tvdbCall(s settings, tmdbID string) hostCall {
	return s.tmdbCall("/tv/"+tmdbID+"/external_ids", nil)
}

func parseTVDB(result hostResult) int {
	body, problem := decodeProvider(result)
	if problem != "" {
		return 0
	}
	if id, ok := number(body["tvdb_id"]); ok && id > 0 {
		return int(id)
	}
	return 0
}
