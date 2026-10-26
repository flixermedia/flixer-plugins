package main

import "testing"

func searchItems(t *testing.T, res driven) []map[string]any {
	t.Helper()
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	secs, _ := res.Result["sections"].([]any)
	var out []map[string]any
	for _, s := range secs {
		for _, it := range s.(map[string]any)["items"].([]any) {
			out = append(out, it.(map[string]any))
		}
	}
	return out
}

func TestSearchReturnsItemsWithDetailsQuery(t *testing.T) {
	h := fixture(t)
	res := h.drive("search", "user", userID, map[string]any{"q": "thrones"}, withSettings(nil))
	items := searchItems(t, res)
	if len(items) == 0 || len(items) > 20 {
		t.Fatalf("items = %v (%v)", items, res.Result)
	}
	if res.Rounds > 3 {
		t.Fatalf("rounds = %d", res.Rounds)
	}
	for _, it := range items {
		if it["page"] != "requests" {
			t.Fatalf("page = %v", it["page"])
		}
		q := it["query"].(map[string]any)
		if q["view"] != "details" || q["id"] == "" || q["type"] == "" {
			t.Fatalf("query = %v", q)
		}
		if p, _ := it["poster"].(string); p != "" && p[:8] != "https://" {
			t.Fatalf("poster = %v", p)
		}
	}
}

func TestSearchExcludesLibraryAndShowsBadge(t *testing.T) {
	h := fixture(t)
	first := searchItems(t, h.drive("search", "user", userID, map[string]any{"q": "thrones"}, withSettings(nil)))
	if len(first) == 0 {
		t.Fatal("no items")
	}
	q := first[0]["query"].(map[string]any)
	h.seed(tableRequests, map[string]any{"id": "r1", "type": q["type"], "external_id": q["id"], "user_id": userID, "status": "pending", "title": "x"})
	badged := searchItems(t, h.drive("search", "user", userID, map[string]any{"q": "thrones"}, withSettings(nil)))
	b, _ := badged[0]["badge"].(map[string]any)
	if b == nil || b["tone"] != "warning" {
		t.Fatalf("badge = %v", badged[0])
	}
	h.library[cardKey(q["type"].(string), q["id"].(string))] = "lib1"
	h.tables[tableRequests] = nil
	after := searchItems(t, h.drive("search", "user", userID, map[string]any{"q": "thrones"}, withSettings(nil)))
	for _, it := range after {
		if it["query"].(map[string]any)["id"] == q["id"] && it["query"].(map[string]any)["type"] == q["type"] {
			t.Fatalf("library item still listed: %v", it)
		}
	}
}

func TestSearchEmptyWithoutKeysOrBadInput(t *testing.T) {
	h := fixture(t)
	h.secrets = map[string]string{}
	for _, body := range []map[string]any{{"q": "thrones"}, {"q": "x"}, {"q": "thrones", "type": "bogus"}} {
		res := h.drive("search", "user", userID, body, withSettings(map[string]any{"tmdbApiKey": "", "discogsToken": ""}))
		if items := searchItems(t, res); len(items) != 0 {
			t.Fatalf("%v -> %v", body, items)
		}
	}
	if res := h.drive("search", "", "", map[string]any{"q": "thrones"}, nil); res.Err == nil && resultError(res) == "" {
		t.Fatal("anonymous search allowed")
	}
}
