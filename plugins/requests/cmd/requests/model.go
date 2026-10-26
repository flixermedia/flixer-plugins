package main

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode"
)

const (
	tableRequests = "requests"
	tableServices = "services"
	tableLimits   = "user_limits"

	maxQuotaLimit = 1000
	maxQuotaDays  = 365

	pluginPagePath = "/plugins/flixer.requests/requests"
)

var (
	mediaTypes   = []string{"movie", "tv", "music"}
	allStatuses  = []string{"pending", "approved", "declined", "failed", "available"}
	serviceKinds = []string{"sonarr", "radarr", "lidarr"}
	kindOfType   = map[string]string{"movie": "radarr", "tv": "sonarr", "music": "lidarr"}
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	numericID    = regexp.MustCompile(`^\d{1,12}$`)
	minAvailable = []string{"announced", "inCinemas", "released"}
)

func contains(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

func containsInt(list []int, value int) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// settings are the plugin settings from the envelope, with defaults applied.
type settings struct {
	TMDBKey           string
	DiscogsToken      string
	Language          string
	Movies            bool
	TV                bool
	Music             bool
	DefaultCanRequest bool
	AutoApprove       bool
	QuotaLimit        int
	QuotaDays         int
	NotifyAdmins      bool
	NotifyUsers       bool
}

func parseSettings(raw map[string]any) settings {
	flag := func(key string, fallback bool) bool {
		if value, ok := raw[key].(bool); ok {
			return value
		}
		return fallback
	}
	count := func(key string, fallback, min, max int) int {
		value, ok := raw[key].(float64)
		if !ok || value != math.Trunc(value) || value < float64(min) || value > float64(max) {
			return fallback
		}
		return int(value)
	}
	text := func(key string) string {
		value, _ := raw[key].(string)
		return strings.TrimSpace(value)
	}
	language := text("language")
	if !regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})?$`).MatchString(language) {
		language = "en"
	}
	return settings{
		TMDBKey:           text("tmdbApiKey"),
		DiscogsToken:      text("discogsToken"),
		Language:          language,
		Movies:            flag("moviesEnabled", true),
		TV:                flag("tvEnabled", true),
		Music:             flag("musicEnabled", true),
		DefaultCanRequest: flag("defaultCanRequest", true),
		AutoApprove:       flag("autoApprove", false),
		QuotaLimit:        count("quotaLimit", 0, 0, maxQuotaLimit),
		QuotaDays:         count("quotaDays", 7, 1, maxQuotaDays),
		NotifyAdmins:      flag("notifyAdmins", true),
		NotifyUsers:       flag("notifyUsers", true),
	}
}

func (s settings) typeEnabled(mediaType string) bool {
	switch mediaType {
	case "movie":
		return s.Movies
	case "tv":
		return s.TV
	case "music":
		return s.Music
	}
	return false
}

// Table rows. JSON names are the declared column names.

type requestRow struct {
	ID               string  `json:"id"`
	UserID           string  `json:"user_id"`
	Type             string  `json:"type"`
	Source           string  `json:"source"`
	ExternalID       string  `json:"external_id"`
	Title            string  `json:"title"`
	Year             *int    `json:"year"`
	Poster           *string `json:"poster"`
	Artist           *string `json:"artist"`
	Seasons          []int   `json:"seasons"`
	Status           string  `json:"status"`
	ServiceID        *string `json:"service_id"`
	QualityProfileID *int    `json:"quality_profile_id"`
	RootFolder       *string `json:"root_folder"`
	DeclineReason    *string `json:"decline_reason"`
	Error            *string `json:"error"`
	ProcessedBy      *string `json:"processed_by"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type serviceRow struct {
	ID                  string  `json:"id"`
	Kind                string  `json:"kind"`
	Name                string  `json:"name"`
	URL                 string  `json:"url"`
	IsDefault           bool    `json:"is_default"`
	Enabled             bool    `json:"enabled"`
	RootFolder          string  `json:"root_folder"`
	QualityProfileID    int     `json:"quality_profile_id"`
	MetadataProfileID   *int    `json:"metadata_profile_id"`
	LanguageProfileID   *int    `json:"language_profile_id"`
	MinimumAvailability *string `json:"minimum_availability"`
	SeasonFolders       bool    `json:"season_folders"`
	Tags                []int   `json:"tags"`
	CreatedAt           string  `json:"created_at"`
}

type limitRow struct {
	UserID      string `json:"user_id"`
	CanRequest  *bool  `json:"can_request"`
	AutoApprove *bool  `json:"auto_approve"`
	QuotaLimit  *int   `json:"quota_limit"`
	QuotaDays   *int   `json:"quota_days"`
}

// complete reports whether the server has been fully set up (a freshly connected draft has no root folder).
func (s serviceRow) complete() bool { return s.RootFolder != "" && s.QualityProfileID > 0 }

func (s serviceRow) usable() bool { return s.Enabled && s.complete() }

