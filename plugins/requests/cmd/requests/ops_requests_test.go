package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func resultError(r driven) string {
	if r.Result == nil {
		return ""
	}
	msg, _ := r.Result["error"].(string)
	return msg
}

func toastText(r driven) string {
	if toast, ok := r.Result["toast"].(map[string]any); ok {
		return toast["text"].(string)
	}
	return ""
}

func statusOfOnly(t *testing.T, h *fakeHost) string {
	t.Helper()
	rows := h.rows(tableRequests)
	if len(rows) != 1 {
		t.Fatalf("expected one request, got %d", len(rows))
	}
	return rows[0]["status"].(string)
}

func TestRequestCreate(t *testing.T) {
	movie := map[string]any{"type": "movie", "externalId": "603"}
	movieHint := map[string]any{"type": "movie", "externalId": "603", "serviceId": radarrID}
	tests := []struct {
		name       string
		role, uid  string
		body       map[string]any
		settings   map[string]any
		prepare    func(h *fakeHost)
		wantError  string
		wantStatus string
		wantNotify int
		wantPosts  int
	}{
		{name: "user request waits for approval", role: "user", uid: userID, body: movie, wantStatus: "pending", wantNotify: 1},
		{name: "no admin notification when switched off", role: "user", uid: userID, body: movie, settings: map[string]any{"notifyAdmins": false}, wantStatus: "pending"},
		{name: "auto approve sends to radarr", role: "user", uid: userID, body: movieHint, settings: map[string]any{"autoApprove": true}, wantStatus: "approved", wantNotify: 1, wantPosts: 1},
		{name: "auto approve without matching hint stays pending", role: "user", uid: userID, body: movie, settings: map[string]any{"autoApprove": true}, wantStatus: "pending", wantNotify: 1},
		{name: "admin requests are approved", role: "admin", uid: adminID, body: movieHint, wantStatus: "approved", wantNotify: 1, wantPosts: 1},
		{
			name: "radarr failure is recorded", role: "admin", uid: adminID, body: movieHint, wantStatus: "failed", wantNotify: 0,
			prepare: func(h *fakeHost) {
				inner := h.http
				h.http = func(m, u string, hd map[string]string, b string) (int, string) {
					if m == "POST" {
						return 500, `{"message":"boom"}`
					}
					return inner(m, u, hd, b)
				}
			},
		},
		{name: "not allowed by default", role: "user", uid: userID, body: movie, settings: map[string]any{"defaultCanRequest": false}, wantError: "You are not allowed to make requests."},
		{name: "movies turned off", role: "user", uid: userID, body: movie, settings: map[string]any{"moviesEnabled": false}, wantError: "This kind of request is turned off."},
		{
			name: "per-user deny", role: "user", uid: userID, body: movie, wantError: "You are not allowed to make requests.",
			prepare: func(h *fakeHost) { h.seed(tableLimits, limitRow{UserID: userID, CanRequest: ptr(false)}) },
		},
		{
			name: "per-user allow beats global deny", role: "user", uid: userID, body: movie, settings: map[string]any{"defaultCanRequest": false}, wantStatus: "pending", wantNotify: 1,
			prepare: func(h *fakeHost) { h.seed(tableLimits, limitRow{UserID: userID, CanRequest: ptr(true)}) },
		},
		{
			name: "duplicate", role: "user", uid: userID, body: movie, wantError: "This has already been requested.",
			prepare: func(h *fakeHost) {
				h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "movie", ExternalID: "603", Status: "pending", Title: "The Matrix"})
			},
		},
		{
			name: "declined can be requested again", role: "user", uid: userID, body: movie, wantNotify: 1,
			prepare: func(h *fakeHost) {
				h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "movie", ExternalID: "603", Status: "declined", Title: "The Matrix"})
			},
		},
		{
			name: "already in library", role: "user", uid: userID, body: movie, wantError: "This is already in the library.",
			prepare: func(h *fakeHost) { h.library["movie:603"] = "item-1" },
		},
		{
			name: "quota exhausted", role: "user", uid: userID, body: movie, settings: map[string]any{"quotaLimit": 1, "quotaDays": 7},
			wantError: "You have used all 1 requests allowed every 7 days.",
			prepare: func(h *fakeHost) {
				h.seed(tableRequests, requestRow{ID: radarrID, UserID: userID, Type: "movie", ExternalID: "1", Status: "pending", Title: "x", CreatedAt: "2026-03-09T00:00:00.000Z"})
			},
		},
		{
			name: "old requests do not count against the quota", role: "user", uid: userID, body: movie, settings: map[string]any{"quotaLimit": 1, "quotaDays": 7}, wantNotify: 1,
			prepare: func(h *fakeHost) {
				h.seed(tableRequests, requestRow{ID: radarrID, UserID: userID, Type: "movie", ExternalID: "1", Status: "pending", Title: "x", CreatedAt: "2026-01-01T00:00:00.000Z"})
			},
		},
		{
			name: "declined requests do not count against the quota", role: "user", uid: userID, body: movie, settings: map[string]any{"quotaLimit": 1}, wantNotify: 1,
			prepare: func(h *fakeHost) {
				h.seed(tableRequests, requestRow{ID: radarrID, UserID: userID, Type: "movie", ExternalID: "1", Status: "declined", Title: "x", CreatedAt: "2026-03-09T00:00:00.000Z"})
			},
		},
		{
			name: "admins ignore quotas", role: "admin", uid: adminID, body: movieHint, settings: map[string]any{"quotaLimit": 1}, wantStatus: "approved", wantNotify: 1, wantPosts: 1,
			prepare: func(h *fakeHost) {
				h.seed(tableRequests, requestRow{ID: radarrID, UserID: adminID, Type: "movie", ExternalID: "1", Status: "pending", Title: "x", CreatedAt: "2026-03-09T00:00:00.000Z"})
			},
		},
		{name: "bad type", role: "user", uid: userID, body: map[string]any{"type": "game", "externalId": "1"}, wantError: "type must be movie, tv or music."},
		{name: "bad id", role: "user", uid: userID, body: map[string]any{"type": "movie", "externalId": "../x"}, wantError: "Invalid id."},
		{name: "bad service id", role: "user", uid: userID, body: map[string]any{"type": "movie", "externalId": "603", "serviceId": "nope"}, wantError: "Invalid serviceId."},
		{name: "missing tmdb key", role: "user", uid: userID, body: movie, settings: map[string]any{"tmdbApiKey": ""}, wantError: noTMDBText},
		{name: "unknown title", role: "user", uid: userID, body: map[string]any{"type": "movie", "externalId": "999"}, wantError: errNotFound},
		{name: "system cannot request", role: "system", uid: "", body: movie, wantError: "You must be signed in."},
		{name: "anonymous cannot request", role: "", uid: "", body: movie, wantError: "You must be signed in."},
		{name: "music auto approve", role: "admin", uid: adminID, body: map[string]any{"type": "music", "externalId": "5", "serviceId": lidarrID}, wantStatus: "approved", wantNotify: 1, wantPosts: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t)
			if tc.prepare != nil {
				tc.prepare(h)
			}
			before := len(h.rows(tableRequests))
			res := h.drive("request", tc.role, tc.uid, tc.body, withSettings(tc.settings))
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if got := resultError(res); got != tc.wantError {
				t.Fatalf("error = %q, want %q", got, tc.wantError)
			}
			if tc.wantError != "" {
				if len(h.rows(tableRequests)) != before {
					t.Fatal("a rejected request must not be stored")
				}
				return
			}
			rows := h.rows(tableRequests)
			if len(rows) != before+1 {
				t.Fatalf("expected a new request, have %d", len(rows))
			}
			created := rows[len(rows)-1]
			if tc.wantStatus != "" && created["status"] != tc.wantStatus {
				t.Fatalf("status = %v, want %v (%v)", created["status"], tc.wantStatus, created["error"])
			}
			if created["user_id"] != tc.uid {
				t.Fatalf("user_id = %v", created["user_id"])
			}
			if len(h.notified) != tc.wantNotify {
				t.Fatalf("notifications = %d, want %d: %v", len(h.notified), tc.wantNotify, h.notified)
			}
			posts := 0
			for _, line := range h.httpLog {
				if strings.HasPrefix(line, "POST ") {
					posts++
				}
			}
			if tc.wantPosts == 0 && tc.wantStatus != "failed" && posts != 0 {
				t.Fatalf("unexpected POSTs: %v", h.httpLog)
			}
			if tc.wantPosts > 0 && posts != tc.wantPosts {
				t.Fatalf("posts = %d, want %d: %v", posts, tc.wantPosts, h.httpLog)
			}
		})
	}
}

