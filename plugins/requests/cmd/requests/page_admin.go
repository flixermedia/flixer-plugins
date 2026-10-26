package main

import (
	"fmt"
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

var kindInfo = map[string]struct{ purpose, detail, example string }{
	"radarr": {"Movies", "Radarr finds, downloads and organises movies. Approved movie requests are added to it.", "https://radarr.example.com"},
	"sonarr": {"TV series", "Sonarr does the same for TV series. Approved series and seasons are added to it.", "https://sonarr.example.com"},
	"lidarr": {"Music", "Lidarr manages music. Approved albums are added to it.", "https://lidarr.example.com"},
}

func addNav(kind string) nav { return nav{Query: map[string]string{"tab": "services", "add": kind}} }

func servicesContent(c *Ctx, q map[string]string) []node {
	if !c.isAdmin() {
		return []node{alert("Only administrators can manage servers.", "danger")}
	}
	if id := q["edit"]; id != "" && uuidPattern.MatchString(id) {
		return serviceEditContent(c, strings.ToLower(id))
	}
	if kind := q["add"]; contains(serviceKinds, kind) {
		return serviceAddContent(kind)
	}
	services := Round(c, []hostCall{dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0)},
		func(rs []hostResult) serviceListR1 { return serviceListR1{Services: rowsOf[serviceRow](rs[0])} }).Services

	if len(services) == 0 {
		cards := make([]node, 0, len(serviceKinds))
		for _, kind := range []string{"radarr", "sonarr", "lidarr"} {
			info := kindInfo[kind]
			to := addNav(kind)
			cards = append(cards, cardNode("Add "+kindLabel(kind), info.purpose, nil, nil, &to,
				[]node{textNode(info.detail, "muted")},
				[]node{link("Add "+kindLabel(kind), to)}))
		}
		return []node{
			section("Connect a server", "Requests are fulfilled by Radarr, Sonarr and Lidarr. Connect at least one to get started; you only need the kinds you want people to request.", nil,
				grid(240, cards),
				textNode("You will need the server's https address and its API key (in its Settings, General).", "muted")),
		}
	}

	cards := make([]node, 0, len(services))
	have := map[string]bool{}
	for _, s := range services {
		have[s.Kind] = true
		badges := []badgeSpec{{Text: kindLabel(s.Kind), Tone: "info"}}
		switch {
		case !s.complete():
			badges = append(badges, badgeSpec{Text: "Needs setup", Tone: "warning"})
		case s.Enabled:
			badges = append(badges, badgeSpec{Text: "Enabled", Tone: "success"})
		default:
			badges = append(badges, badgeSpec{Text: "Disabled", Tone: "default"})
		}
		if s.IsDefault {
			badges = append(badges, badgeSpec{Text: "Default", Tone: "default"})
		}
		id := map[string]any{"id": s.ID}
		edit := nav{Query: map[string]string{"tab": "services", "edit": s.ID}}
		body := []node{}
		if !s.complete() {
			body = append(body, textNode("Connected, but it is not used until you choose a root folder and quality profile.", "muted"))
		}
		cards = append(cards, cardNode(s.Name, s.URL, nil, badges, nil, body, []node{
			link("Edit", edit),
			button("Test", action{Op: "service-test", Body: id}, "quiet"),
			button("Delete", action{Op: "service-delete", Body: id, Reload: true, Confirm: "Delete " + s.Name + "? Existing requests are kept."}, "danger"),
		}))
	}
	adders := []node{}
	for _, kind := range []string{"radarr", "sonarr", "lidarr"} {
		adders = append(adders, link("Add "+kindLabel(kind), addNav(kind)))
	}
	out := []node{section("Servers", "Requests go to the default enabled server of each kind.", adders, grid(280, cards))}
	for _, kind := range []string{"radarr", "sonarr", "lidarr"} {
		if !have[kind] {
			out = append(out, alert(fmt.Sprintf("No %s server: %s requests are unavailable.", kindLabel(kind), strings.ToLower(kindInfo[kind].purpose)), "info"))
		}
	}
	return out
}

