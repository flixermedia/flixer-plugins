package main

import (
	"strconv"
	"strings"
)

type serviceListR1 struct {
	Services []serviceRow `json:"services"`
}

type editR1 struct {
	Service *serviceRow `json:"service"`
	Key     string      `json:"-"` // only needed in the invocation that sends the next round
	HasKey  bool        `json:"hasKey"`
}

func servicesContent(c *Ctx, q map[string]string) []node {
	if !c.isAdmin() {
		return []node{alert("Only administrators can manage servers.", "danger")}
	}
	if id := q["edit"]; id != "" && uuidPattern.MatchString(id) {
		return serviceEditContent(c, strings.ToLower(id))
	}
	services := Round(c, []hostCall{dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0)},
		func(rs []hostResult) serviceListR1 { return serviceListR1{Services: rowsOf[serviceRow](rs[0])} }).Services

	rows := make([]node, 0, len(services))
	for _, s := range services {
		badges := []node{badge(strings.ToUpper(s.Kind[:1])+s.Kind[1:], "info")}
		switch {
		case !s.complete():
			badges = append(badges, badge("Needs setup", "warning"))
		case s.Enabled:
			badges = append(badges, badge("Enabled", "success"))
		default:
			badges = append(badges, badge("Disabled", "default"))
		}
		if s.IsDefault {
			badges = append(badges, badge("Default", "default"))
		}
		id := map[string]any{"id": s.ID}
		rows = append(rows, tableRow(s.ID, map[string]any{
			"name":   s.Name,
			"url":    s.URL,
			"status": row(1, badges...),
			"actions": row(2,
				link("Edit", nav{Query: map[string]string{"tab": "services", "edit": s.ID}}),
				button("Test", action{Op: "service-test", Body: id}, "quiet"),
				button("Delete", action{Op: "service-delete", Body: id, Reload: true, Confirm: "Delete this server? Existing requests are kept."}, "danger"),
			),
		}))
	}
	addForm := form([]field{
		{Name: "kind", Label: "Type", Type: "select", Value: "sonarr", Required: true, Options: []option{
			{"sonarr", "Sonarr (series)"}, {"radarr", "Radarr (movies)"}, {"lidarr", "Lidarr (music)"},
		}},
		{Name: "name", Label: "Name", Type: "text", Required: true},
		{Name: "url", Label: "Address", Type: "text", Required: true, Placeholder: "https://sonarr.example.com", Help: "Must be a public https:// address."},
		{Name: "apiKey", Label: "API key", Type: "password", Required: true},
	}, "Connect", action{Op: "service-save"}, "primary", "column")
	return []node{
		heading("Servers", 2),
		tableNode([]column2{{"name", "Name"}, {"url", "Address"}, {"status", "Status"}, {"actions", ""}}, rows, "No servers yet."),
		heading("Add a server", 3),
		textNode("Requests are sent to the default enabled server of each kind. After connecting you choose the quality profile and root folder.", "muted"),
		addForm,
	}
}

func profileOptions(list []profile, current *int, optional bool) []option {
	options := []option{}
	if optional {
		options = append(options, option{Value: "", Label: "None"})
	}
	found := current == nil
	for _, p := range list {
		options = append(options, option{Value: strconv.Itoa(p.ID), Label: p.Name})
		found = found || p.ID == *current
	}
	if !found {
		options = append(options, option{Value: strconv.Itoa(*current), Label: "Profile " + strconv.Itoa(*current)})
	}
	return options
}

