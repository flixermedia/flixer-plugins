package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var nodeTypes = map[string]bool{
	"stack": true, "heading": true, "text": true, "badge": true, "image": true, "grid": true, "card": true, "tabs": true,
	"table": true, "form": true, "button": true, "link": true, "divider": true, "empty": true, "alert": true, "pager": true,
	"searchbox": true, "chips": true,
}

// checkTree validates a view against the UI protocol limits and returns its node count.
func checkTree(t *testing.T, value any, depth int, count *int) {
	t.Helper()
	switch v := value.(type) {
	case []any:
		for _, entry := range v {
			checkTree(t, entry, depth, count)
		}
	case map[string]any:
		if kind, ok := v["type"].(string); ok && nodeTypes[kind] {
			*count++
			depth++
			if depth > 12 {
				t.Fatalf("tree deeper than 12 at %v", kind)
			}
			if kind == "image" {
				if src, _ := v["src"].(string); !strings.HasPrefix(src, "https://") {
					t.Fatalf("image src %q", src)
				}
			}
		}
		for key, entry := range v {
			if key == "fields" {
				continue
			}
			checkTree(t, entry, depth, count)
		}
	}
}

func pageView(t *testing.T, h *fakeHost, role, uid string, query map[string]string, settings map[string]any) (map[string]any, driven) {
	t.Helper()
	res := h.drive("page", role, uid, map[string]any{"page": "requests", "query": query}, settings)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if msg := resultError(res); msg != "" {
		t.Fatalf("page error: %s", msg)
	}
	view, _ := res.Result["view"].(map[string]any)
	if view == nil {
		t.Fatalf("no view: %v", res.Result)
	}
	count := 0
	checkTree(t, view, 0, &count)
	if count > 2000 {
		t.Fatalf("%d nodes", count)
	}
	for _, secret := range []string{radarrKey, tmdbKey, discogs, "sonarr-key", "lidarr-key"} {
		if strings.Contains(res.Raw, secret) {
			t.Fatalf("page leaks a secret (query %v)", query)
		}
	}
	return view, res
}

func tabIDs(view map[string]any) []string {
	var ids []string
	for _, entry := range view["children"].([]any)[0].(map[string]any)["tabs"].([]any) {
		ids = append(ids, entry.(map[string]any)["id"].(string))
	}
	return ids
}

func TestPageTabsByRole(t *testing.T) {
	tests := []struct {
		role, uid string
		want      string
	}{
		{"user", userID, "discover,search,mine"},
		{"admin", adminID, "settings,services,users,all,discover,search,mine"},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			h := fixture(t)
			view, res := pageView(t, h, tc.role, tc.uid, nil, withSettings(nil))
			if got := strings.Join(tabIDs(view), ","); got != tc.want {
				t.Fatalf("tabs = %s", got)
			}
			if res.Rounds > maxRounds {
				t.Fatalf("rounds = %d", res.Rounds)
			}
		})
	}
	t.Run("system has no page", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("page", "system", "", map[string]any{"page": "requests"}, withSettings(nil))
		if resultError(res) != "You must be signed in." {
			t.Fatalf("result = %v", res.Result)
		}
	})
	t.Run("unknown page", func(t *testing.T) {
		h := fixture(t)
		res := h.drive("page", "user", userID, map[string]any{"page": "other"}, withSettings(nil))
		if resultError(res) != "Unknown page." {
			t.Fatalf("result = %v", res.Result)
		}
	})
}

func TestPageUserCannotOpenAdminTabs(t *testing.T) {
	for _, tab := range []string{"all", "services", "users"} {
		t.Run(tab, func(t *testing.T) {
			h := fixture(t)
			h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "movie", ExternalID: "603", Title: "Secret Title", Status: "pending", CreatedAt: "2026-03-09T00:00:00.000Z"})
			_, res := pageView(t, h, "user", userID, map[string]string{"tab": tab}, withSettings(nil))
			if strings.Contains(res.Raw, "Secret Title") || strings.Contains(res.Raw, "bob") {
				t.Fatal("admin data shown to a user")
			}
			if h.usedCall("users.list") != 0 {
				t.Fatal("user list fetched for a user")
			}
		})
	}
}

