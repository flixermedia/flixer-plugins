package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const maxBodyBytes = 64 << 10

// normalizeArrURL trims whitespace and trailing slashes and requires a public https URL on the default port.
func normalizeArrURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	problem := fail("Enter the server address as a public https:// URL (port 443).")
	if trimmed == "" || len(trimmed) > 500 {
		return "", problem
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(trimmed, "?") || strings.Contains(trimmed, "#") {
		return "", problem
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", problem
	}
	return trimmed, nil
}

type arrConn struct {
	Kind string
	URL  string
	Key  string
}

func (a arrConn) base() string {
	version := "v3"
	if a.Kind == "lidarr" {
		version = "v1"
	}
	return strings.TrimRight(a.URL, "/") + "/api/" + version
}

func (a arrConn) call(method, path string, body any) hostCall {
	headers := map[string]string{"x-api-key": a.Key, "accept": "application/json"}
	payload := ""
	if body != nil {
		headers["content-type"] = "application/json"
		payload = mustJSON(body)
	}
	return httpRequest(method, a.base()+path, headers, payload)
}

// decodeArr returns the parsed JSON of an *arr answer or a readable problem.
func decodeArr(kind string, r hostResult) (any, string) {
	if r.Status == 401 || r.Status == 403 {
		return nil, kind + " rejected the API key."
	}
	if r.Status == 0 && !r.OK {
		reason := r.Error
		if reason == "" {
			reason = "unknown error"
		}
		return nil, fmt.Sprintf("Could not reach %s: %s", kind, reason)
	}
	var parsed any
	if strings.TrimSpace(r.Body) != "" {
		if err := json.Unmarshal([]byte(r.Body), &parsed); err != nil {
			parsed = nil
		}
	}
	if !r.OK {
		detail := ""
		if list, ok := parsed.([]any); ok {
			for _, entry := range list {
				if message := text(asObj(entry)["errorMessage"]); message != "" {
					detail = message
					break
				}
			}
		}
		if detail == "" {
			detail = text(asObj(parsed)["message"])
		}
		if detail != "" {
			return nil, fmt.Sprintf("%s answered: %s", kind, detail)
		}
		return nil, fmt.Sprintf("%s answered with HTTP %d.", kind, r.Status)
	}
	return parsed, ""
}

type profile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type rootFolder struct {
	Path      string  `json:"path"`
	FreeSpace float64 `json:"freeSpace"`
}

type testResult struct {
	OK       bool         `json:"ok"`
	Version  string       `json:"version"`
	Error    string       `json:"error"`
	Quality  []profile    `json:"qualityProfiles"`
	Roots    []rootFolder `json:"rootFolders"`
	Metadata []profile    `json:"metadataProfiles"`
	Language []profile    `json:"languageProfiles"`
}

func profilesOf(value any) []profile {
	out := []profile{}
	for _, entry := range asList(value) {
		id, ok := number(entry["id"])
		name, isString := entry["name"].(string)
		if ok && isString {
			out = append(out, profile{ID: int(id), Name: name})
		}
	}
	return out
}

// testCalls asks the server for its status, quality profiles, root folders and the kind-specific profiles.
func (a arrConn) testCalls() []hostCall {
	calls := []hostCall{
		a.call("GET", "/system/status", nil),
		a.call("GET", "/qualityprofile", nil),
		a.call("GET", "/rootfolder", nil),
	}
	switch a.Kind {
	case "lidarr":
		calls = append(calls, a.call("GET", "/metadataprofile", nil))
	case "sonarr":
		calls = append(calls, a.call("GET", "/languageprofile", nil))
	}
	return calls
}

