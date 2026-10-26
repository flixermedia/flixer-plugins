package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// fakeHost answers host calls like the Flixer host would: an in-memory database, secrets, users, library and HTTP routes.
type fakeHost struct {
	t        *testing.T
	tables   map[string][]map[string]any
	secrets  map[string]string
	users    []hostUser
	library  map[string]string // card key -> item id
	notified []map[string]any
	http     func(method, rawURL string, headers map[string]string, body string) (int, string)
	httpLog  []string
	calls    []map[string]any
	rounds   int
	maxCalls int
}

func newFakeHost(t *testing.T) *fakeHost {
	return &fakeHost{
		t: t, tables: map[string][]map[string]any{}, secrets: map[string]string{}, library: map[string]string{},
		http: func(string, string, map[string]string, string) (int, string) { return 404, "{}" },
	}
}

func normalize(value any) any {
	raw, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (h *fakeHost) seed(table string, rows ...any) {
	for _, row := range rows {
		h.tables[table] = append(h.tables[table], normalize(row).(map[string]any))
	}
}

func compare(a, b any) int {
	if x, ok := a.(float64); ok {
		y, _ := b.(float64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

func (h *fakeHost) matches(row map[string]any, where []any) bool {
	for _, entry := range where {
		w := entry.(map[string]any)
		col, cmp, want := w["col"].(string), w["cmp"].(string), normalize(w["value"])
		got := row[col]
		var ok bool
		switch cmp {
		case "eq":
			ok = got != nil && compare(got, want) == 0
		case "ne":
			ok = got == nil || compare(got, want) != 0
		case "gt":
			ok = got != nil && compare(got, want) > 0
		case "gte":
			ok = got != nil && compare(got, want) >= 0
		case "lt":
			ok = got != nil && compare(got, want) < 0
		case "lte":
			ok = got != nil && compare(got, want) <= 0
		case "in":
			for _, candidate := range want.([]any) {
				ok = ok || (got != nil && compare(got, candidate) == 0)
			}
		case "isnull":
			ok = got == nil
		case "notnull":
			ok = got != nil
		default:
			h.t.Fatalf("unsupported comparison %q", cmp)
		}
		if !ok {
			return false
		}
	}
	return true
}

func (h *fakeHost) filter(call map[string]any) []map[string]any {
	table, _ := call["table"].(string)
	where, _ := normalize(call["where"]).([]any)
	var out []map[string]any
	for _, row := range h.tables[table] {
		if h.matches(row, where) {
			out = append(out, row)
		}
	}
	return out
}

func (h *fakeHost) answer(call map[string]any) map[string]any {
	op, _ := call["op"].(string)
	res := map[string]any{"id": call["id"], "ok": true}
	switch op {
	case "db.find":
		rows := h.filter(call)
		if orders, ok := normalize(call["orderBy"]).([]any); ok && len(orders) > 0 {
			first := orders[0].(map[string]any)
			col, desc := first["col"].(string), first["dir"] == "desc"
			sort.SliceStable(rows, func(i, j int) bool {
				if desc {
					return compare(rows[i][col], rows[j][col]) > 0
				}
				return compare(rows[i][col], rows[j][col]) < 0
			})
		}
		offset, _ := call["offset"].(int)
		limit, _ := call["limit"].(int)
		if offset > len(rows) {
			offset = len(rows)
		}
		rows = rows[offset:]
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		res["rows"] = rows
	case "db.count":
		res["count"] = len(h.filter(call))
	case "db.insert":
		table := call["table"].(string)
		for _, row := range normalize(call["rows"]).([]any) {
			h.tables[table] = append(h.tables[table], row.(map[string]any))
		}
		res["inserted"] = len(call["rows"].([]map[string]any))
	case "db.update":
		rows := h.filter(call)
		for _, row := range rows {
			for key, value := range normalize(call["set"]).(map[string]any) {
				row[key] = value
			}
		}
		res["updated"] = len(rows)
	case "db.delete":
		table := call["table"].(string)
		doomed := h.filter(call)
		kept := h.tables[table][:0:0]
		for _, row := range h.tables[table] {
			isDoomed := false
			for _, d := range doomed {
				isDoomed = isDoomed || fmt.Sprint(d) == fmt.Sprint(row)
			}
			if !isDoomed {
				kept = append(kept, row)
			}
		}
		h.tables[table] = kept
		res["deleted"] = len(doomed)
	case "secret.get":
		if value, ok := h.secrets[call["name"].(string)]; ok {
			res["value"] = value
		} else {
			res["ok"], res["error"] = false, "not_found"
		}
	case "secret.set":
		h.secrets[call["name"].(string)] = call["value"].(string)
	case "users.get":
		found := []hostUser{}
		for _, id := range call["ids"].([]string) {
			for _, user := range h.users {
				if user.ID == id {
					found = append(found, user)
				}
			}
		}
		res["users"] = found
	case "users.list":
		limit, offset := call["limit"].(int), call["offset"].(int)
		list := h.users
		if offset > len(list) {
			offset = len(list)
		}
		list = list[offset:]
		if len(list) > limit {
			list = list[:limit]
		}
		res["users"], res["total"] = list, len(h.users)
	case "library.match":
		matches := map[string]string{}
		for _, c := range call["cards"].([]libraryCard) {
			if id, ok := h.library[c.Key]; ok {
				matches[c.Key] = id
			}
		}
		res["matches"] = matches
	case "notify.send":
		h.notified = append(h.notified, normalize(call).(map[string]any))
	case "http.request":
		headers, _ := call["headers"].(map[string]string)
		body, _ := call["body"].(string)
		target := call["url"].(string)
		h.httpLog = append(h.httpLog, call["method"].(string)+" "+target)
		status, payload := h.http(call["method"].(string), target, headers, body)
		res["status"], res["body"] = status, payload
		res["ok"] = status >= 200 && status < 300
		if status == 0 {
			res["error"] = "unreachable"
		}
	default:
		h.t.Fatalf("unexpected host call %q", op)
	}
	return res
}

type driven struct {
	Result map[string]any
	Raw    string
	Rounds int
	Err    error
}

// drive runs an operation to completion, answering host calls and enforcing the round and call budgets.
func (h *fakeHost) drive(operation, role, userID string, body any, settings map[string]any) driven {
	h.t.Helper()
	env := map[string]any{
		"apiVersion": 1, "operation": operation, "method": "POST",
		"user":     map[string]any{"id": userID, "role": role},
		"request":  map[string]any{"method": "POST", "body": body},
		"settings": settings, "now": "2026-03-10T12:00:00.000Z",
	}
	rounds := 0
	for {
		raw, err := json.Marshal(env)
		if err != nil {
			h.t.Fatal(err)
		}
		var out bytes.Buffer
		if err := run(bytes.NewReader(raw), &out); err != nil {
			return driven{Err: err, Rounds: rounds}
		}
		var doc struct {
			Result       map[string]any `json:"result"`
			HostCalls    []map[string]any
			Continuation json.RawMessage `json:"continuation"`
		}
		var generic map[string]json.RawMessage
		_ = json.Unmarshal(out.Bytes(), &generic)
		if rawCalls, ok := generic["hostCalls"]; ok {
			// Decode the calls the way the host would, but keep typed values for the fake.
			_ = json.Unmarshal(rawCalls, &doc.HostCalls)
			doc.Continuation = generic["continuation"]
		} else {
			if err := json.Unmarshal(generic["result"], &doc.Result); err != nil {
				// Results that are not objects (none today) are exposed through Raw.
				doc.Result = nil
			}
			return driven{Result: doc.Result, Raw: out.String(), Rounds: rounds}
		}
		rounds++
		if rounds > maxRounds {
			h.t.Fatalf("%s needed more than %d rounds", operation, maxRounds)
		}
		if len(doc.HostCalls) == 0 || len(doc.HostCalls) > maxCallsPerRound {
			h.t.Fatalf("%s sent %d calls in round %d", operation, len(doc.HostCalls), rounds)
		}
		results := make([]map[string]any, 0, len(doc.HostCalls))
		for _, call := range doc.HostCalls {
			h.calls = append(h.calls, call)
			results = append(results, h.answer(h.typed(call)))
		}
		env["hostResults"] = results
		var continuation any
		_ = json.Unmarshal(doc.Continuation, &continuation)
		env["continuation"] = continuation
	}
}

// typed restores the Go types the answer function expects after a JSON round trip.
func (h *fakeHost) typed(call map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range call {
		out[key] = value
	}
	switch call["op"] {
	case "db.find":
		out["limit"] = int(call["limit"].(float64))
		if offset, ok := call["offset"].(float64); ok {
			out["offset"] = int(offset)
		}
	case "db.insert":
		rows := []map[string]any{}
		for _, row := range call["rows"].([]any) {
			rows = append(rows, row.(map[string]any))
		}
		out["rows"] = rows
	case "users.get":
		ids := []string{}
		for _, id := range call["ids"].([]any) {
			ids = append(ids, id.(string))
		}
		out["ids"] = ids
	case "users.list":
		out["limit"], out["offset"] = int(call["limit"].(float64)), int(call["offset"].(float64))
	case "library.match":
		var cards []libraryCard
		raw, _ := json.Marshal(call["cards"])
		_ = json.Unmarshal(raw, &cards)
		out["cards"] = cards
	case "http.request":
		headers := map[string]string{}
		if given, ok := call["headers"].(map[string]any); ok {
			for key, value := range given {
				headers[key] = value.(string)
			}
		}
		out["headers"] = headers
	}
	return out
}

func (h *fakeHost) rows(table string) []map[string]any { return h.tables[table] }

func (h *fakeHost) usedCall(op string) int {
	n := 0
	for _, c := range h.calls {
		if c["op"] == op {
			n++
		}
	}
	return n
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