func TestRequestSeries(t *testing.T) {
	tests := []struct {
		name        string
		body        map[string]any
		existing    []int
		wantErr     string
		wantSeasons []int
		all         bool
	}{
		{name: "all seasons", body: map[string]any{"type": "tv", "externalId": "1399"}, all: true},
		{name: "chosen seasons array", body: map[string]any{"type": "tv", "externalId": "1399", "seasons": []any{2, 1}}, wantSeasons: []int{1, 2}},
		{name: "season checkboxes", body: map[string]any{"type": "tv", "externalId": "1399", "season_1": false, "season_2": true, "season_3": true}, wantSeasons: []int{2, 3}},
		{name: "no season chosen", body: map[string]any{"type": "tv", "externalId": "1399", "season_1": false}, wantErr: "Choose at least one valid season."},
		{name: "unknown season", body: map[string]any{"type": "tv", "externalId": "1399", "seasons": []any{9}}, wantErr: "Choose at least one valid season."},
		{name: "specials are not requestable", body: map[string]any{"type": "tv", "externalId": "1399", "seasons": []any{0}}, wantErr: "Invalid season."},
		{name: "already requested seasons are dropped", body: map[string]any{"type": "tv", "externalId": "1399", "seasons": []any{1, 2}}, existing: []int{1}, wantSeasons: []int{2}},
		{name: "everything requested", body: map[string]any{"type": "tv", "externalId": "1399", "seasons": []any{1}}, existing: []int{1}, wantErr: "Every chosen season has already been requested."},
		{name: "bad seasons shape", body: map[string]any{"type": "tv", "externalId": "1399", "seasons": "1"}, wantErr: "seasons must be a list of season numbers."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t)
			if tc.existing != nil {
				h.seed(tableRequests, requestRow{ID: sonarrID, UserID: otherID, Type: "tv", ExternalID: "1399", Status: "pending", Title: "Game of Thrones", Seasons: tc.existing})
			}
			before := len(h.rows(tableRequests))
			res := h.drive("request", "user", userID, tc.body, withSettings(nil))
			if got := resultError(res); got != tc.wantErr {
				t.Fatalf("error = %q, want %q", got, tc.wantErr)
			}
			if tc.wantErr != "" {
				return
			}
			rows := h.rows(tableRequests)
			created := rows[len(rows)-1]
			if len(rows) != before+1 {
				t.Fatal("no request stored")
			}
			if tc.all {
				if created["seasons"] != nil {
					t.Fatalf("seasons = %v, want null for everything", created["seasons"])
				}
				return
			}
			raw, _ := json.Marshal(created["seasons"])
			want, _ := json.Marshal(tc.wantSeasons)
			if string(raw) != string(want) {
				t.Fatalf("seasons = %s, want %s", raw, want)
			}
		})
	}
}

