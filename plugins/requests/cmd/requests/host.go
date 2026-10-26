package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Host limits of the plugin runtime: calls per round and rounds per invocation.
const (
	maxCallsPerRound = 8
	maxRounds        = 4
	continuationV    = 1
)

// hostCall is one request to the host. Its id is assigned by Round.
type hostCall map[string]any

type groupCount struct {
	Value any `json:"value"`
	Count int `json:"count"`
}

type hostUser struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"displayName"`
	Role        string  `json:"role"`
}

type hostResult struct {
	ID       string            `json:"id"`
	OK       bool              `json:"ok"`
	Error    string            `json:"error"`
	Status   int               `json:"status"`
	Body     string            `json:"body"`
	Rows     json.RawMessage   `json:"rows"`
	Count    int               `json:"count"`
	Groups   []groupCount      `json:"groups"`
	Total    int               `json:"total"`
	Users    []hostUser        `json:"users"`
	Matches  map[string]string `json:"matches"`
	Value    string            `json:"value"`
	Inserted int               `json:"inserted"`
	Updated  int               `json:"updated"`
	Deleted  int               `json:"deleted"`
}

type envelope struct {
	APIVersion int    `json:"apiVersion"`
	Method     string `json:"method"`
	Operation  string `json:"operation"`
	User       struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"user"`
	Request struct {
		Method string          `json:"method"`
		Body   json.RawMessage `json:"body"`
	} `json:"request"`
	Settings     map[string]any  `json:"settings"`
	Now          string          `json:"now"`
	Continuation json.RawMessage `json:"continuation"`
	HostResults  []hostResult    `json:"hostResults"`
}

type continuation struct {
	V      int               `json:"v"`
	Rounds []json.RawMessage `json:"rounds"`
}

// userError is a message that is safe and useful to show to the caller.
type userError struct{ msg string }

func (e *userError) Error() string { return e.msg }

func fail(format string, args ...any) error {
	return &userError{msg: fmt.Sprintf(format, args...)}
}

type pendingRound struct{ calls []hostCall }

type protocolError struct{ err error }

// Ctx drives the continuation protocol by re-running the operation from the start
// on every invocation. Each Round is answered from memoised, reduced results of
// earlier invocations, or from the host results of this one, or it suspends the
// operation so the host can run the calls.
type Ctx struct {
	env      *envelope
	memo     []json.RawMessage
	round    int
	consumed bool
	now      time.Time
	rounds   int
}

func newCtx(env *envelope) (*Ctx, error) {
	c := &Ctx{env: env, now: time.Now().UTC()}
	if env.Now != "" {
		parsed, err := time.Parse(time.RFC3339Nano, env.Now)
		if err != nil {
			return nil, errors.New("invalid now in envelope")
		}
		c.now = parsed.UTC()
	}
	if len(env.Continuation) > 0 && string(env.Continuation) != "null" {
		var state continuation
		if err := json.Unmarshal(env.Continuation, &state); err != nil || state.V != continuationV {
			return nil, errors.New("invalid continuation")
		}
		c.memo = state.Rounds
	}
	return c, nil
}

func (c *Ctx) role() string   { return c.env.User.Role }
func (c *Ctx) userID() string { return c.env.User.ID }
func (c *Ctx) isAdmin() bool  { return c.env.User.Role == "admin" }

// Round runs up to eight host calls together and returns what reduce makes of
// their results. reduce must be deterministic and its result must survive a JSON
// round trip, because it is stored in the continuation. An empty call list does
// not use a round.
func Round[T any](c *Ctx, calls []hostCall, reduce func(rs []hostResult) T) T {
	if len(calls) == 0 {
		return reduce(nil)
	}
	if len(calls) > maxCallsPerRound {
		panic(protocolError{fmt.Errorf("round has %d calls, the limit is %d", len(calls), maxCallsPerRound)})
	}
	index := c.round
	c.round++
	if index >= maxRounds {
		panic(protocolError{fmt.Errorf("operation needs more than %d rounds", maxRounds)})
	}
	var out T
	if index < len(c.memo) {
		if err := json.Unmarshal(c.memo[index], &out); err != nil {
			panic(protocolError{fmt.Errorf("corrupt continuation: %w", err)})
		}
		return out
	}
	if index == len(c.memo) && len(c.env.HostResults) > 0 && !c.consumed {
		c.consumed = true
		out = reduce(c.ordered(index, len(calls)))
		raw, err := json.Marshal(out)
		if err != nil {
			panic(protocolError{err})
		}
		c.memo = append(c.memo, raw)
		return out
	}
	prepared := make([]hostCall, len(calls))
	for i, call := range calls {
		copied := hostCall{}
		for key, value := range call {
			copied[key] = value
		}
		copied["id"] = fmt.Sprintf("%d.%d", index, i)
		prepared[i] = copied
	}
	panic(pendingRound{calls: prepared})
}