func intText(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func serviceEditContent(c *Ctx, id string) []node {
	back := link("Back to servers", nav{Query: map[string]string{"tab": "services"}})
	r1 := Round(c, []hostCall{
		dbFind(tableServices, []map[string]any{cond("id", "eq", id)}, nil, 1, 0),
		secretGet(secretName(id)),
	}, func(rs []hostResult) editR1 {
		out := editR1{}
		if rows := rowsOf[serviceRow](rs[0]); len(rows) > 0 {
			out.Service = &rows[0]
		}
		if rs[1].OK {
			out.Key, out.HasKey = rs[1].Value, rs[1].Value != ""
		}
		return out
	})
	if r1.Service == nil {
		return []node{back, alert("Server not found.", "danger")}
	}
	s := *r1.Service
	conn := arrConn{Kind: s.Kind, URL: s.URL, Key: r1.Key}
	var result testResult
	if r1.HasKey {
		result = Round(c, conn.testCalls(), conn.parseTest)
	} else {
		result = testResult{Error: "The API key for this server is missing. Enter it again below."}
	}

	out := []node{back, heading(s.Name, 2)}
	if !result.OK {
		out = append(out, alert("Could not load profiles from the server: "+result.Error+" You can still edit the settings by hand.", "warning"))
	} else {
		out = append(out, alert("Connected"+map[bool]string{true: ", version " + result.Version, false: ""}[result.Version != ""]+".", "success"))
	}

	fields := []field{
		hidden("id", s.ID), hidden("kind", s.Kind),
		{Name: "name", Label: "Name", Type: "text", Value: s.Name, Required: true},
		{Name: "url", Label: "Address", Type: "text", Value: s.URL, Required: true, Help: "Must be a public https:// address."},
		{Name: "apiKey", Label: "API key", Type: "password", Help: "Leave empty to keep the saved key."},
	}
	if result.OK {
		roots := []option{}
		found := false
		for _, r := range result.Roots {
			roots = append(roots, option{Value: r.Path, Label: r.Path})
			found = found || r.Path == s.RootFolder
		}
		if s.RootFolder != "" && !found {
			roots = append(roots, option{Value: s.RootFolder, Label: s.RootFolder})
		}
		quality := s.QualityProfileID
		var current *int
		if quality > 0 {
			current = &quality
		}
		fields = append(fields,
			field{Name: "rootFolder", Label: "Root folder", Type: "select", Value: s.RootFolder, Required: true, Options: roots},
			field{Name: "qualityProfileId", Label: "Quality profile", Type: "select", Value: intText(current), Required: true, Options: profileOptions(result.Quality, current, false)},
		)
		switch s.Kind {
		case "lidarr":
			fields = append(fields, field{Name: "metadataProfileId", Label: "Metadata profile", Type: "select", Value: intText(s.MetadataProfileID), Required: true, Options: profileOptions(result.Metadata, s.MetadataProfileID, false)})
		case "sonarr":
			if len(result.Language) > 0 || s.LanguageProfileID != nil {
				fields = append(fields, field{Name: "languageProfileId", Label: "Language profile", Type: "select", Value: intText(s.LanguageProfileID), Options: profileOptions(result.Language, s.LanguageProfileID, true)})
			}
		}
	} else {
		fields = append(fields,
			field{Name: "rootFolder", Label: "Root folder", Type: "text", Value: s.RootFolder},
			field{Name: "qualityProfileId", Label: "Quality profile id", Type: "number", Value: intText(&s.QualityProfileID), Min: ptr(1)},
		)
		switch s.Kind {
		case "lidarr":
			fields = append(fields, field{Name: "metadataProfileId", Label: "Metadata profile id", Type: "number", Value: intText(s.MetadataProfileID), Min: ptr(1)})
		case "sonarr":
			fields = append(fields, field{Name: "languageProfileId", Label: "Language profile id", Type: "number", Value: intText(s.LanguageProfileID), Min: ptr(1)})
		}
	}
	switch s.Kind {
	case "radarr":
		available := "released"
		if s.MinimumAvailability != nil {
			available = *s.MinimumAvailability
		}
		fields = append(fields, field{Name: "minimumAvailability", Label: "Minimum availability", Type: "select", Value: available, Options: []option{
			{"announced", "Announced"}, {"inCinemas", "In cinemas"}, {"released", "Released"},
		}})
	case "sonarr":
		fields = append(fields, field{Name: "seasonFolders", Label: "Use season folders", Type: "checkbox", Value: s.SeasonFolders})
	}
	tags := make([]string, 0, len(s.Tags))
	for _, tag := range s.Tags {
		tags = append(tags, strconv.Itoa(tag))
	}
	fields = append(fields,
		field{Name: "tags", Label: "Tag ids", Type: "text", Value: strings.Join(tags, ","), Help: "Comma separated numbers (optional)."},
		field{Name: "enabled", Label: "Enabled", Type: "checkbox", Value: s.Enabled},
		field{Name: "isDefault", Label: "Default server for its type", Type: "checkbox", Value: s.IsDefault},
	)
	out = append(out, form(fields, "Save", action{Op: "service-save"}, "primary", "column"),
		button("Test connection", action{Op: "service-test", Body: map[string]any{"id": s.ID}}, "quiet"))
	return out
}

func ptr[T any](v T) *T { return &v }

type usersR1 struct {
	Users []hostUser `json:"users"`
	Total int        `json:"total"`
}

type limitsR2 struct {
	Limits []limitRow `json:"limits"`
}

func usersContent(c *Ctx, q map[string]string) []node {
	if !c.isAdmin() {
		return []node{alert("Only administrators can manage limits.", "danger")}
	}
	page := queryPage(q)
	r1 := Round(c, []hostCall{usersList(pageSize, (page-1)*pageSize)}, func(rs []hostResult) usersR1 {
		return usersR1{Users: rs[0].Users, Total: rs[0].Total}
	})
	members := []hostUser{}
	ids := []string{}
	for _, user := range r1.Users {
		if user.Role != "admin" {
			members = append(members, user)
			ids = append(ids, user.ID)
		}
	}
	var calls []hostCall
	if len(ids) > 0 {
		calls = append(calls, dbFind(tableLimits, []map[string]any{cond("user_id", "in", ids)}, nil, 200, 0))
	}
	limits := map[string]limitRow{}
	for _, l := range Round(c, calls, func(rs []hostResult) limitsR2 { return limitsR2{Limits: rowsOf[limitRow](at(rs, 0))} }).Limits {
		limits[l.UserID] = l
	}

	tri := func(value *bool) string {
		switch {
		case value == nil:
			return "inherit"
		case *value:
			return "allow"
		}
		return "deny"
	}
	triOptions := []option{{"inherit", "Default"}, {"allow", "Allow"}, {"deny", "Deny"}}
	rows := make([]node, 0, len(members))
	for _, user := range members {
		l := limits[user.ID]
		rows = append(rows, tableRow(user.ID, map[string]any{
			"user": displayName(&user),
			"limits": form([]field{
				hidden("userId", user.ID),
				{Name: "canRequest", Label: "Can request", Type: "select", Value: tri(l.CanRequest), Options: triOptions},
				{Name: "autoApprove", Label: "Auto-approve", Type: "select", Value: tri(l.AutoApprove), Options: triOptions},
				{Name: "quotaLimit", Label: "Quota", Type: "number", Value: intText(l.QuotaLimit), Placeholder: "Default", Min: ptr(0), Max: ptr(maxQuotaLimit)},
				{Name: "quotaDays", Label: "Days", Type: "number", Value: intText(l.QuotaDays), Placeholder: "Default", Min: ptr(1), Max: ptr(maxQuotaDays)},
			}, "Save", action{Op: "user-limit-save", Reload: true}, "quiet", "inline"),
		}))
	}
	out := []node{
		heading("Users", 2),
		textNode("Leave a value on Default to use the global settings. A quota of 0 means unlimited. Administrators have no limits.", "muted"),
		tableNode([]column2{{"user", "User"}, {"limits", "Limits"}}, rows, "No users."),
	}
	if r1.Total > pageSize {
		out = append(out, pagerNode(page, pageSize, r1.Total))
	}
	return out
}
