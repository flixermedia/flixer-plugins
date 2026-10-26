package main

import (
	"strconv"
	"strings"
)

type servicesR1 struct {
	Services []serviceRow `json:"services"`
	Status   string       `json:"status"`
	Error    string       `json:"error"`
}

func parseTags(a args) ([]int, error) {
	out := []int{}
	switch v := a["tags"].(type) {
	case nil:
	case []any:
		for _, entry := range v {
			n, ok := entry.(float64)
			if !ok || n != float64(int(n)) {
				return nil, fail("tags must be whole numbers.")
			}
			out = append(out, int(n))
		}
	case string:
		for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fail("tags must be whole numbers separated by commas.")
			}
			out = append(out, n)
		}
	default:
		return nil, fail("tags must be whole numbers.")
	}
	if len(out) > 50 {
		return nil, fail("Too many tags.")
	}
	return out, nil
}

func successToast(text string) actionResult {
	return actionResult{Toast: &toast{Text: text}, Reload: true}
}

// opServiceSave creates or updates a Sonarr, Radarr or Lidarr server. Creating one first connects to it with the
// given key, stores the key as a secret and leaves the server as a draft until its profile and root folder are chosen.
func opServiceSave(c *Ctx) (any, error) {
	if err := requireAdmin(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	id, err := a.uuid("id")
	if err != nil {
		return nil, err
	}
	kind, err := a.str("kind", 8)
	if err != nil {
		return nil, err
	}
	if !contains(serviceKinds, kind) {
		return nil, fail("Unknown server type.")
	}
	name, err := a.str("name", 100)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, fail("Enter a name for the server.")
	}
	serverURL, err := normalizeArrURL(mustStr(a.str("url", 500)))
	if err != nil {
		return nil, err
	}
	apiKey, err := a.str("apiKey", 200)
	if err != nil {
		return nil, err
	}
	root, err := a.str("rootFolder", 500)
	if err != nil {
		return nil, err
	}
	quality, err := a.optInt("qualityProfileId")
	if err != nil {
		return nil, err
	}
	metadata, err := a.optInt("metadataProfileId")
	if err != nil {
		return nil, err
	}
	language, err := a.optInt("languageProfileId")
	if err != nil {
		return nil, err
	}
	minAvail, err := a.str("minimumAvailability", 16)
	if err != nil {
		return nil, err
	}
	enabled, err := a.optBool("enabled")
	if err != nil {
		return nil, err
	}
	isDefault, err := a.optBool("isDefault")
	if err != nil {
		return nil, err
	}
	seasonFolders, err := a.optBool("seasonFolders")
	if err != nil {
		return nil, err
	}
	tags, err := parseTags(a)
	if err != nil {
		return nil, err
	}

	creating := id == ""
	draft := creating && root == "" && quality == nil
	if creating && apiKey == "" {
		return nil, fail("An API key is required.")
	}
	if !draft {
		if root == "" {
			return nil, fail("Choose a root folder.")
		}
		if quality == nil || *quality < 1 {
			return nil, fail("Choose a quality profile.")
		}
		if kind == "lidarr" && metadata == nil {
			return nil, fail("Lidarr needs a metadata profile.")
		}
		if minAvail != "" && !contains(minAvailable, minAvail) {
			return nil, fail("Unknown minimum availability.")
		}
	}

	calls := []hostCall{dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0)}
	if creating {
		calls = append(calls, arrConn{Kind: kind, URL: serverURL, Key: apiKey}.call("GET", "/system/status", nil))
	}
	r1 := Round(c, calls, func(rs []hostResult) servicesR1 {
		out := servicesR1{Services: rowsOf[serviceRow](rs[0])}
		if creating {
			if _, problem := decodeArr(kind, at(rs, 1)); problem != "" {
				out.Error = problem
			}
		}
		return out
	})
	if r1.Error != "" {
		return nil, fail("%s", r1.Error)
	}

	var existing *serviceRow
	if creating {
		id = newID()
	} else {
		for i := range r1.Services {
			if r1.Services[i].ID == id {
				existing = &r1.Services[i]
			}
		}
		if existing == nil {
			return nil, fail("Server not found.")
		}
	}
	siblings := 0
	for _, service := range r1.Services {
		if service.Kind == kind && service.ID != id {
			siblings++
		}
	}

	row := serviceRow{
		ID: id, Kind: kind, Name: name, URL: serverURL, RootFolder: root, SeasonFolders: seasonFolders == nil || *seasonFolders,
		Tags: tags, CreatedAt: isoTime(c.now),
	}
	if quality != nil {
		row.QualityProfileID = *quality
	}
	if kind == "lidarr" {
		row.MetadataProfileID = metadata
	}
	if kind == "sonarr" {
		row.LanguageProfileID = language
	}
	if kind == "radarr" && !draft {
		if minAvail == "" {
			minAvail = "released"
		}
		row.MinimumAvailability = &minAvail
	}
	switch {
	case draft:
		row.Enabled = false
	case enabled != nil:
		row.Enabled = *enabled
	default:
		row.Enabled = existing == nil || existing.Enabled
	}
	row.IsDefault = siblings == 0 || (isDefault != nil && *isDefault)
	if isDefault == nil && existing != nil {
		row.IsDefault = existing.IsDefault || siblings == 0
	}

	var final []hostCall
	if apiKey != "" {
		final = append(final, secretSet(secretName(id), apiKey))
	}
	values := serviceValues(row)
	if creating {
		values["id"] = id
		values["created_at"] = row.CreatedAt
		final = append(final, dbInsert(tableServices, values))
	} else {
		final = append(final, dbUpdate(tableServices, []map[string]any{cond("id", "eq", id)}, values))
	}
	if row.IsDefault && siblings > 0 {
		final = append(final, dbUpdate(tableServices, []map[string]any{
			cond("kind", "eq", kind), cond("id", "ne", id), cond("is_default", "eq", true),
		}, map[string]any{"is_default": false}))
	}
	Round(c, final, func([]hostResult) struct{} { return struct{}{} })

	if draft {
		return actionResult{
			Toast:    &toast{Text: "Connected. Now choose the quality profile and root folder."},
			Navigate: &nav{Query: map[string]string{"tab": "services", "edit": id}},
		}, nil
	}
	return actionResult{Toast: &toast{Text: "Server saved."}, Navigate: &nav{Query: map[string]string{"tab": "services"}}}, nil
}