func TestPageDiscover(t *testing.T) {
	h := fixture(t)
	h.library["movie:603"] = "item-1"
	h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "tv", ExternalID: "1399", Title: "Game of Thrones", Status: "pending"})
	_, res := pageView(t, h, "user", userID, nil, withSettings(nil))
	for _, want := range []string{"Trending", "Popular movies", "Popular series", "Upcoming movies", "Popular albums", "Available", "Requested", "The Matrix", "https://image.tmdb.org/t/p/w342/matrix.jpg"} {
		if !strings.Contains(res.Raw, want) {
			t.Errorf("missing %q", want)
		}
	}
	if res.Rounds != 3 {
		t.Fatalf("rounds = %d", res.Rounds)
	}
}

func TestPageDiscoverWithoutKeysOrServers(t *testing.T) {
	t.Run("missing keys warn admins only", func(t *testing.T) {
		h := fixture(t)
		_, admin := pageView(t, h, "admin", adminID, map[string]string{"tab": "discover"}, map[string]any{})
		if !strings.Contains(admin.Raw, noTMDBText) || !strings.Contains(admin.Raw, noDiscogs) {
			t.Fatal("admin warnings missing")
		}
		_, user := pageView(t, h, "user", userID, nil, map[string]any{})
		if strings.Contains(user.Raw, noTMDBText) {
			t.Fatal("user sees configuration warnings")
		}
	})
	t.Run("without servers nothing can be requested", func(t *testing.T) {
		h := fixture(t)
		h.tables[tableServices] = nil
		_, res := pageView(t, h, "admin", adminID, map[string]string{"tab": "discover"}, withSettings(nil))
		if !strings.Contains(res.Raw, "Servers tab") || strings.Contains(res.Raw, "Popular movies") {
			t.Fatal("expected the setup hint and no sections")
		}
	})
	t.Run("draft servers do not count", func(t *testing.T) {
		h := fixture(t)
		draft := service(radarrID, "radarr", "Movies")
		draft.RootFolder, draft.QualityProfileID, draft.Enabled = "", 0, false
		h.tables[tableServices] = nil
		h.seed(tableServices, draft)
		_, res := pageView(t, h, "user", userID, nil, withSettings(nil))
		if strings.Contains(res.Raw, "Popular movies") {
			t.Fatal("movies shown with only a draft server")
		}
	})
	t.Run("provider failure", func(t *testing.T) {
		h := fixture(t)
		h.http = func(string, string, map[string]string, string) (int, string) { return 500, "{}" }
		pageView(t, h, "user", userID, nil, withSettings(nil))
	})
}