func (c *Ctx) ordered(round, count int) []hostResult {
	byID := map[string]hostResult{}
	for _, result := range c.env.HostResults {
		byID[result.ID] = result
	}
	out := make([]hostResult, count)
	for i := range out {
		id := fmt.Sprintf("%d.%d", round, i)
		if result, ok := byID[id]; ok {
			out[i] = result
		} else {
			out[i] = hostResult{ID: id, OK: false, Error: "missing host result"}
		}
	}
	return out
}

type resultOutput struct {
	Result any `json:"result"`
}

type callsOutput struct {
	HostCalls    []hostCall   `json:"hostCalls"`
	Continuation continuation `json:"continuation"`
}

// execute runs handler and turns its outcome into the stdout document.
func execute(env *envelope, handler func(c *Ctx) (any, error)) (output any, err error) {
	c, err := newCtx(env)
	if err != nil {
		return nil, err
	}
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case pendingRound:
			rounds := c.memo
			if rounds == nil {
				rounds = []json.RawMessage{}
			}
			output = callsOutput{HostCalls: recovered.calls, Continuation: continuation{V: continuationV, Rounds: rounds}}
			err = nil
		case protocolError:
			output, err = nil, recovered.err
		default:
			panic(recovered)
		}
	}()
	result, handlerErr := handler(c)
	if handlerErr != nil {
		var user *userError
		if errors.As(handlerErr, &user) {
			return resultOutput{Result: map[string]any{"error": user.msg}}, nil
		}
		return nil, handlerErr
	}
	return resultOutput{Result: result}, nil
}

// Host call builders.

func cond(col, cmp string, value any) map[string]any {
	return map[string]any{"col": col, "cmp": cmp, "value": value}
}

func condNull(col, cmp string) map[string]any {
	return map[string]any{"col": col, "cmp": cmp}
}

type order struct {
	Col string `json:"col"`
	Dir string `json:"dir"`
}

func dbFind(table string, where []map[string]any, orderBy []order, limit, offset int) hostCall {
	call := hostCall{"op": "db.find", "table": table, "limit": limit}
	if len(where) > 0 {
		call["where"] = where
	}
	if len(orderBy) > 0 {
		call["orderBy"] = orderBy
	}
	if offset > 0 {
		call["offset"] = offset
	}
	return call
}

func dbCount(table string, where []map[string]any, groupBy string) hostCall {
	call := hostCall{"op": "db.count", "table": table}
	if len(where) > 0 {
		call["where"] = where
	}
	if groupBy != "" {
		call["groupBy"] = groupBy
	}
	return call
}

func dbInsert(table string, row map[string]any) hostCall {
	return hostCall{"op": "db.insert", "table": table, "rows": []map[string]any{row}}
}

func dbUpdate(table string, where []map[string]any, set map[string]any) hostCall {
	return hostCall{"op": "db.update", "table": table, "where": where, "set": set}
}

func dbDelete(table string, where []map[string]any) hostCall {
	return hostCall{"op": "db.delete", "table": table, "where": where}
}

func secretGet(name string) hostCall { return hostCall{"op": "secret.get", "name": name} }

func secretSet(name, value string) hostCall {
	return hostCall{"op": "secret.set", "name": name, "value": value}
}

func usersGet(ids []string) hostCall { return hostCall{"op": "users.get", "ids": ids} }

func usersList(limit, offset int) hostCall {
	return hostCall{"op": "users.list", "limit": limit, "offset": offset}
}

type libraryCard struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Source string `json:"source"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Year   *int   `json:"year"`
}

func libraryMatch(cards []libraryCard) hostCall {
	return hostCall{"op": "library.match", "cards": cards}
}

func notifyUser(userID, title, message, url string) hostCall {
	call := hostCall{"op": "notify.send", "to": "user", "userId": userID, "title": title, "message": truncate(message, 500)}
	if url != "" {
		call["url"] = url
	}
	return call
}

func notifyAdmins(title, message, url string) hostCall {
	call := hostCall{"op": "notify.send", "to": "admins", "title": title, "message": truncate(message, 500)}
	if url != "" {
		call["url"] = url
	}
	return call
}

func httpRequest(method, url string, headers map[string]string, body string) hostCall {
	call := hostCall{"op": "http.request", "method": method, "url": url}
	if len(headers) > 0 {
		call["headers"] = headers
	}
	if body != "" {
		call["body"] = body
	}
	return call
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max]))
}

func rowsOf[T any](result hostResult) []T {
	if !result.OK || len(result.Rows) == 0 {
		return nil
	}
	var rows []T
	if err := json.Unmarshal(result.Rows, &rows); err != nil {
		return nil
	}
	return rows
}

func isoTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