// serviceAddContent is the first step of connecting a server: it checks the address and key, then continues on the edit page.
func serviceAddContent(kind string) []node {
	info := kindInfo[kind]
	back := link("Back to servers", serversTab())
	addForm := form([]field{
		hidden("kind", kind),
		{Name: "name", Label: "Name", Type: "text", Value: kindLabel(kind), Required: true, Help: "How this server is called in Flixer."},
		{Name: "url", Label: "Address", Type: "text", Required: true, Placeholder: info.example, Help: "A public https:// address, without a path."},
		{Name: "apiKey", Label: "API key", Type: "password", Required: true, Help: "Found in " + kindLabel(kind) + " under Settings, General, Security."},
	}, "Test connection and continue", action{Op: "service-save"}, "primary", "column")
	return []node{
		back,
		section("Add "+kindLabel(kind), info.detail+" Flixer connects with the key first; if that works you choose the quality profile and root folder next.", nil, addForm),
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

	status := []node{}
	if !result.OK {
		status = append(status, alert("Could not load profiles from the server: "+result.Error+" You can still edit the settings by hand.", "warning"))
	} else {
		text := "Connection works"
		if result.Version != "" {
			text += ", " + kindLabel(s.Kind) + " " + result.Version
		}
		text += fmt.Sprintf(". Found %d root folders and %d quality profiles.", len(result.Roots), len(result.Quality))
		status = append(status, alert(text, "success"))
	}
	if !s.complete() {
		status = append(status, alert("Choose a root folder and a quality profile, turn the server on and save. Until then it receives no requests.", "info"))
	}

	fields := []field{
		hidden("id", s.ID), hidden("kind", s.Kind),
		{Name: "name", Label: "Name", Type: "text", Value: s.Name, Required: true},
		{Name: "url", Label: "Address", Type: "text", Value: s.URL, Required: true, Help: "Must be a public https:// address."},
		{Name: "apiKey", Label: "API key", Type: "password", HasValue: r1.HasKey, Help: "Leave empty to keep the saved key."},
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
		fields = append(fields, field{Name: "seasonFolders", Label: "Use season folders", Type: "switch", Value: s.SeasonFolders, Help: "Put each season of a series in its own folder."})
	}
	tags := make([]string, 0, len(s.Tags))
	for _, tag := range s.Tags {
		tags = append(tags, strconv.Itoa(tag))
	}
	fields = append(fields,
		field{Name: "tags", Label: "Tag ids", Type: "text", Value: strings.Join(tags, ","), Help: "Optional. Comma separated numbers of tags to put on everything Flixer adds."},
		field{Name: "enabled", Label: "Enabled", Type: "switch", Value: s.Enabled, Help: "Only enabled servers receive requests."},
		field{Name: "isDefault", Label: "Default " + s.Kind + " server", Type: "switch", Value: s.IsDefault, Help: "Requests go here unless an administrator picks another server of the same kind."},
	)
	testButton := button("Test connection", action{Op: "service-test", Body: map[string]any{"id": s.ID}}, "quiet")
	deleteButton := button("Delete", action{Op: "service-delete", Body: map[string]any{"id": s.ID}, Confirm: "Delete " + s.Name + "? Existing requests are kept."}, "danger")
	return []node{
		back,
		section(s.Name, kindInfo[s.Kind].detail, []node{testButton, deleteButton}, append(status, form(fields, "Save server", action{Op: "service-save"}, "primary", "column"))...),
	}
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
	out := []node{section("Users", "Leave a value on Default to use the global settings. A quota of 0 means unlimited. Administrators have no limits.", nil,
		tableNode([]column2{{"user", "User"}, {"limits", "Limits"}}, rows, "No users."))}
	if r1.Total > pageSize {
		out = append(out, pagerNode(page, pageSize, r1.Total))
	}
	return out
}
