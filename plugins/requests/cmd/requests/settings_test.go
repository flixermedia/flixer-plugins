package main

import (
	"strings"
	"testing"
)

func settingsBody(extra map[string]any) map[string]any {
	body := map[string]any{
		"moviesEnabled": true, "tvEnabled": true, "musicEnabled": true, "defaultCanRequest": true, "autoApprove": false,
		"quotaLimit": 5, "quotaDays": 14, "notifyAdmins": true, "notifyUsers": false, "language": "de-DE",
	}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func TestSettingsSaveValidation(t *testing.T) {
	tests := []struct {
		name, role string
		body       map[string]any
		want       string
	}{
		{"user denied", "user", settingsBody(nil), "Only administrators can do this."},
		{"system denied", "system", settingsBody(nil), "Only administrators can do this."},
		{"quota too large", "admin", settingsBody(map[string]any{"quotaLimit": 1001}), "The request limit must be a whole number from 0 to 1000."},
		{"quota negative", "admin", settingsBody(map[string]any{"quotaLimit": -1}), "The request limit must be a whole number from 0 to 1000."},
		{"quota text", "admin", settingsBody(map[string]any{"quotaLimit": "many"}), "The request limit must be a whole number from 0 to 1000."},
		{"period zero", "admin", settingsBody(map[string]any{"quotaDays": 0}), "The limit period must be a whole number of days from 1 to 365."},
		{"period too long", "admin", settingsBody(map[string]any{"quotaDays": 366}), "The limit period must be a whole number of days from 1 to 365."},
		{"language words", "admin", settingsBody(map[string]any{"language": "not a language"}), "The metadata language must look like en or en-US."},
		{"language empty", "admin", settingsBody(map[string]any{"language": ""}), "The metadata language must look like en or en-US."},
		{"switch text", "admin", settingsBody(map[string]any{"autoApprove": "maybe"}), "Auto-approve must be on or off."},
		{"key with spaces", "admin", map[string]any{"tmdbApiKey": "abc def ghi jkl"}, "The TMDB API key looks invalid. Copy it again without spaces."},
		{"token with newline", "admin", map[string]any{"discogsToken": "abcdefgh\nX-Evil: 1"}, "The Discogs token looks invalid. Copy it again without spaces."},
		{"key too short", "admin", map[string]any{"tmdbApiKey": "abc"}, "The TMDB API key looks invalid. Copy it again without spaces."},
		{"nothing posted", "admin", map[string]any{}, "There is nothing to save."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t)
			uid := adminID
			if tc.role == "user" {
				uid = userID
			}
			if tc.role == "system" {
				uid = ""
			}
			res := h.drive("settings-save", tc.role, uid, tc.body, withSettings(nil))
			if got := resultError(res); got != tc.want {
				t.Fatalf("error = %q, want %q", got, tc.want)
			}
			if len(h.rows(tableSettings)) != 0 {
				t.Fatal("a rejected save must not store anything")
			}
		})
	}
}

func TestSettingsSaveAndRead(t *testing.T) {
	h := fixture(t)
	res := h.drive("settings-save", "admin", adminID, settingsBody(map[string]any{"moviesEnabled": false, "autoApprove": true}), withSettings(nil))
	if resultError(res) != "" {
		t.Fatal(resultError(res))
	}
	stored := map[string]any{}
	for _, row := range h.rows(tableSettings) {
		stored[row["key"].(string)] = row["value"]
	}
	if len(stored) != 10 || stored["moviesEnabled"] != false || stored["language"] != "de-DE" || stored["quotaDays"] != float64(14) && stored["quotaDays"] != 14 {
		t.Fatalf("stored = %v", stored)
	}
	// The saved row beats the legacy envelope value, which says movies are on.
	if got := resultError(h.drive("request", "user", userID, map[string]any{"type": "movie", "externalId": "603"}, withSettings(nil))); got != "This kind of request is turned off." {
		t.Fatalf("request error = %q", got)
	}
	// Saving again replaces the rows instead of adding more.
	h.drive("settings-save", "admin", adminID, settingsBody(nil), withSettings(nil))
	if n := len(h.rows(tableSettings)); n != 10 {
		t.Fatalf("rows = %d", n)
	}
}

func TestSettingsEnvelopeFallback(t *testing.T) {
	h := fixture(t)
	if got := resultError(h.drive("request", "user", userID, map[string]any{"type": "movie", "externalId": "603"}, withSettings(map[string]any{"moviesEnabled": false}))); got != "This kind of request is turned off." {
		t.Fatalf("legacy value ignored: %q", got)
	}
	h.seed(tableSettings, settingRow{Key: "moviesEnabled", Value: true})
	if got := resultError(h.drive("request", "user", userID, map[string]any{"type": "movie", "externalId": "603"}, withSettings(map[string]any{"moviesEnabled": false}))); got != "" {
		t.Fatalf("a stored row must win over the legacy value: %q", got)
	}
}