func secretName(serviceID string) string { return "arr." + serviceID }

func str(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// usableKinds lists the server kinds that can take requests.
func usableKinds(services []serviceRow) map[string]bool {
	kinds := map[string]bool{}
	for _, service := range services {
		if service.usable() {
			kinds[service.Kind] = true
		}
	}
	return kinds
}

// effective returns the settings with each media type switched off when no usable server can fulfil it.
func (s settings) effective(services []serviceRow) settings {
	kinds := usableKinds(services)
	s.Movies = s.Movies && kinds["radarr"]
	s.TV = s.TV && kinds["sonarr"]
	s.Music = s.Music && kinds["lidarr"]
	return s
}

// resolveService picks the server a request goes to: the chosen one, otherwise the default (or oldest) usable server of that kind.
func resolveService(services []serviceRow, kind, chosen string) (*serviceRow, error) {
	if chosen != "" {
		for i := range services {
			if services[i].ID == chosen {
				if services[i].Kind != kind || !services[i].Enabled {
					return nil, fail("The chosen server is not an enabled %s server.", kind)
				}
				if !services[i].complete() {
					return nil, fail("The chosen server has not been fully set up.")
				}
				return &services[i], nil
			}
		}
		return nil, fail("The chosen server is not an enabled %s server.", kind)
	}
	var best *serviceRow
	for i := range services {
		candidate := &services[i]
		if candidate.Kind != kind || !candidate.usable() {
			continue
		}
		if best == nil || (candidate.IsDefault && !best.IsDefault) ||
			(candidate.IsDefault == best.IsDefault && candidate.CreatedAt < best.CreatedAt) {
			best = candidate
		}
	}
	return best, nil
}

type access struct {
	CanRequest  bool
	AutoApprove bool
	QuotaLimit  int // 0 is unlimited
	QuotaDays   int
}

func resolveAccess(admin bool, s settings, limit *limitRow) access {
	if admin {
		return access{CanRequest: true, AutoApprove: true, QuotaDays: s.QuotaDays}
	}
	quota := s.QuotaLimit
	days := s.QuotaDays
	allow := s.DefaultCanRequest
	auto := s.AutoApprove
	if limit != nil {
		if limit.QuotaLimit != nil {
			quota = *limit.QuotaLimit
		}
		if limit.QuotaDays != nil {
			days = *limit.QuotaDays
		}
		if limit.CanRequest != nil {
			allow = *limit.CanRequest
		}
		if limit.AutoApprove != nil {
			auto = *limit.AutoApprove
		}
	}
	if quota < 0 {
		quota = 0
	}
	return access{CanRequest: allow, AutoApprove: auto, QuotaLimit: quota, QuotaDays: days}
}

// remainingSeasons are the seasons left to request once the already requested ones are removed.
func remainingSeasons(requested []int, taken [][]int, all []int) []int {
	covered := map[int]bool{}
	for _, seasons := range taken {
		list := seasons
		if seasons == nil {
			list = all
		}
		for _, season := range list {
			covered[season] = true
		}
	}
	source := requested
	if requested == nil {
		source = all
	}
	out := []int{}
	for _, season := range source {
		if !covered[season] {
			out = append(out, season)
		}
	}
	return out
}

// normalizeTitle keeps lowercase ASCII letters and digits so titles compare equal across punctuation and spacing.
func normalizeTitle(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(foldAccents(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

var accentMap = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ç': "c", 'è': "e", 'é': "e", 'ê': "e", 'ë': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ñ': "n", 'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ý': "y", 'ÿ': "y", 'ß': "ss", 'æ': "ae", 'œ': "oe",
}

func foldAccents(value string) string {
	var out strings.Builder
	for _, r := range value {
		lower := unicode.ToLower(r)
		if mapped, ok := accentMap[lower]; ok {
			out.WriteString(mapped)
		} else {
			out.WriteRune(lower)
		}
	}
	return out.String()
}

var discogsSuffix = regexp.MustCompile(`\s*\(\d+\)$`)

// splitDiscogsTitle splits "Artist - Album"; some artists carry a "(2)" disambiguation suffix.
func splitDiscogsTitle(value string) (artist, title string) {
	index := strings.Index(value, " - ")
	if index < 0 {
		return "", strings.TrimSpace(value)
	}
	artist = strings.TrimSpace(discogsSuffix.ReplaceAllString(value[:index], ""))
	return artist, strings.TrimSpace(value[index+3:])
}

// yearOf reads the year at the start of a date such as 2010-07-16.
func yearOf(value any) int {
	switch v := value.(type) {
	case float64:
		if v == math.Trunc(v) && v > 0 {
			return int(v)
		}
	case string:
		if len(v) >= 4 {
			year := 0
			for _, r := range v[:4] {
				if r < '0' || r > '9' {
					return 0
				}
				year = year*10 + int(r-'0')
			}
			return year
		}
	}
	return 0
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