func (a arrConn) parseTest(rs []hostResult) testResult {
	out := testResult{Quality: []profile{}, Roots: []rootFolder{}, Metadata: []profile{}, Language: []profile{}}
	if len(rs) < 3 {
		out.Error = "Connection failed."
		return out
	}
	status, problem := decodeArr(a.Kind, rs[0])
	if problem != "" {
		out.Error = problem
		return out
	}
	out.Version = text(asObj(status)["version"])
	quality, problem := decodeArr(a.Kind, rs[1])
	if problem != "" {
		out.Error = problem
		return out
	}
	out.Quality = profilesOf(quality)
	folders, problem := decodeArr(a.Kind, rs[2])
	if problem != "" {
		out.Error = problem
		return out
	}
	for _, entry := range asList(folders) {
		if path, ok := entry["path"].(string); ok {
			space, _ := number(entry["freeSpace"])
			out.Roots = append(out.Roots, rootFolder{Path: path, FreeSpace: space})
		}
	}
	if len(rs) > 3 {
		extra, problem := decodeArr(a.Kind, rs[3])
		switch {
		case a.Kind == "lidarr" && problem != "":
			out.Error = problem
			return out
		case a.Kind == "lidarr":
			out.Metadata = profilesOf(extra)
		case problem == "": // Sonarr v4 dropped language profiles.
			out.Language = profilesOf(extra)
		}
	}
	out.OK = true
	return out
}

type arrTarget struct {
	RootFolder        string `json:"rootFolder"`
	QualityProfileID  int    `json:"qualityProfileId"`
	MetadataProfileID *int   `json:"metadataProfileId"`
	LanguageProfileID *int   `json:"languageProfileId"`
	MinAvailability   string `json:"minimumAvailability"`
	SeasonFolders     bool   `json:"seasonFolders"`
	Tags              []int  `json:"tags"`
}

func targetOf(s serviceRow, qualityProfileID *int, root string) arrTarget {
	target := arrTarget{
		RootFolder: s.RootFolder, QualityProfileID: s.QualityProfileID, MetadataProfileID: s.MetadataProfileID,
		LanguageProfileID: s.LanguageProfileID, MinAvailability: str(s.MinimumAvailability), SeasonFolders: s.SeasonFolders, Tags: s.Tags,
	}
	if root != "" {
		target.RootFolder = root
	}
	if qualityProfileID != nil && *qualityProfileID > 0 {
		target.QualityProfileID = *qualityProfileID
	}
	if target.Tags == nil {
		target.Tags = []int{}
	}
	return target
}

