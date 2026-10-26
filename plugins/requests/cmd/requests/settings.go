package main

import (
	"regexp"
	"sort"
	"strings"
)

const (
	tableSettings = "settings"

	secretTMDB    = "tmdb-api-key"
	secretDiscogs = "discogs-token"
)

var (
	languagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
	tokenPattern    = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{8,200}$`)
)

type settingRow struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type settingsR1 struct {
	Values  map[string]any `json:"values"`
	TMDB    string         `json:"tmdb"` // kept in the continuation: later rounds build provider calls with it
	Discogs string         `json:"discogs"`
}

func settingsCalls() []hostCall {
	return []hostCall{dbFind(tableSettings, nil, nil, 100, 0), secretGet(secretTMDB), secretGet(secretDiscogs)}
}

// settingsFrom reduces the three results of settingsCalls that start at offset.
func settingsFrom(rs []hostResult, offset int) settingsR1 {
	out := settingsR1{Values: map[string]any{}}
	for _, r := range rowsOf[settingRow](at(rs, offset)) {
		out.Values[r.Key] = r.Value
	}
	if r := at(rs, offset+1); r.OK {
		out.TMDB = strings.TrimSpace(r.Value)
	}
	if r := at(rs, offset+2); r.OK {
		out.Discogs = strings.TrimSpace(r.Value)
	}
	return out
}

// buildSettings merges the settings table over the values the host passes from the legacy settings schema
// (a row always wins); the metadata keys come from the secret store.
func buildSettings(c *Ctx, r settingsR1) settings {
	merged := map[string]any{}
	for key, value := range c.env.Settings {
		merged[key] = value
	}
	for key, value := range r.Values {
		merged[key] = value
	}
	cfg := parseSettings(merged)
	if r.TMDB != "" {
		cfg.TMDBKey = r.TMDB
	}
	if r.Discogs != "" {
		cfg.DiscogsToken = r.Discogs
	}
	return cfg
}

// loadSettings reads the effective settings in a round of their own.
func loadSettings(c *Ctx) settings {
	return buildSettings(c, Round(c, settingsCalls(), func(rs []hostResult) settingsR1 { return settingsFrom(rs, 0) }))
}

var settingFlags = []struct{ key, label string }{
	{"moviesEnabled", "Movies"}, {"tvEnabled", "Series"}, {"musicEnabled", "Music"},
	{"defaultCanRequest", "Users can request by default"}, {"autoApprove", "Auto-approve"},
	{"notifyAdmins", "Notify admins"}, {"notifyUsers", "Notify users"},
}

func opSettingsSave(c *Ctx) (any, error) {
	if err := requireAdmin(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	// Each form posts its own fields; keys that are not posted stay as they are.
	values := map[string]any{}
	for _, flag := range settingFlags {
		value, err := a.optBool(flag.key)
		if err != nil {
			return nil, fail("%s must be on or off.", flag.label)
		}
		if value != nil {
			values[flag.key] = *value
		}
	}
	if _, given := a["quotaLimit"]; given {
		limit, err := a.optInt("quotaLimit")
		if err != nil || limit == nil || *limit < 0 || *limit > maxQuotaLimit {
			return nil, fail("The request limit must be a whole number from 0 to %d.", maxQuotaLimit)
		}
		values["quotaLimit"] = *limit
	}
	if _, given := a["quotaDays"]; given {
		days, err := a.optInt("quotaDays")
		if err != nil || days == nil || *days < 1 || *days > maxQuotaDays {
			return nil, fail("The limit period must be a whole number of days from 1 to %d.", maxQuotaDays)
		}
		values["quotaDays"] = *days
	}
	if _, given := a["language"]; given {
		language, err := a.str("language", 16)
		if err != nil || !languagePattern.MatchString(language) {
			return nil, fail("The metadata language must look like en or en-US.")
		}
		values["language"] = language
	}

	deleteKeys := make([]string, 0, len(values)+2)
	for key := range values {
		deleteKeys = append(deleteKeys, key)
	}
	var secrets []hostCall
	for _, spec := range []struct{ field, clear, key, secret, label string }{
		{"tmdbApiKey", "clearTmdbApiKey", "tmdbApiKey", secretTMDB, "The TMDB API key"},
		{"discogsToken", "clearDiscogsToken", "discogsToken", secretDiscogs, "The Discogs token"},
	} {
		value, err := a.str(spec.field, 200)
		if err != nil {
			return nil, err
		}
		clear, err := a.optBool(spec.clear)
		if err != nil {
			return nil, err
		}
		switch {
		case clear != nil && *clear:
			secrets = append(secrets, secretSet(spec.secret, ""))
			values[spec.key] = ""
			deleteKeys = append(deleteKeys, spec.key)
		case value != "":
			if !tokenPattern.MatchString(value) {
				return nil, fail("%s looks invalid. Copy it again without spaces.", spec.label)
			}
			secrets = append(secrets, secretSet(spec.secret, value))
			deleteKeys = append(deleteKeys, spec.key)
		}
	}

	if len(deleteKeys) == 0 {
		return nil, fail("There is nothing to save.")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, map[string]any{"key": key, "value": values[key]})
	}
	first := append([]hostCall{dbDelete(tableSettings, []map[string]any{cond("key", "in", deleteKeys)})}, secrets...)
	Round(c, first, func([]hostResult) struct{} { return struct{}{} })
	if len(rows) > 0 {
		Round(c, []hostCall{{"op": "db.insert", "table": tableSettings, "rows": rows}}, func([]hostResult) struct{} { return struct{}{} })
	}
	return successToast("Settings saved."), nil
}
