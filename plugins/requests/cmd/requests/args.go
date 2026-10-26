package main

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// args is a decoded operation body with lenient, validating accessors (form posts send strings for selects and hidden fields).
type args map[string]any

func parseArgs(raw json.RawMessage) (args, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return args{}, nil
	}
	var out args
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return nil, fail("The request body must be a JSON object.")
	}
	return out, nil
}

func (a args) str(key string, max int) (string, error) {
	switch v := a[key].(type) {
	case nil:
		return "", nil
	case string:
		value := strings.TrimSpace(v)
		if len([]rune(value)) > max {
			return "", fail("%s is too long.", key)
		}
		return value, nil
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		}
	}
	return "", fail("%s must be text.", key)
}

// optInt reads a whole number; an empty string or a missing key is "not given".
func (a args) optInt(key string) (*int, error) {
	switch v := a[key].(type) {
	case nil:
		return nil, nil
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e9 {
			n := int(v)
			return &n, nil
		}
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return &n, nil
		}
	}
	return nil, fail("%s must be a whole number.", key)
}

// optBool reads a boolean; "", "inherit" and a missing key are "not given".
func (a args) optBool(key string) (*bool, error) {
	switch v := a[key].(type) {
	case nil:
		return nil, nil
	case bool:
		return &v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "inherit":
			return nil, nil
		case "true", "yes", "allow", "on":
			t := true
			return &t, nil
		case "false", "no", "deny", "off":
			f := false
			return &f, nil
		}
	}
	return nil, fail("%s must be true or false.", key)
}

func (a args) uuid(key string) (string, error) {
	value, err := a.str(key, 36)
	if err != nil {
		return "", err
	}
	if value != "" && !uuidPattern.MatchString(value) {
		return "", fail("Invalid %s.", key)
	}
	return strings.ToLower(value), nil
}

func (a args) mediaType() (string, error) {
	value, err := a.str("type", 8)
	if err != nil {
		return "", err
	}
	if !contains(mediaTypes, value) {
		return "", fail("type must be movie, tv or music.")
	}
	return value, nil
}

func (a args) externalID() (string, error) {
	value, err := a.str("externalId", 12)
	if err != nil {
		return "", err
	}
	if !numericID.MatchString(value) {
		return "", fail("Invalid id.")
	}
	return value, nil
}

// seasons reads the chosen TV seasons from a seasons array or from season_<n> checkboxes. given is false when neither is present.
func (a args) seasons() (list []int, given bool, err error) {
	seen := map[int]bool{}
	add := func(n int) error {
		if n < 1 || n > 1000 {
			return fail("Invalid season.")
		}
		if !seen[n] {
			seen[n] = true
			list = append(list, n)
		}
		return nil
	}
	if raw, ok := a["seasons"]; ok && raw != nil {
		values, isList := raw.([]any)
		if !isList || len(values) > 200 {
			return nil, false, fail("seasons must be a list of season numbers.")
		}
		given = true
		for _, value := range values {
			n, isNumber := value.(float64)
			if !isNumber || n != math.Trunc(n) {
				return nil, false, fail("seasons must be a list of season numbers.")
			}
			if err := add(int(n)); err != nil {
				return nil, false, err
			}
		}
	}
	for key, value := range a {
		suffix, isSeason := strings.CutPrefix(key, "season_")
		if !isSeason {
			continue
		}
		n, convErr := strconv.Atoi(suffix)
		if convErr != nil {
			continue
		}
		given = true
		if checked, _ := value.(bool); checked {
			if err := add(n); err != nil {
				return nil, false, err
			}
		}
	}
	sortInts(list)
	return list, given, nil
}

func sortInts(list []int) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func requireMember(c *Ctx) error {
	if c.userID() == "" || (c.role() != "user" && c.role() != "admin") {
		return fail("You must be signed in.")
	}
	return nil
}

func requireAdmin(c *Ctx) error {
	if c.role() != "admin" || c.userID() == "" {
		return fail("Only administrators can do this.")
	}
	return nil
}

type toast struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

type actionResult struct {
	Toast    *toast `json:"toast,omitempty"`
	Navigate *nav   `json:"navigate,omitempty"`
	Reload   bool   `json:"reload,omitempty"`
}

func done(text, tone string) actionResult {
	return actionResult{Toast: &toast{Text: text, Tone: tone}, Reload: true}
}

func displayName(user *hostUser) string {
	if user == nil {
		return "Someone"
	}
	if user.DisplayName != nil && strings.TrimSpace(*user.DisplayName) != "" {
		return *user.DisplayName
	}
	return user.Username
}

func quotaCutoff(c *Ctx, days int) string {
	return isoTime(c.now.Add(-time.Duration(days) * 24 * time.Hour))
}

func quotaCall(c *Ctx, userID string, days int) hostCall {
	return dbCount(tableRequests, []map[string]any{
		cond("user_id", "eq", userID),
		cond("status", "ne", "declined"),
		cond("created_at", "gt", quotaCutoff(c, days)),
	}, "")
}