type sendInfo struct {
	Type       string `json:"type"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Seasons    []int  `json:"seasons"`
	TVDB       int    `json:"tvdb"`
}

// sender sends one request to Sonarr, Radarr or Lidarr in two steps: look the title up, then add or update it.
type sender struct {
	Conn   arrConn
	Info   sendInfo
	Target arrTarget
}

type lookupData struct {
	Err      string `json:"err"`
	Existing obj    `json:"existing"`
	Found    obj    `json:"found"`
}

type outcome struct {
	OK     bool       `json:"ok"`
	Err    string     `json:"err"`
	Follow []hostCall `json:"follow"`
}

func (s sender) preflight() string {
	switch s.Info.Type {
	case "tv":
		if s.Info.TVDB <= 0 {
			return "This show has no TVDB id, which Sonarr needs."
		}
	case "movie":
		if !numericID.MatchString(s.Info.ExternalID) {
			return "Radarr could not find this movie."
		}
	}
	return ""
}

func (s sender) lookupCalls() []hostCall {
	if s.preflight() != "" {
		return nil
	}
	c := s.Conn
	switch s.Info.Type {
	case "movie":
		return []hostCall{
			c.call("GET", "/movie?tmdbId="+s.Info.ExternalID, nil),
			c.call("GET", "/movie/lookup/tmdb?tmdbId="+s.Info.ExternalID, nil),
		}
	case "tv":
		return []hostCall{
			c.call("GET", fmt.Sprintf("/series?tvdbId=%d", s.Info.TVDB), nil),
			c.call("GET", "/series/lookup?term="+url.QueryEscape(fmt.Sprintf("tvdb:%d", s.Info.TVDB)), nil),
		}
	default:
		term := strings.TrimSpace(s.Info.Artist + " " + s.Info.Title)
		return []hostCall{c.call("GET", "/album/lookup?term="+url.QueryEscape(term), nil)}
	}
}

func (s sender) parseLookup(rs []hostResult) lookupData {
	if problem := s.preflight(); problem != "" {
		return lookupData{Err: problem}
	}
	kind := s.Conn.Kind
	parsed := make([]any, len(rs))
	for i, r := range rs {
		value, problem := decodeArr(kind, r)
		if problem != "" {
			return lookupData{Err: problem}
		}
		parsed[i] = value
	}
	switch s.Info.Type {
	case "movie":
		out := lookupData{Found: asObj(parsed[1])}
		if existing := asList(parsed[0]); len(existing) > 0 {
			out.Existing = existing[0]
		}
		return out
	case "tv":
		out := lookupData{}
		if existing := asList(parsed[0]); len(existing) > 0 {
			out.Existing = existing[0]
		}
		if found := asList(parsed[1]); len(found) > 0 {
			out.Found = found[0]
		}
		return out
	default:
		return lookupData{Found: pickAlbum(asList(parsed[0]), s.Info.Title, s.Info.Artist)}
	}
}

// pickAlbum picks the lookup result whose title matches best: exact (ignoring punctuation) first, otherwise the first result.
func pickAlbum(results []obj, title, artist string) obj {
	wanted := normalizeTitle(title)
	wantedArtist := normalizeTitle(artist)
	named := func(entry obj) bool { return normalizeTitle(text(entry["title"])) == wanted }
	sameArtist := func(entry obj) bool {
		return wantedArtist == "" || normalizeTitle(text(asObj(entry["artist"])["artistName"])) == wantedArtist
	}
	for _, entry := range results {
		if named(entry) && sameArtist(entry) {
			return entry
		}
	}
	for _, entry := range results {
		if named(entry) {
			return entry
		}
	}
	if len(results) > 0 {
		return results[0]
	}
	return nil
}

func withKeys(base obj, extra obj) obj {
	out := obj{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

type applyPlan struct {
	Calls  []hostCall
	Err    string
	Follow []hostCall
}

func (s sender) isWanted(season int) bool {
	return s.Info.Seasons == nil || containsInt(s.Info.Seasons, season)
}

// plan decides which writes the lookup calls for. No calls and no error means there is nothing left to do.
func (s sender) plan(ld lookupData) applyPlan {
	plan := s.planRaw(ld)
	for _, call := range plan.Calls {
		if body, _ := call["body"].(string); len(body) > maxBodyBytes {
			return applyPlan{Err: s.Conn.Kind + " needs a larger request than the plugin may send."}
		}
	}
	return plan
}

func (s sender) planRaw(ld lookupData) applyPlan {
	if ld.Err != "" {
		return applyPlan{Err: ld.Err}
	}
	c, t := s.Conn, s.Target
	switch s.Info.Type {
	case "movie":
		if ld.Existing != nil {
			if has, _ := ld.Existing["hasFile"].(bool); !has {
				return applyPlan{Calls: []hostCall{c.call("POST", "/command", obj{"name": "MoviesSearch", "movieIds": []any{ld.Existing["id"]}})}}
			}
			return applyPlan{}
		}
		if text(ld.Found["title"]) == "" {
			return applyPlan{Err: "Radarr could not find this movie."}
		}
		availability := t.MinAvailability
		if availability == "" {
			availability = "released"
		}
		return applyPlan{Calls: []hostCall{c.call("POST", "/movie", withKeys(ld.Found, obj{
			"qualityProfileId": t.QualityProfileID, "rootFolderPath": t.RootFolder, "minimumAvailability": availability,
			"tags": t.Tags, "monitored": true, "addOptions": obj{"searchForMovie": true},
		}))}}
	case "tv":
		if ld.Existing != nil {
			return s.planExistingSeries(ld.Existing)
		}
		if text(ld.Found["title"]) == "" {
			return applyPlan{Err: "Sonarr could not find this series."}
		}
		seasons := []any{}
		for _, season := range asList(ld.Found["seasons"]) {
			n, ok := number(season["seasonNumber"])
			seasons = append(seasons, withKeys(season, obj{"monitored": ok && n > 0 && s.isWanted(int(n))}))
		}
		body := withKeys(ld.Found, obj{
			"qualityProfileId": t.QualityProfileID, "rootFolderPath": t.RootFolder, "seasonFolder": t.SeasonFolders,
			"tags": t.Tags, "monitored": true, "seasons": seasons,
			"addOptions": obj{"ignoreEpisodesWithFiles": true, "ignoreEpisodesWithoutFiles": false, "searchForMissingEpisodes": true},
		})
		if t.LanguageProfileID != nil {
			body["languageProfileId"] = *t.LanguageProfileID
		}
		return applyPlan{Calls: []hostCall{c.call("POST", "/series", body)}}
	default:
		album := ld.Found
		if album == nil || text(album["foreignAlbumId"]) == "" {
			return applyPlan{Err: "Lidarr could not find this album."}
		}
		if id, ok := number(album["id"]); ok && id > 0 {
			return applyPlan{
				Calls:  []hostCall{c.call("PUT", "/album/monitor", obj{"albumIds": []any{album["id"]}, "monitored": true})},
				Follow: []hostCall{c.call("POST", "/command", obj{"name": "AlbumSearch", "albumIds": []any{album["id"]}})},
			}
		}
		artist := withKeys(asObj(album["artist"]), obj{
			"qualityProfileId": t.QualityProfileID, "metadataProfileId": t.MetadataProfileID, "rootFolderPath": t.RootFolder,
			"tags": t.Tags, "monitored": true, "monitorNewItems": "none",
			"addOptions": obj{"monitor": "none", "searchForMissingAlbums": false},
		})
		return applyPlan{Calls: []hostCall{c.call("POST", "/album", withKeys(album, obj{
			"monitored": true, "anyReleaseOk": true, "artist": artist, "addOptions": obj{"searchForNewAlbum": true},
		}))}}
	}
}

// planExistingSeries monitors the requested seasons of a series Sonarr already has and searches for them.
func (s sender) planExistingSeries(series obj) applyPlan {
	updated := []any{}
	searchable := []int{}
	for _, season := range asList(series["seasons"]) {
		n, ok := number(season["seasonNumber"])
		if ok && n > 0 && s.isWanted(int(n)) {
			season = withKeys(season, obj{"monitored": true})
			searchable = append(searchable, int(n))
		}
		updated = append(updated, season)
	}
	put := s.Conn.call("PUT", fmt.Sprintf("/series/%s", idString(series["id"])), withKeys(series, obj{"monitored": true, "seasons": updated}))
	plan := applyPlan{Calls: []hostCall{put}}
	switch {
	case len(searchable) == 0:
	case len(searchable) > 4:
		plan.Follow = []hostCall{s.Conn.call("POST", "/command", obj{"name": "SeriesSearch", "seriesId": series["id"]})}
	default:
		for _, season := range searchable {
			plan.Follow = append(plan.Follow, s.Conn.call("POST", "/command", obj{"name": "SeasonSearch", "seriesId": series["id"], "seasonNumber": season}))
		}
	}
	return plan
}

// parseApply folds the answers of the write calls into an outcome.
func (s sender) parseApply(plan applyPlan, rs []hostResult) outcome {
	if plan.Err != "" {
		return outcome{Err: plan.Err}
	}
	for _, r := range rs {
		if _, problem := decodeArr(s.Conn.Kind, r); problem != "" {
			return outcome{Err: problem}
		}
	}
	return outcome{OK: true, Follow: plan.Follow}
}