func mustStr(value string, err error) string {
	if err != nil {
		return ""
	}
	return value
}

func serviceValues(row serviceRow) map[string]any {
	tags := row.Tags
	if tags == nil {
		tags = []int{}
	}
	return map[string]any{
		"kind": row.Kind, "name": row.Name, "url": row.URL, "is_default": row.IsDefault, "enabled": row.Enabled,
		"root_folder": row.RootFolder, "quality_profile_id": row.QualityProfileID, "metadata_profile_id": row.MetadataProfileID,
		"language_profile_id": row.LanguageProfileID, "minimum_availability": row.MinimumAvailability,
		"season_folders": row.SeasonFolders, "tags": tags,
	}
}

func opServiceDelete(c *Ctx) (any, error) {
	if err := requireAdmin(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	id, err := a.uuid("id")
	if err != nil || id == "" {
		return nil, fail("Invalid id.")
	}
	Round(c, []hostCall{
		dbDelete(tableServices, []map[string]any{cond("id", "eq", id)}),
		secretSet(secretName(id), ""),
	}, func([]hostResult) struct{} { return struct{}{} })
	return actionResult{Toast: &toast{Text: "Server removed."}, Navigate: &nav{Query: map[string]string{"tab": "services"}}}, nil
}

type testR1 struct {
	Service *serviceRow `json:"service"`
	Key     string      `json:"-"` // only needed in the invocation that sends the next round
	HasKey  bool        `json:"hasKey"`
}

// opServiceTest connects to a saved server with its stored key and reports its version.
func opServiceTest(c *Ctx) (any, error) {
	if err := requireAdmin(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	id, err := a.uuid("id")
	if err != nil || id == "" {
		return nil, fail("Invalid id.")
	}
	r1 := Round(c, []hostCall{
		dbFind(tableServices, []map[string]any{cond("id", "eq", id)}, nil, 1, 0),
		secretGet(secretName(id)),
	}, func(rs []hostResult) testR1 {
		out := testR1{}
		if rows := rowsOf[serviceRow](rs[0]); len(rows) > 0 {
			out.Service = &rows[0]
		}
		if rs[1].OK {
			out.Key, out.HasKey = rs[1].Value, rs[1].Value != ""
		}
		return out
	})
	if r1.Service == nil {
		return nil, fail("Server not found.")
	}
	if !r1.HasKey {
		return nil, fail("The API key for this server is missing. Edit the server and enter it again.")
	}
	conn := arrConn{Kind: r1.Service.Kind, URL: r1.Service.URL, Key: r1.Key}
	result := Round(c, conn.testCalls(), conn.parseTest)
	if !result.OK {
		return actionResult{Toast: &toast{Text: result.Error, Tone: "danger"}}, nil
	}
	text := "Connected"
	if result.Version != "" {
		text += ", version " + result.Version
	}
	return actionResult{Toast: &toast{Text: text + "."}}, nil
}

type limitR1 struct {
	User   *hostUser `json:"user"`
	Exists bool      `json:"exists"`
}

// opUserLimitSave stores a user's overrides of the global request settings; empty values mean "use the default".
func opUserLimitSave(c *Ctx) (any, error) {
	if err := requireAdmin(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	userID, err := a.uuid("userId")
	if err != nil || userID == "" {
		return nil, fail("Invalid userId.")
	}
	canRequest, err := a.optBool("canRequest")
	if err != nil {
		return nil, err
	}
	autoApprove, err := a.optBool("autoApprove")
	if err != nil {
		return nil, err
	}
	quotaLimit, err := a.optInt("quotaLimit")
	if err != nil {
		return nil, err
	}
	quotaDays, err := a.optInt("quotaDays")
	if err != nil {
		return nil, err
	}
	if quotaLimit != nil && (*quotaLimit < 0 || *quotaLimit > maxQuotaLimit) {
		return nil, fail("The quota must be from 0 (unlimited) to %d.", maxQuotaLimit)
	}
	if quotaDays != nil && (*quotaDays < 1 || *quotaDays > maxQuotaDays) {
		return nil, fail("The quota period must be from 1 to %d days.", maxQuotaDays)
	}
	r1 := Round(c, []hostCall{
		usersGet([]string{userID}),
		dbCount(tableLimits, []map[string]any{cond("user_id", "eq", userID)}, ""),
	}, func(rs []hostResult) limitR1 {
		out := limitR1{Exists: rs[1].Count > 0}
		if len(rs[0].Users) > 0 {
			out.User = &rs[0].Users[0]
		}
		return out
	})
	if r1.User == nil {
		return nil, fail("User not found.")
	}
	values := map[string]any{"can_request": canRequest, "auto_approve": autoApprove, "quota_limit": quotaLimit, "quota_days": quotaDays}
	var call hostCall
	switch {
	case canRequest == nil && autoApprove == nil && quotaLimit == nil && quotaDays == nil:
		call = dbDelete(tableLimits, []map[string]any{cond("user_id", "eq", userID)})
	case r1.Exists:
		call = dbUpdate(tableLimits, []map[string]any{cond("user_id", "eq", userID)}, values)
	default:
		values["user_id"] = userID
		call = dbInsert(tableLimits, values)
	}
	Round(c, []hostCall{call}, func([]hostResult) struct{} { return struct{}{} })
	return successToast("Saved."), nil
}