func TestRequestSeriesAutoApproveDispatch(t *testing.T) {
	h := fixture(t)
	body := map[string]any{"type": "tv", "externalId": "1399", "serviceId": sonarrID, "seasons": []any{1}}
	res := h.drive("request", "user", userID, body, withSettings(map[string]any{"autoApprove": true}))
	if resultError(res) != "" {
		t.Fatal(resultError(res))
	}
	if statusOfOnly(t, h) != "approved" {
		t.Fatalf("status = %v", h.rows(tableRequests)[0])
	}
	if res.Rounds > maxRounds {
		t.Fatalf("rounds = %d", res.Rounds)
	}
}

func TestRequestNeverLeaksSecrets(t *testing.T) {
	h := fixture(t)
	res := h.drive("request", "admin", adminID, map[string]any{"type": "movie", "externalId": "603", "serviceId": radarrID}, withSettings(nil))
	for _, secret := range []string{radarrKey, tmdbKey, discogs} {
		if strings.Contains(res.Raw, secret) {
			t.Fatal("secret in the result")
		}
		for _, row := range h.rows(tableRequests) {
			raw, _ := json.Marshal(row)
			if strings.Contains(string(raw), secret) {
				t.Fatal("secret stored in a table")
			}
		}
	}
}

func TestApproveRetryDeclineCancel(t *testing.T) {
	pending := requestRow{ID: radarrID, UserID: userID, Type: "movie", Source: "tmdb", ExternalID: "603", Title: "The Matrix", Status: "pending", CreatedAt: "2026-03-09T00:00:00.000Z"}
	hint := func(id string) map[string]any {
		return map[string]any{"id": id, "type": "movie", "externalId": "603", "serviceId": radarrID}
	}
	tests := []struct {
		name       string
		op         string
		role, uid  string
		status     string
		body       func(id string) map[string]any
		wantError  string
		wantStatus string
		wantNotify int
		wantGone   bool
	}{
		{name: "admin approves pending", op: "approve", role: "admin", uid: adminID, status: "pending", body: hint, wantStatus: "approved", wantNotify: 1},
		{name: "admin approves declined", op: "approve", role: "admin", uid: adminID, status: "declined", body: hint, wantStatus: "approved", wantNotify: 1},
		{name: "cannot approve an approved request", op: "approve", role: "admin", uid: adminID, status: "approved", body: hint, wantError: "Only pending or declined requests can be approved."},
		{name: "user cannot approve", op: "approve", role: "user", uid: userID, status: "pending", body: hint, wantError: "Only administrators can do this."},
		{name: "system cannot approve", op: "approve", role: "system", uid: "", status: "pending", body: hint, wantError: "Only administrators can do this."},
		{name: "wrong server hint", op: "approve", role: "admin", uid: adminID, status: "pending", body: func(id string) map[string]any {
			return map[string]any{"id": id, "serviceId": sonarrID}
		}, wantError: "The chosen server is not an enabled radarr server."},
		{name: "missing server hint", op: "approve", role: "admin", uid: adminID, status: "pending", body: func(id string) map[string]any {
			return map[string]any{"id": id}
		}, wantError: "The server for this request changed. Reload the page and try again."},
		{name: "invalid id", op: "approve", role: "admin", uid: adminID, status: "pending", body: func(string) map[string]any { return map[string]any{"id": "x"} }, wantError: "Invalid id."},
		{name: "unknown request", op: "approve", role: "admin", uid: adminID, status: "pending", body: func(string) map[string]any { return hint(sonarrID) }, wantError: "Request not found."},
		{name: "retry failed", op: "retry", role: "admin", uid: adminID, status: "failed", body: hint, wantStatus: "approved", wantNotify: 1},
		{name: "retry needs failed", op: "retry", role: "admin", uid: adminID, status: "pending", body: hint, wantError: "Only failed requests can be retried."},
		{name: "user cannot retry", op: "retry", role: "user", uid: userID, status: "failed", body: hint, wantError: "Only administrators can do this."},
		{name: "decline with reason", op: "decline", role: "admin", uid: adminID, status: "pending", body: func(id string) map[string]any {
			return map[string]any{"id": id, "reason": "No thanks"}
		}, wantStatus: "declined", wantNotify: 1},
		{name: "cannot decline fulfilled", op: "decline", role: "admin", uid: adminID, status: "available", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantError: "This request has already been fulfilled."},
		{name: "user cannot decline", op: "decline", role: "user", uid: userID, status: "pending", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantError: "Only administrators can do this."},
		{name: "reason too long", op: "decline", role: "admin", uid: adminID, status: "pending", body: func(id string) map[string]any {
			return map[string]any{"id": id, "reason": strings.Repeat("x", 501)}
		}, wantError: "reason is too long."},
		{name: "owner cancels pending", op: "cancel", role: "user", uid: userID, status: "pending", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantGone: true},
		{name: "owner cannot cancel approved", op: "cancel", role: "user", uid: userID, status: "approved", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantError: "You can only cancel your own pending requests."},
		{name: "other user cannot cancel", op: "cancel", role: "user", uid: otherID, status: "pending", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantError: "You can only cancel your own pending requests."},
		{name: "admin removes any", op: "cancel", role: "admin", uid: adminID, status: "approved", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantGone: true},
		{name: "system cannot cancel", op: "cancel", role: "system", uid: "", status: "pending", body: func(id string) map[string]any { return map[string]any{"id": id} }, wantError: "You must be signed in."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture(t)
			row := pending
			row.Status = tc.status
			h.seed(tableRequests, row)
			res := h.drive(tc.op, tc.role, tc.uid, tc.body(row.ID), withSettings(nil))
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if got := resultError(res); got != tc.wantError {
				t.Fatalf("error = %q, want %q", got, tc.wantError)
			}
			rows := h.rows(tableRequests)
			switch {
			case tc.wantError != "":
				if len(rows) != 1 || rows[0]["status"] != tc.status {
					t.Fatal("a rejected operation must not change the request")
				}
			case tc.wantGone:
				if len(rows) != 0 {
					t.Fatal("request not removed")
				}
			default:
				if rows[0]["status"] != tc.wantStatus {
					t.Fatalf("status = %v want %s (%v)", rows[0]["status"], tc.wantStatus, rows[0]["error"])
				}
				if len(h.notified) != tc.wantNotify {
					t.Fatalf("notifications = %d", len(h.notified))
				}
				if rows[0]["processed_by"] != tc.uid {
					t.Fatalf("processed_by = %v", rows[0]["processed_by"])
				}
			}
		})
	}
}

