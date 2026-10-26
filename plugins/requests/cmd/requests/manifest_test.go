package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type manifest struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Permissions  []string `json:"permissions"`
	Targets      map[string]struct {
		Entry string `json:"entry"`
	} `json:"targets"`
	Database struct {
		Tables map[string]struct {
			Columns map[string]struct {
				Type     string `json:"type"`
				Nullable *bool  `json:"nullable"`
				Primary  bool   `json:"primary"`
			} `json:"columns"`
			Indexes []struct {
				Columns []string `json:"columns"`
			} `json:"indexes"`
		} `json:"tables"`
	} `json:"database"`
	Schedules []struct {
		Operation       string `json:"operation"`
		IntervalSeconds int    `json:"intervalSeconds"`
	} `json:"schedules"`
	UI struct {
		Pages []struct {
			ID, Title, Nav string
		} `json:"pages"`
	} `json:"ui"`
	SettingsSchema string `json:"settingsSchema"`
}

func loadManifest(t *testing.T) manifest {
	t.Helper()
	raw, err := os.ReadFile("../../1.0.0/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func columnsOf(row any) []string {
	var names []string
	rt := reflect.TypeOf(row)
	for i := 0; i < rt.NumField(); i++ {
		names = append(names, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
	}
	sort.Strings(names)
	return names
}

func TestManifestMatchesCode(t *testing.T) {
	m := loadManifest(t)
	if m.ID != "flixer.requests" || m.Version != "1.0.0" || m.Targets["flixer"].Entry != "backend/plugin.wasm" {
		t.Fatalf("identity: %+v", m)
	}
	t.Run("tables match the row types", func(t *testing.T) {
		rows := map[string]any{tableRequests: requestRow{}, tableServices: serviceRow{}, tableLimits: limitRow{}}
		if len(m.Database.Tables) != len(rows) {
			t.Fatalf("tables = %d", len(m.Database.Tables))
		}
		for name, row := range rows {
			var declared []string
			for column, def := range m.Database.Tables[name].Columns {
				declared = append(declared, column)
				if def.Nullable == nil && !def.Primary {
					t.Errorf("%s.%s: declare nullability", name, column)
				}
			}
			sort.Strings(declared)
			if !reflect.DeepEqual(declared, columnsOf(row)) {
				t.Errorf("%s: manifest %v, code %v", name, declared, columnsOf(row))
			}
		}
	})
	t.Run("permissions cover the operations", func(t *testing.T) {
		have := map[string]bool{}
		for _, permission := range m.Permissions {
			have[permission] = true
		}
		for op := range operations {
			if !have["api.routes."+op] {
				t.Errorf("missing api.routes.%s", op)
			}
		}
		for permission := range have {
			if op, ok := strings.CutPrefix(permission, "api.routes."); ok && operations[op] == nil {
				t.Errorf("permission for unknown operation %s", op)
			}
		}
		for _, required := range []string{"database.tables", "secrets.manage", "users.read", "library.read", "notifications.send", "schedule.run", "settings.read", "network.http.any"} {
			if !have[required] {
				t.Errorf("missing %s", required)
			}
		}
	})
	t.Run("schedule and page", func(t *testing.T) {
		if len(m.Schedules) != 1 || m.Schedules[0].Operation != "sync" || m.Schedules[0].IntervalSeconds != 300 || operations["sync"] == nil {
			t.Fatalf("schedules = %+v", m.Schedules)
		}
		if len(m.UI.Pages) != 1 || m.UI.Pages[0].ID != "requests" || m.UI.Pages[0].Nav != "main" {
			t.Fatalf("pages = %+v", m.UI.Pages)
		}
	})
	t.Run("capabilities", func(t *testing.T) {
		for _, want := range []string{"api.routes", "schedule", "ui.pages", "network.http", "database.tables"} {
			found := false
			for _, got := range m.Capabilities {
				found = found || got == want
			}
			if !found {
				t.Errorf("missing capability %s", want)
			}
		}
	})
}

func TestSettingsSchema(t *testing.T) {
	raw, err := os.ReadFile("../../1.0.0/settings.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Type    string `json:"type"`
			Secret  bool   `json:"x-flixer-secret"`
			Default any    `json:"default"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tmdbApiKey", "discogsToken"} {
		if p := schema.Properties[key]; !p.Secret || p.Type != "string" {
			t.Errorf("%s must be a secret string", key)
		}
	}
	for _, key := range []string{"moviesEnabled", "tvEnabled", "musicEnabled", "defaultCanRequest", "autoApprove", "quotaLimit", "quotaDays", "notifyAdmins", "notifyUsers", "language"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("missing %s", key)
		}
	}
	// The defaults the schema advertises must be what the code assumes.
	cfg := parseSettings(map[string]any{})
	want := map[string]any{
		"moviesEnabled": cfg.Movies, "tvEnabled": cfg.TV, "musicEnabled": cfg.Music, "defaultCanRequest": cfg.DefaultCanRequest,
		"autoApprove": cfg.AutoApprove, "notifyAdmins": cfg.NotifyAdmins, "notifyUsers": cfg.NotifyUsers,
		"quotaLimit": float64(cfg.QuotaLimit), "quotaDays": float64(cfg.QuotaDays), "language": cfg.Language,
	}
	for key, value := range want {
		if schema.Properties[key].Default != value {
			t.Errorf("%s default = %v, code assumes %v", key, schema.Properties[key].Default, value)
		}
	}
}

func TestParseSettings(t *testing.T) {
	got := parseSettings(map[string]any{
		"tmdbApiKey": " key ", "quotaLimit": float64(5000), "quotaDays": float64(0), "language": "not a language", "moviesEnabled": false, "autoApprove": "yes",
	})
	if got.TMDBKey != "key" || got.QuotaLimit != 0 || got.QuotaDays != 7 || got.Language != "en" || got.Movies || got.AutoApprove {
		t.Fatalf("settings = %+v", got)
	}
	if parseSettings(nil).Language != "en" {
		t.Fatal("nil settings")
	}
}

func TestRunProtocolErrors(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"bad json", `{`, "decode request"},
		{"wrong api version", `{"apiVersion":2,"operation":"page"}`, "unsupported plugin API version"},
		{"unknown operation", `{"apiVersion":1,"operation":"nope"}`, "unsupported operation"},
		{"bad continuation", `{"apiVersion":1,"operation":"page","user":{"id":"` + userID + `","role":"user"},"continuation":{"v":9}}`, "invalid continuation"},
		{"bad now", `{"apiVersion":1,"operation":"page","now":"yesterday"}`, "invalid now"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(strings.NewReader(tc.input), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRoundProtocol(t *testing.T) {
	handler := func(c *Ctx) (any, error) {
		a := Round(c, []hostCall{secretGet("a")}, func(rs []hostResult) string { return rs[0].Value })
		b := Round(c, []hostCall{secretGet("b-" + a)}, func(rs []hostResult) string { return rs[0].Value })
		return map[string]string{"a": a, "b": b}, nil
	}
	exec := func(env envelope) (any, error) { return execute(&env, handler) }

	out, err := exec(envelope{APIVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := out.(callsOutput)
	if len(first.HostCalls) != 1 || first.HostCalls[0]["id"] != "0.0" || first.HostCalls[0]["name"] != "b-" && first.HostCalls[0]["name"] != "a" {
		t.Fatalf("first = %+v", first)
	}
	out, err = exec(envelope{APIVersion: 1, HostResults: []hostResult{{ID: "0.0", OK: true, Value: "X"}}})
	if err != nil {
		t.Fatal(err)
	}
	second := out.(callsOutput)
	if second.HostCalls[0]["id"] != "1.0" || second.HostCalls[0]["name"] != "b-X" || len(second.Continuation.Rounds) != 1 {
		t.Fatalf("second = %+v", second)
	}
	raw, _ := json.Marshal(second.Continuation)
	out, err = exec(envelope{APIVersion: 1, Continuation: raw, HostResults: []hostResult{{ID: "1.0", OK: true, Value: "Y"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(resultOutput).Result.(map[string]string); got["a"] != "X" || got["b"] != "Y" {
		t.Fatalf("result = %v", got)
	}

	t.Run("missing host result", func(t *testing.T) {
		out, err := exec(envelope{APIVersion: 1, HostResults: []hostResult{{ID: "9.9", OK: true}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := out.(callsOutput); !ok {
			t.Fatalf("out = %v", out)
		}
	})
	t.Run("too many calls", func(t *testing.T) {
		_, err := execute(&envelope{APIVersion: 1}, func(c *Ctx) (any, error) {
			Round(c, make([]hostCall, 9), func([]hostResult) int { return 0 })
			return nil, nil
		})
		if err == nil {
			t.Fatal("expected a protocol error")
		}
	})
	t.Run("too many rounds", func(t *testing.T) {
		_, err := execute(&envelope{APIVersion: 1, Continuation: json.RawMessage(`{"v":1,"rounds":[1,2,3,4]}`)}, func(c *Ctx) (any, error) {
			for i := 0; i < 5; i++ {
				Round(c, []hostCall{secretGet("x")}, func([]hostResult) int { return 0 })
			}
			return nil, nil
		})
		if err == nil {
			t.Fatal("expected a protocol error")
		}
	})
	t.Run("empty round is free", func(t *testing.T) {
		out, err := exec2(func(c *Ctx) (any, error) {
			Round(c, nil, func([]hostResult) int { return 0 })
			return "ok", nil
		})
		if err != nil || out.(resultOutput).Result != "ok" {
			t.Fatalf("out = %v, err = %v", out, err)
		}
	})
	t.Run("user errors become results", func(t *testing.T) {
		out, _ := exec2(func(c *Ctx) (any, error) { return nil, fail("nope %d", 1) })
		if out.(resultOutput).Result.(map[string]any)["error"] != "nope 1" {
			t.Fatalf("out = %v", out)
		}
	})
}

func exec2(handler func(c *Ctx) (any, error)) (any, error) {
	return execute(&envelope{APIVersion: 1}, handler)
}

func TestCrossUserIsolationOfRequests(t *testing.T) {
	h := fixture(t)
	h.seed(tableRequests, requestRow{ID: radarrID, UserID: otherID, Type: "movie", ExternalID: "603", Title: "Bob's", Status: "pending", CreatedAt: "2026-03-09T00:00:00.000Z"})
	res := h.drive("cancel", "user", userID, map[string]any{"id": radarrID}, withSettings(nil))
	if resultError(res) == "" || len(h.rows(tableRequests)) != 1 {
		t.Fatal("a user removed someone else's request")
	}
}

func TestNewID(t *testing.T) {
	opName := regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	permission := regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	for op := range operations {
		if !opName.MatchString(op) || !permission.MatchString("api.routes."+op) {
			t.Errorf("operation %q is not host-valid", op)
		}
	}
	for _, p := range loadManifest(t).Permissions {
		if !permission.MatchString(p) {
			t.Errorf("permission %q is not host-valid", p)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := realNewID()
		if !uuidPattern.MatchString(id) || id[14] != '4' || seen[id] {
			t.Fatalf("bad id %q", id)
		}
		seen[id] = true
	}
}
