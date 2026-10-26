package main

import (
	"strings"
	"testing"
)

func TestNormalizeArrURL(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"https://sonarr.example.com", "https://sonarr.example.com", true},
		{"  https://sonarr.example.com/  ", "https://sonarr.example.com", true},
		{"https://example.com/sonarr/", "https://example.com/sonarr", true},
		{"https://example.com:443", "https://example.com:443", true},
		{"http://example.com", "", false},
		{"ftp://example.com", "", false},
		{"https://example.com:8989", "", false},
		{"https://user:pass@example.com", "", false},
		{"https://example.com?x=1", "", false},
		{"https://example.com#x", "", false},
		{"https://", "", false},
		{"", "", false},
		{"example.com", "", false},
		{"https://" + strings.Repeat("a", 600) + ".com", "", false},
	}
	for _, tc := range tests {
		got, err := normalizeArrURL(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("normalizeArrURL(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestServiceSave(t *testing.T) {
	newID = func() string { return "dddddddd-dddd-4ddd-8ddd-dddddddddddd" }
	t.Cleanup(func() { newID = realNewID })
	const key = "brand-new-key"
	create := map[string]any{"kind": "radarr", "name": "4K", "url": "https://radarr.example.com/", "apiKey": key}
	tests := []struct {
		name      string
		role      string
		uid       string
		body      map[string]any
		prepare   func(h *fakeHost)
		wantError string
		check     func(t *testing.T, h *fakeHost, res driven)
	}{
		{
			name: "admin connects and creates a draft", role: "admin", uid: adminID, body: create,
			check: func(t *testing.T, h *fakeHost, res driven) {
				if h.secrets["arr.dddddddd-dddd-4ddd-8ddd-dddddddddddd"] != key {
					t.Fatal("key not stored as a secret")
				}
				rows := h.rows(tableServices)
				draft := rows[len(rows)-1]
				if draft["enabled"] != false || draft["root_folder"] != "" || draft["url"] != "https://radarr.example.com" {
					t.Fatalf("draft = %v", draft)
				}
				for _, v := range draft {
					if v == key {
						t.Fatal("key stored in the table")
					}
				}
				nav := res.Result["navigate"].(map[string]any)["query"].(map[string]any)
				if nav["edit"] != "dddddddd-dddd-4ddd-8ddd-dddddddddddd" {
					t.Fatalf("navigate = %v", nav)
				}
			},
		},
		{name: "http url", role: "admin", uid: adminID, body: map[string]any{"kind": "radarr", "name": "x", "url": "http://radarr.example.com", "apiKey": "k"}, wantError: "Enter the server address as a public https:// URL (port 443)."},
		{name: "custom port", role: "admin", uid: adminID, body: map[string]any{"kind": "radarr", "name": "x", "url": "https://radarr.example.com:7878", "apiKey": "k"}, wantError: "Enter the server address as a public https:// URL (port 443)."},
		{name: "bad kind", role: "admin", uid: adminID, body: map[string]any{"kind": "plex", "name": "x", "url": "https://radarr.example.com", "apiKey": "k"}, wantError: "Unknown server type."},
		{name: "missing name", role: "admin", uid: adminID, body: map[string]any{"kind": "radarr", "name": " ", "url": "https://radarr.example.com", "apiKey": "k"}, wantError: "Enter a name for the server."},
		{name: "missing key", role: "admin", uid: adminID, body: map[string]any{"kind": "radarr", "name": "x", "url": "https://radarr.example.com"}, wantError: "An API key is required."},
		{name: "rejected key", role: "admin", uid: adminID, body: create, wantError: "radarr rejected the API key.", prepare: func(h *fakeHost) {
			h.http = func(string, string, map[string]string, string) (int, string) { return 401, "{}" }
		}},
		{name: "unreachable", role: "admin", uid: adminID, body: create, wantError: "Could not reach radarr: unreachable", prepare: func(h *fakeHost) {
			h.http = func(string, string, map[string]string, string) (int, string) { return 0, "" }
		}},
		{name: "user denied", role: "user", uid: userID, body: create, wantError: "Only administrators can do this."},
		{name: "system denied", role: "system", uid: "", body: create, wantError: "Only administrators can do this."},
		{
			name: "update keeps the stored key", role: "admin", uid: adminID,
			body: map[string]any{
				"id": radarrID, "kind": "radarr", "name": "Renamed", "url": "https://radarr.example.com", "rootFolder": "/media",
				"qualityProfileId": "4", "minimumAvailability": "inCinemas", "enabled": true, "isDefault": true, "tags": "1, 2",
			},
			check: func(t *testing.T, h *fakeHost, res driven) {
				if h.secrets["arr."+radarrID] != radarrKey {
					t.Fatal("key changed")
				}
				row := h.rows(tableServices)[0]
				if row["name"] != "Renamed" || row["minimum_availability"] != "inCinemas" {
					t.Fatalf("row = %v", row)
				}
				if tags := row["tags"].([]any); len(tags) != 2 {
					t.Fatalf("tags = %v", tags)
				}
			},
		},
		{
			name: "update replaces the key", role: "admin", uid: adminID,
			body: map[string]any{"id": radarrID, "kind": "radarr", "name": "Movies", "url": "https://radarr.example.com", "rootFolder": "/media", "qualityProfileId": 4, "apiKey": key},
			check: func(t *testing.T, h *fakeHost, res driven) {
				if h.secrets["arr."+radarrID] != key {
					t.Fatal("key not replaced")
				}
			},
		},
		{name: "update needs a root folder", role: "admin", uid: adminID, body: map[string]any{"id": radarrID, "kind": "radarr", "name": "x", "url": "https://radarr.example.com", "qualityProfileId": 4}, wantError: "Choose a root folder."},
		{name: "update needs a profile", role: "admin", uid: adminID, body: map[string]any{"id": radarrID, "kind": "radarr", "name": "x", "url": "https://radarr.example.com", "rootFolder": "/m"}, wantError: "Choose a quality profile."},
		{name: "lidarr needs metadata", role: "admin", uid: adminID, body: map[string]any{"id": lidarrID, "kind": "lidarr", "name": "x", "url": "https://lidarr.example.com", "rootFolder": "/m", "qualityProfileId": 1}, wantError: "Lidarr needs a metadata profile."},
		{name: "unknown min availability", role: "admin", uid: adminID, body: map[string]any{"id": radarrID, "kind": "radarr", "name": "x", "url": "https://radarr.example.com", "rootFolder": "/m", "qualityProfileId": 1, "minimumAvailability": "never"}, wantError: "Unknown minimum availability."},
		{name: "update of unknown server", role: "admin", uid: adminID, body: map[string]any{"id": otherID, "kind": "radarr", "name": "x", "url": "https://radarr.example.com", "rootFolder": "/m", "qualityProfileId": 1}, wantError: "Server not found."},
		{name: "bad tags", role: "admin", uid: adminID, body: map[string]any{"id": radarrID, "kind": "radarr", "name": "x", "url": "https://radarr.example.com", "rootFolder": "/m", "qualityProfileId": 1, "tags": "a"}, wantError: "tags must be whole numbers separated by commas."},
		{
			name: "making another server default demotes the old one", role: "admin", uid: adminID,
			body: map[string]any{"id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "kind": "radarr", "name": "Second", "url": "https://radarr.example.com", "rootFolder": "/m", "qualityProfileId": 1, "isDefault": true},
			prepare: func(h *fakeHost) {
				h.seed(tableServices, service("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "radarr", "Second"))
				h.rows(tableServices)[3]["is_default"] = false
			},
			check: func(t *testing.T, h *fakeHost, res driven) {
				if h.rows(tableServices)[0]["is_default"] != false || h.rows(tableServices)[3]["is_default"] != true {
					t.Fatalf("defaults = %v %v", h.rows(tableServices)[0]["is_default"], h.rows(tableServices)[3]["is_default"])
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t)
			if tc.prepare != nil {
				tc.prepare(h)
			}
			count := len(h.rows(tableServices))
			res := h.drive("service-save", tc.role, tc.uid, tc.body, withSettings(nil))
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if got := resultError(res); got != tc.wantError {
				t.Fatalf("error = %q, want %q", got, tc.wantError)
			}
			if tc.wantError != "" {
				if len(h.rows(tableServices)) != count {
					t.Fatal("a rejected save must not store anything")
				}
				return
			}
			tc.check(t, h, res)
		})
	}
}

func TestServiceDeleteAndTest(t *testing.T) {
	t.Run("delete removes the server and key", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("service-delete", "admin", adminID, map[string]any{"id": radarrID}, withSettings(nil))
		if resultError(res) != "" || len(h.rows(tableServices)) != 2 || h.secrets["arr."+radarrID] != "" {
			t.Fatalf("result = %v", res.Result)
		}
	})
	t.Run("delete is admin only", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("service-delete", "user", userID, map[string]any{"id": radarrID}, withSettings(nil))
		if resultError(res) == "" || len(h.rows(tableServices)) != 3 {
			t.Fatal("user deleted a server")
		}
	})
	t.Run("test connection", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("service-test", "admin", adminID, map[string]any{"id": radarrID}, withSettings(nil))
		if !strings.Contains(toastText(res), "version 5.0.0") {
			t.Fatalf("result = %v", res.Result)
		}
		if strings.Contains(res.Raw, radarrKey) {
			t.Fatal("key in result")
		}
	})
	t.Run("test connection failure", func(t *testing.T) {
		h := fixture(t)
		h.http = func(string, string, map[string]string, string) (int, string) { return 401, "{}" }
		res := h.drive("service-test", "admin", adminID, map[string]any{"id": radarrID}, withSettings(nil))
		if res.Result["toast"].(map[string]any)["tone"] != "danger" {
			t.Fatalf("result = %v", res.Result)
		}
	})
	t.Run("test with missing key", func(t *testing.T) {
		h := fixture(t)
		delete(h.secrets, "arr."+radarrID)
		res := h.drive("service-test", "admin", adminID, map[string]any{"id": radarrID}, withSettings(nil))
		if !strings.Contains(resultError(res), "API key") {
			t.Fatalf("result = %v", res.Result)
		}
	})
	t.Run("test is admin only", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("service-test", "user", userID, map[string]any{"id": radarrID}, withSettings(nil))
		if resultError(res) != "Only administrators can do this." || len(h.httpLog) != 0 {
			t.Fatal("user tested a server")
		}
	})
}

func TestUserLimitSave(t *testing.T) {
	tests := []struct {
		name      string
		role      string
		body      map[string]any
		seed      bool
		wantError string
		want      map[string]any
		wantRows  int
	}{
		{name: "creates overrides", role: "admin", body: map[string]any{"userId": userID, "canRequest": "deny", "autoApprove": "allow", "quotaLimit": "5", "quotaDays": 30}, want: map[string]any{"can_request": false, "auto_approve": true, "quota_limit": float64(5), "quota_days": float64(30)}, wantRows: 1},
		{name: "updates overrides", role: "admin", seed: true, body: map[string]any{"userId": userID, "quotaLimit": 2}, want: map[string]any{"quota_limit": float64(2), "can_request": nil}, wantRows: 1},
		{name: "all inherit removes the row", role: "admin", seed: true, body: map[string]any{"userId": userID, "canRequest": "inherit"}, wantRows: 0},
		{name: "quota too large", role: "admin", body: map[string]any{"userId": userID, "quotaLimit": 1001}, wantError: "The quota must be from 0 (unlimited) to 1000."},
		{name: "quota negative", role: "admin", body: map[string]any{"userId": userID, "quotaLimit": -1}, wantError: "The quota must be from 0 (unlimited) to 1000."},
		{name: "period too long", role: "admin", body: map[string]any{"userId": userID, "quotaDays": 366}, wantError: "The quota period must be from 1 to 365 days."},
		{name: "period zero", role: "admin", body: map[string]any{"userId": userID, "quotaDays": 0}, wantError: "The quota period must be from 1 to 365 days."},
		{name: "unknown user", role: "admin", body: map[string]any{"userId": "44444444-4444-4444-8444-444444444444", "quotaLimit": 1}, wantError: "User not found."},
		{name: "bad user id", role: "admin", body: map[string]any{"userId": "x"}, wantError: "Invalid userId."},
		{name: "bad boolean", role: "admin", body: map[string]any{"userId": userID, "canRequest": "maybe"}, wantError: "canRequest must be true or false."},
		{name: "user denied", role: "user", body: map[string]any{"userId": userID, "quotaLimit": 1}, wantError: "Only administrators can do this."},
		{name: "system denied", role: "system", body: map[string]any{"userId": userID, "quotaLimit": 1}, wantError: "Only administrators can do this."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t)
			if tc.seed {
				h.seed(tableLimits, limitRow{UserID: userID, CanRequest: ptr(true), QuotaLimit: ptr(9)})
			}
			uid := adminID
			if tc.role != "admin" {
				uid = userID
			}
			res := h.drive("user-limit-save", tc.role, uid, tc.body, withSettings(nil))
			if got := resultError(res); got != tc.wantError {
				t.Fatalf("error = %q, want %q", got, tc.wantError)
			}
			if tc.wantError != "" {
				return
			}
			rows := h.rows(tableLimits)
			if len(rows) != tc.wantRows {
				t.Fatalf("rows = %v", rows)
			}
			for key, want := range tc.want {
				if rows[0][key] != want {
					t.Fatalf("%s = %v, want %v (%v)", key, rows[0][key], want, rows[0])
				}
			}
		})
	}
}