func TestApproveFailureMarksRequestFailed(t *testing.T) {
	h := fixture(t)
	h.seed(tableRequests, requestRow{ID: sonarrID, UserID: userID, Type: "movie", ExternalID: "603", Title: "The Matrix", Status: "pending"})
	delete(h.secrets, "arr."+radarrID)
	res := h.drive("approve", "admin", adminID, map[string]any{"id": sonarrID, "serviceId": radarrID}, withSettings(nil))
	if resultError(res) != "" {
		t.Fatal(resultError(res))
	}
	row := h.rows(tableRequests)[0]
	if row["status"] != "failed" || !strings.Contains(row["error"].(string), "API key") {
		t.Fatalf("row = %v", row)
	}
	if len(h.notified) != 0 {
		t.Fatal("no approval notification for failures")
	}
}

func TestApproveSeries(t *testing.T) {
	h := fixture(t)
	h.seed(tableRequests, requestRow{ID: radarrID, UserID: userID, Type: "tv", Source: "tmdb", ExternalID: "1399", Title: "Game of Thrones", Status: "pending", Seasons: []int{1}})
	body := map[string]any{"id": radarrID, "type": "tv", "externalId": "1399", "serviceId": sonarrID}
	res := h.drive("approve", "admin", adminID, body, withSettings(nil))
	if resultError(res) != "" {
		t.Fatal(resultError(res))
	}
	if h.rows(tableRequests)[0]["status"] != "approved" {
		t.Fatalf("row = %v", h.rows(tableRequests)[0])
	}
	mismatch := map[string]any{"id": radarrID, "type": "tv", "externalId": "1", "serviceId": sonarrID}
	if got := resultError(h.drive("approve", "admin", adminID, mismatch, withSettings(nil))); got == "" {
		t.Fatal("a hint for another show must be rejected")
	}
}