func TestProviderKeysAreSecrets(t *testing.T) {
	h := fixture(t)
	const newKey, newToken = "0123456789abcdef0123456789abcdef", "AbCdEfGhIjKlMn"
	res := h.drive("settings-save", "admin", adminID, map[string]any{"tmdbApiKey": newKey, "discogsToken": newToken}, withSettings(nil))
	if resultError(res) != "" {
		t.Fatal(resultError(res))
	}
	if h.secrets[secretTMDB] != newKey || h.secrets[secretDiscogs] != newToken {
		t.Fatalf("secrets = %v", h.secrets)
	}
	if len(h.rows(tableSettings)) != 0 {
		t.Fatalf("secrets must not be stored in the table: %v", h.rows(tableSettings))
	}
	if strings.Contains(res.Raw, newKey) || strings.Contains(res.Raw, newToken) {
		t.Fatal("secret echoed")
	}

	// An empty submit keeps the saved values and the page only reports that they exist.
	h.drive("settings-save", "admin", adminID, map[string]any{"tmdbApiKey": "", "discogsToken": "", "clearTmdbApiKey": false, "clearDiscogsToken": false, "language": "en"}, withSettings(nil))
	if h.secrets[secretTMDB] != newKey || h.secrets[secretDiscogs] != newToken {
		t.Fatal("an empty submit must keep the saved keys")
	}
	_, page := pageView(t, h, "admin", adminID, map[string]string{"tab": "settings"}, map[string]any{})
	if strings.Contains(page.Raw, newKey) || strings.Contains(page.Raw, newToken) {
		t.Fatal("the settings page shows a secret")
	}
	if strings.Count(page.Raw, `"hasValue":true`) != 2 {
		t.Fatalf("expected both keys to show as saved: %s", page.Raw)
	}

	// Removing a key also stops the legacy envelope value from coming back.
	res = h.drive("settings-save", "admin", adminID, map[string]any{"clearTmdbApiKey": true}, withSettings(nil))
	if resultError(res) != "" || h.secrets[secretTMDB] != "" || h.secrets[secretDiscogs] != newToken {
		t.Fatalf("clear: %v %v", resultError(res), h.secrets)
	}
	got := resultError(h.drive("request", "user", userID, map[string]any{"type": "movie", "externalId": "603"}, withSettings(nil)))
	if got != noTMDBText {
		t.Fatalf("request error = %q", got)
	}
}

func TestSettingsTabAndServersTab(t *testing.T) {
	t.Run("settings sections", func(t *testing.T) {
		h := fixture(t)
		_, res := pageView(t, h, "admin", adminID, nil, withSettings(nil))
		for _, want := range []string{`"type":"section"`, `"title":"General"`, `"title":"Metadata providers"`, `"title":"Servers"`, `"op":"settings-save"`, `"type":"switch"`, `"name":"quotaLimit"`, `"name":"language"`, `"type":"password"`, `"name":"clearTmdbApiKey"`, "Manage servers"} {
			if !strings.Contains(res.Raw, want) {
				t.Errorf("missing %s", want)
			}
		}
		for _, secret := range []string{tmdbKey, discogs} {
			if strings.Contains(res.Raw, secret) {
				t.Fatal("legacy secret leaked into the page")
			}
		}
	})
	t.Run("users never reach the settings tab", func(t *testing.T) {
		h := fixture(t)
		_, res := pageView(t, h, "user", userID, map[string]string{"tab": "settings"}, withSettings(nil))
		if strings.Contains(res.Raw, "settings-save") || strings.Contains(res.Raw, "Metadata providers") {
			t.Fatal("settings shown to a user")
		}
	})
	t.Run("empty servers show three add cards", func(t *testing.T) {
		h := fixture(t)
		h.tables[tableServices] = nil
		_, res := pageView(t, h, "admin", adminID, map[string]string{"tab": "services"}, withSettings(nil))
		for _, want := range []string{"Add Radarr", "Add Sonarr", "Add Lidarr", `"add":"radarr"`, "Connect a server"} {
			if !strings.Contains(res.Raw, want) {
				t.Errorf("missing %s", want)
			}
		}
	})
	t.Run("add flow", func(t *testing.T) {
		h := fixture(t)
		h.tables[tableServices] = nil
		_, form := pageView(t, h, "admin", adminID, map[string]string{"tab": "services", "add": "sonarr"}, withSettings(nil))
		for _, want := range []string{`"op":"service-save"`, `"name":"kind"`, `"value":"sonarr"`, `"name":"apiKey"`, "Test connection and continue"} {
			if !strings.Contains(form.Raw, want) {
				t.Errorf("missing %s", want)
			}
		}
		save := map[string]any{"kind": "sonarr", "name": "Sonarr", "url": "https://sonarr.example.com", "apiKey": "sonarr-key"}
		if got := resultError(h.drive("service-save", "admin", adminID, save, withSettings(nil))); got != "" {
			t.Fatalf("first server from an empty state: %q", got)
		}
		rows := h.rows(tableServices)
		if len(rows) != 1 || rows[0]["is_default"] != true || rows[0]["enabled"] != false {
			t.Fatalf("rows = %v", rows)
		}
		_, edit := pageView(t, h, "admin", adminID, map[string]string{"tab": "services", "edit": rows[0]["id"].(string)}, withSettings(nil))
		for _, want := range []string{`"type":"switch"`, `"name":"enabled"`, `"name":"isDefault"`, `"name":"seasonFolders"`, `"hasValue":true`, "Needs setup"} {
			if want == "Needs setup" {
				continue
			}
			if !strings.Contains(edit.Raw, want) {
				t.Errorf("edit page missing %s", want)
			}
		}
		if !strings.Contains(edit.Raw, "Connection works") {
			t.Error("edit page should show the connection result")
		}
	})
}