func TestPageSearch(t *testing.T) {
	h := fixture(t)
	_, res := pageView(t, h, "user", userID, map[string]string{"tab": "search", "q": "matrix"}, withSettings(nil))
	for _, want := range []string{"The Matrix", "Game of Thrones", "Daft Punk"} {
		if !strings.Contains(res.Raw, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(res.Raw, "Someone") {
		t.Fatal("people must not be listed")
	}
	_, onlyMovies := pageView(t, fixture(t), "user", userID, map[string]string{"tab": "search", "q": "matrix", "type": "movie"}, withSettings(nil))
	if strings.Contains(onlyMovies.Raw, "Game of Thrones") || strings.Contains(onlyMovies.Raw, "Daft Punk") {
		t.Fatal("type filter ignored")
	}
	_, blank := pageView(t, fixture(t), "user", userID, map[string]string{"tab": "search", "q": "  "}, withSettings(nil))
	if !strings.Contains(blank.Raw, "Type something to search for.") {
		t.Fatal("empty search prompt missing")
	}
	_, long := pageView(t, fixture(t), "user", userID, map[string]string{"tab": "search", "q": strings.Repeat("a", 500)}, withSettings(nil))
	if strings.Contains(long.Raw, strings.Repeat("a", 101)) {
		t.Fatal("search term not truncated")
	}
}

func TestPageMyRequests(t *testing.T) {
	h := fixture(t)
	for i := 0; i < 12; i++ {
		h.seed(tableRequests, requestRow{ID: newTestID(i), UserID: userID, Type: "movie", ExternalID: "603", Title: fmt.Sprintf("Mine %02d", i), Status: "pending", CreatedAt: fmt.Sprintf("2026-03-%02dT00:00:00.000Z", i+1)})
	}
	h.seed(tableRequests,
		requestRow{ID: newTestID(50), UserID: otherID, Type: "movie", ExternalID: "603", Title: "Not mine", Status: "pending", CreatedAt: "2026-03-09T00:00:00.000Z"},
		requestRow{ID: newTestID(51), UserID: userID, Type: "movie", ExternalID: "603", Title: "Gone", Status: "declined", DeclineReason: ptr("Too long"), CreatedAt: "2026-03-20T00:00:00.000Z"},
	)
	_, res := pageView(t, h, "user", userID, map[string]string{"tab": "mine"}, withSettings(nil))
	if strings.Contains(res.Raw, "Not mine") {
		t.Fatal("another user's request is shown")
	}
	if !strings.Contains(res.Raw, "Gone") || !strings.Contains(res.Raw, "Too long") || !strings.Contains(res.Raw, `"type":"pager"`) {
		t.Fatal("expected the newest request, its reason and a pager")
	}
	if strings.Contains(res.Raw, `"op":"approve"`) || strings.Contains(res.Raw, `"op":"decline"`) {
		t.Fatal("admin actions shown to a user")
	}
	if !strings.Contains(res.Raw, `"op":"cancel"`) {
		t.Fatal("pending requests can be cancelled")
	}
	_, filtered := pageView(t, h, "user", userID, map[string]string{"tab": "mine", "status": "declined"}, withSettings(nil))
	if strings.Contains(filtered.Raw, "Mine 01") || !strings.Contains(filtered.Raw, "Gone") {
		t.Fatal("status filter ignored")
	}
	_, second := pageView(t, h, "user", userID, map[string]string{"tab": "mine", "page": "2"}, withSettings(nil))
	if !strings.Contains(second.Raw, "Mine 00") || strings.Contains(second.Raw, "Gone") {
		t.Fatal("second page wrong")
	}
	_, bad := pageView(t, h, "user", userID, map[string]string{"tab": "mine", "page": "-4", "status": "bogus"}, withSettings(nil))
	if !strings.Contains(bad.Raw, "Gone") {
		t.Fatal("invalid paging must fall back to the first page")
	}
}

func TestPageAllRequestsAdmin(t *testing.T) {
	h := fixture(t)
	h.seed(tableRequests,
		requestRow{ID: radarrID, UserID: userID, Type: "movie", ExternalID: "603", Title: "Pending one", Status: "pending", CreatedAt: "2026-03-09T00:00:00.000Z"},
		requestRow{ID: sonarrID, UserID: otherID, Type: "movie", ExternalID: "603", Title: "Failed one", Status: "failed", Error: ptr("boom"), CreatedAt: "2026-03-08T00:00:00.000Z"},
	)
	_, res := pageView(t, h, "admin", adminID, map[string]string{"tab": "all"}, withSettings(nil))
	for _, want := range []string{"Pending one", "alice", "bob", `"op":"approve"`, `"op":"decline"`, `"op":"retry"`, "boom"} {
		if !strings.Contains(res.Raw, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(res.Raw, radarrID) { // the approve hint names the default radarr server
		t.Error("approve hint missing")
	}
}

func TestPageServices(t *testing.T) {
	h := fixture(t)
	_, list := pageView(t, h, "admin", adminID, map[string]string{"tab": "services"}, withSettings(nil))
	for _, want := range []string{"Movies", "Shows", "Music", "https://radarr.example.com", `"op":"service-test"`, `"op":"service-delete"`} {
		if !strings.Contains(list.Raw, want) {
			t.Errorf("missing %q", want)
		}
	}
	_, edit := pageView(t, h, "admin", adminID, map[string]string{"tab": "services", "edit": sonarrID}, withSettings(nil))
	if !strings.Contains(edit.Raw, `"name":"rootFolder"`) || !strings.Contains(edit.Raw, "/tv") || !strings.Contains(edit.Raw, "HD") {
		t.Fatal("edit form should offer the server's folders and profiles")
	}
	if edit.Rounds != 3 {
		t.Fatalf("rounds = %d", edit.Rounds)
	}
	t.Run("unreachable server falls back to text inputs", func(t *testing.T) {
		h := fixture(t)
		h.http = func(string, string, map[string]string, string) (int, string) { return 0, "" }
		_, res := pageView(t, h, "admin", adminID, map[string]string{"tab": "services", "edit": radarrID}, withSettings(nil))
		if !strings.Contains(res.Raw, "Could not reach radarr") || !strings.Contains(res.Raw, `"name":"qualityProfileId"`) {
			t.Fatal("expected a warning and manual fields")
		}
	})
	t.Run("unknown server", func(t *testing.T) {
		_, res := pageView(t, fixture(t), "admin", adminID, map[string]string{"tab": "services", "edit": otherID}, withSettings(nil))
		if !strings.Contains(res.Raw, "Server not found.") {
			t.Fatal("expected not found")
		}
	})
}

func TestPageUsers(t *testing.T) {
	h := fixture(t)
	h.seed(tableLimits, limitRow{UserID: userID, CanRequest: ptr(false), QuotaLimit: ptr(3)})
	_, res := pageView(t, h, "admin", adminID, map[string]string{"tab": "users"}, withSettings(nil))
	if !strings.Contains(res.Raw, "alice") || !strings.Contains(res.Raw, "bob") || strings.Contains(res.Raw, "root") {
		t.Fatal("expected non-admin users only")
	}
	if !strings.Contains(res.Raw, `"op":"user-limit-save"`) || !strings.Contains(res.Raw, `"deny"`) {
		t.Fatal("expected limit forms with the stored override")
	}
}

func TestPageDetails(t *testing.T) {
	t.Run("movie", func(t *testing.T) {
		h := fixture(t)
		_, res := pageView(t, h, "user", userID, map[string]string{"view": "details", "type": "movie", "id": "603", "from": "search", "q": "matrix"}, withSettings(nil))
		for _, want := range []string{"The Matrix", "136 min", "Keanu Reeves", `"op":"request"`, `"name":"serviceId"`, radarrID, `"Back"`} {
			if !strings.Contains(res.Raw, want) {
				t.Errorf("missing %q", want)
			}
		}
		if res.Rounds != 3 {
			t.Fatalf("rounds = %d", res.Rounds)
		}
	})
	t.Run("series offers season checkboxes without specials", func(t *testing.T) {
		h := fixture(t)
		h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "tv", ExternalID: "1399", Title: "Game of Thrones", Status: "approved", Seasons: []int{1}})
		_, res := pageView(t, h, "user", userID, map[string]string{"view": "details", "type": "tv", "id": "1399"}, withSettings(nil))
		if !strings.Contains(res.Raw, `"name":"season_2"`) || !strings.Contains(res.Raw, `"name":"season_3"`) {
			t.Fatal("remaining seasons missing")
		}
		if strings.Contains(res.Raw, `"name":"season_1"`) || strings.Contains(res.Raw, `"name":"season_0"`) {
			t.Fatal("requested seasons and specials must not be offered")
		}
	})
	t.Run("series fully requested", func(t *testing.T) {
		h := fixture(t)
		h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "tv", ExternalID: "1399", Title: "Game of Thrones", Status: "pending"})
		_, res := pageView(t, h, "user", userID, map[string]string{"view": "details", "type": "tv", "id": "1399"}, withSettings(nil))
		if !strings.Contains(res.Raw, "Every season has already been requested.") || strings.Contains(res.Raw, `"op":"request"`) {
			t.Fatal("expected no form")
		}
	})
	t.Run("album", func(t *testing.T) {
		_, res := pageView(t, fixture(t), "user", userID, map[string]string{"view": "details", "type": "music", "id": "5"}, withSettings(nil))
		if !strings.Contains(res.Raw, "Discovery") || !strings.Contains(res.Raw, "Daft Punk") || !strings.Contains(res.Raw, "One More Time") {
			t.Fatal("album details missing")
		}
	})
	t.Run("already requested", func(t *testing.T) {
		h := fixture(t)
		h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "movie", ExternalID: "603", Title: "The Matrix", Status: "pending"})
		_, res := pageView(t, h, "user", userID, map[string]string{"view": "details", "type": "movie", "id": "603"}, withSettings(nil))
		if !strings.Contains(res.Raw, "This has already been requested.") || strings.Contains(res.Raw, `"op":"request"`) {
			t.Fatal("expected no form")
		}
	})
	t.Run("in the library", func(t *testing.T) {
		h := fixture(t)
		h.library["movie:603"] = "item"
		_, res := pageView(t, h, "user", userID, map[string]string{"view": "details", "type": "movie", "id": "603"}, withSettings(nil))
		if !strings.Contains(res.Raw, "This is already in the library.") {
			t.Fatal("expected the library message")
		}
	})
	t.Run("not allowed to request", func(t *testing.T) {
		_, res := pageView(t, fixture(t), "user", userID, map[string]string{"view": "details", "type": "movie", "id": "603"}, withSettings(map[string]any{"defaultCanRequest": false}))
		if !strings.Contains(res.Raw, "You are not allowed to make requests.") || strings.Contains(res.Raw, `"op":"request"`) {
			t.Fatal("expected no form")
		}
	})
	t.Run("admin picks the server when several exist", func(t *testing.T) {
		h := fixture(t)
		second := service("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "radarr", "4K")
		second.IsDefault = false
		h.seed(tableServices, second)
		_, res := pageView(t, h, "admin", adminID, map[string]string{"view": "details", "type": "movie", "id": "603"}, withSettings(nil))
		if !strings.Contains(res.Raw, `"type":"select"`) || !strings.Contains(res.Raw, "4K") {
			t.Fatal("expected a server select")
		}
	})
	t.Run("invalid input", func(t *testing.T) {
		for _, q := range []map[string]string{
			{"view": "details", "type": "game", "id": "1"},
			{"view": "details", "type": "movie", "id": "abc"},
			{"view": "details", "type": "movie", "id": "../../x"},
			{"view": "details"},
		} {
			_, res := pageView(t, fixture(t), "user", userID, q, withSettings(nil))
			if !strings.Contains(res.Raw, "Invalid title.") {
				t.Fatalf("query %v accepted", q)
			}
		}
	})
	t.Run("unknown title", func(t *testing.T) {
		_, res := pageView(t, fixture(t), "user", userID, map[string]string{"view": "details", "type": "movie", "id": "999"}, withSettings(nil))
		if !strings.Contains(res.Raw, errNotFound) {
			t.Fatal("expected not found")
		}
	})
}

func TestPageRejectsMalformedBody(t *testing.T) {
	h := fixture(t)
	res := h.drive("page", "user", userID, []any{1}, withSettings(nil))
	if resultError(res) == "" {
		t.Fatal("expected an error")
	}
	raw, _ := json.Marshal(res.Result)
	_ = raw
}