func TestSync(t *testing.T) {
	setup := func(h *fakeHost) {
		h.seed(tableRequests,
			requestRow{ID: radarrID, UserID: userID, Type: "movie", ExternalID: "603", Title: "The Matrix", Status: "approved", CreatedAt: "2026-03-01T00:00:00.000Z"},
			requestRow{ID: sonarrID, UserID: userID, Type: "movie", ExternalID: "604", Title: "Reloaded", Status: "approved", CreatedAt: "2026-03-02T00:00:00.000Z"},
			requestRow{ID: lidarrID, UserID: userID, Type: "movie", ExternalID: "605", Title: "Pending", Status: "pending", CreatedAt: "2026-03-03T00:00:00.000Z"},
		)
	}
	t.Run("marks matched requests available", func(t *testing.T) {
		h := fixture(t)
		setup(h)
		h.library[radarrID] = "item-9"
		h.library[lidarrID] = "item-10"
		res := h.drive("sync", "system", "", nil, withSettings(nil))
		if res.Err != nil || resultError(res) != "" {
			t.Fatal(res.Err, resultError(res))
		}
		got := map[string]any{}
		for _, row := range h.rows(tableRequests) {
			got[row["id"].(string)] = row["status"]
		}
		if got[radarrID] != "available" || got[sonarrID] != "approved" || got[lidarrID] != "pending" {
			t.Fatalf("statuses = %v", got)
		}
		if len(h.notified) != 1 || h.notified[0]["url"] != "/media/item-9" {
			t.Fatalf("notifications = %v", h.notified)
		}
	})
	t.Run("nothing approved", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("sync", "system", "", nil, withSettings(nil))
		if res.Result["changed"] != float64(0) {
			t.Fatalf("result = %v", res.Result)
		}
	})
	t.Run("notifications can be switched off", func(t *testing.T) {
		h := fixture(t)
		setup(h)
		h.library[radarrID] = "item-9"
		h.drive("sync", "system", "", nil, withSettings(map[string]any{"notifyUsers": false}))
		if len(h.notified) != 0 {
			t.Fatal("unexpected notification")
		}
	})
	t.Run("many matches stay within the call budget", func(t *testing.T) {
		h := fixture(t)
		for i := 0; i < 20; i++ {
			id := newTestID(i)
			h.seed(tableRequests, requestRow{ID: id, UserID: userID, Type: "movie", ExternalID: "1", Title: "t", Status: "approved", CreatedAt: "2026-03-01T00:00:00.000Z"})
			h.library[id] = "item"
		}
		res := h.drive("sync", "system", "", nil, withSettings(nil))
		if res.Result["changed"] != float64(syncNotify) {
			t.Fatalf("result = %v", res.Result)
		}
	})
	for _, role := range []string{"user", "admin", ""} {
		t.Run("forbidden for "+role, func(t *testing.T) {
			h := fixture(t)
			setup(h)
			h.library[radarrID] = "item-9"
			res := h.drive("sync", role, userID, nil, withSettings(nil))
			if resultError(res) != "Only the scheduler can run this." {
				t.Fatalf("result = %v", res.Result)
			}
		})
	}
}

func newTestID(i int) string {
	return "99999999-9999-4999-8999-" + strings.Repeat("0", 12-len(itoa(i))) + itoa(i)
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
