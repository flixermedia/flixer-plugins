package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	pageSize = 10
	cardCap  = 18
)

var pageParams = []string{"q", "type", "status", "page", "edit", "view", "id", "from"}

type pageBody struct {
	Page  string            `json:"page"`
	Query map[string]string `json:"query"`
}

// opPage builds the whole page for the current URL query.
func opPage(c *Ctx) (any, error) {
	if err := requireMember(c); err != nil {
		return nil, err
	}
	var body pageBody
	if raw := c.env.Request.Body; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fail("The request body must be a JSON object.")
		}
	}
	if body.Page != "" && body.Page != "requests" {
		return nil, fail("Unknown page.")
	}
	if body.Query == nil {
		body.Query = map[string]string{}
	}
	cfg := loadSettings(c)
	var view node
	if body.Query["view"] == "details" {
		view = detailsView(c, cfg, body.Query)
	} else {
		view = listView(c, cfg, body.Query)
	}
	return map[string]any{"title": "Requests", "view": view}, nil
}

func queryPage(q map[string]string) int {
	n, err := strconv.Atoi(q["page"])
	if err != nil || n < 1 || n > 100000 {
		return 1
	}
	return n
}

func listView(c *Ctx, cfg settings, q map[string]string) node {
	admin := c.isAdmin()
	tabs := []tabSpec{{ID: "discover", Label: "Discover"}, {ID: "search", Label: "Search"}, {ID: "mine", Label: "My requests"}}
	if admin {
		tabs = []tabSpec{{ID: "settings", Label: "Settings"}, {ID: "services", Label: "Servers"}, {ID: "users", Label: "Users"}, {ID: "all", Label: "Requests queue"}}
		tabs = append(tabs, tabSpec{ID: "discover", Label: "Discover"}, tabSpec{ID: "search", Label: "Search"}, tabSpec{ID: "mine", Label: "My requests"})
	}
	active := q["tab"]
	found := false
	for _, tab := range tabs {
		found = found || tab.ID == active
	}
	if !found {
		active = tabs[0].ID
	}
	var content []node
	switch active {
	case "settings":
		content = settingsContent(c, cfg)
	case "discover":
		content = discoverContent(c, cfg)
	case "search":
		content = searchContent(c, cfg, q)
	case "mine":
		content = requestsContent(c, cfg, q, false)
	case "all":
		content = []node{section("Requests queue", "Approve, decline or retry what people asked for.", nil, requestsContent(c, cfg, q, true)...)}
	case "services":
		content = servicesContent(c, q)
	case "users":
		content = usersContent(c, q)
	}
	for i := range tabs {
		if tabs[i].ID == active {
			tabs[i].Children = content
		} else {
			tabs[i].Children = []node{}
		}
	}
	return column(3, tabsNode(tabs, pageParams))
}

// Statuses of cards: the library and the requests table.

type reqRef struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
	Seasons    []int  `json:"seasons"`
}

func refOf(row requestRow) reqRef {
	return reqRef{ID: row.ID, Type: row.Type, ExternalID: row.ExternalID, Status: row.Status, Seasons: row.Seasons}
}

type statusData struct {
	Matches map[string]string `json:"matches"`
	Refs    []reqRef          `json:"refs"`
}

func cardKey(mediaType, id string) string { return mediaType + ":" + id }

// statusCalls looks up the requests for the cards and, for movies and albums, their library matches.
func statusCalls(cards []card) []hostCall {
	if len(cards) == 0 {
		return nil
	}
	ids := []string{}
	seen := map[string]bool{}
	var library []libraryCard
	for _, cd := range cards {
		if !seen[cd.ID] {
			seen[cd.ID] = true
			ids = append(ids, cd.ID)
		}
		if cd.Type != "tv" && len(library) < 120 {
			library = append(library, libraryCard{Key: cardKey(cd.Type, cd.ID), Type: cd.Type, Source: cd.Source, ID: cd.ID, Title: cd.Title, Year: yearPtr(cd.Year)})
		}
	}
	calls := []hostCall{dbFind(tableRequests, []map[string]any{
		cond("external_id", "in", ids), cond("status", "in", []string{"pending", "approved", "available"}),
	}, nil, 200, 0)}
	for start := 0; start < len(library); start += 60 {
		calls = append(calls, libraryMatch(library[start:min(start+60, len(library))]))
	}
	return calls
}

func statusParse(rs []hostResult) statusData {
	out := statusData{Matches: map[string]string{}, Refs: []reqRef{}}
	if len(rs) == 0 {
		return out
	}
	for _, row := range rowsOf[requestRow](rs[0]) {
		out.Refs = append(out.Refs, refOf(row))
	}
	for _, r := range rs[1:] {
		if r.OK {
			for key, value := range r.Matches {
				out.Matches[key] = value
			}
		}
	}
	return out
}

var statusRank = map[string]int{"pending": 1, "approved": 2, "available": 3}

func (d statusData) statusOf(mediaType, id string) string {
	if _, ok := d.Matches[cardKey(mediaType, id)]; ok {
		return "available"
	}
	best := ""
	for _, ref := range d.Refs {
		if ref.Type == mediaType && ref.ExternalID == id && statusRank[ref.Status] > statusRank[best] {
			best = ref.Status
		}
	}
	return best
}

func cardAspect(mediaType string) string {
	if mediaType == "music" {
		return "square"
	}
	return "poster"
}

func detailsNav(mediaType, id, from, q string) nav {
	query := map[string]string{"view": "details", "type": mediaType, "id": id, "from": from}
	if q != "" {
		query["q"] = q
	}
	return nav{Query: query}
}

func cardView(cd card, status, from, q string) node {
	var badges []badgeSpec
	if label, ok := cardStatusLabels[status]; ok {
		badges = append(badges, badgeSpec{Text: label, Tone: statusTone(status)})
	}
	to := detailsNav(cd.Type, cd.ID, from, q)
	subtitle := typeLabels[cd.Type] + yearSuffix(cd.Year)
	if cd.Type == "music" && cd.Overview != "" {
		subtitle = cd.Overview + yearSuffix(cd.Year)
	}
	return cardNode(cd.Title, subtitle, imageNode(cd.Poster, cd.Title, cardAspect(cd.Type)), badges, &to, nil, nil)
}

func cardsGrid(cards []card, d statusData, from, q string) node {
	items := make([]node, 0, len(cards))
	for _, cd := range cards {
		items = append(items, cardView(cd, d.statusOf(cd.Type, cd.ID), from, q))
	}
	return grid(150, items)
}

func configWarnings(c *Ctx, cfg settings) []node {
	if !c.isAdmin() {
		return nil
	}
	var out []node
	if cfg.TMDBKey == "" && (cfg.Movies || cfg.TV) {
		out = append(out, alert(noTMDBText, "warning"))
	}
	if cfg.DiscogsToken == "" && cfg.Music {
		out = append(out, alert(noDiscogs, "warning"))
	}
	return out
}

type discoverSection struct {
	Name  string `json:"name"`
	Cards []card `json:"cards"`
}

type discoverR1 struct {
	Services []serviceRow      `json:"services"`
	Limit    *limitRow         `json:"limit"`
	Sections []discoverSection `json:"sections"`
}

var sectionTitles = map[string]string{
	"trending": "Trending", "movies": "Popular movies", "tv": "Popular series", "upcoming": "Upcoming movies", "music": "Popular albums",
}

func trimCards(cards []card) []card {
	if len(cards) > cardCap {
		cards = cards[:cardCap]
	}
	for i := range cards {
		if cards[i].Type != "music" { // for albums the overview holds the artist
			cards[i].Overview = ""
		}
	}
	return cards
}

func discoverContent(c *Ctx, cfg settings) []node {
	calls := []hostCall{dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0)}
	iLimit := -1
	if !c.isAdmin() {
		iLimit = len(calls)
		calls = append(calls, dbFind(tableLimits, []map[string]any{cond("user_id", "eq", c.userID())}, nil, 1, 0))
	}
	var names []string
	for _, name := range discoverSections {
		if call, ok := discoverCall(cfg, name, c.now.Year()); ok {
			names = append(names, name)
			calls = append(calls, call)
		}
	}
	first := len(calls) - len(names)
	r1 := Round(c, calls, func(rs []hostResult) discoverR1 {
		out := discoverR1{Services: rowsOf[serviceRow](rs[0]), Sections: []discoverSection{}}
		if limits := rowsOf[limitRow](at(rs, iLimit)); len(limits) > 0 {
			out.Limit = &limits[0]
		}
		for i, name := range names {
			out.Sections = append(out.Sections, discoverSection{Name: name, Cards: trimCards(parseDiscover(cfg, name, rs[first+i]))})
		}
		return out
	})
	eff := cfg.effective(r1.Services)
	var shown []discoverSection
	var all []card
	for _, section := range r1.Sections {
		kept := []card{}
		for _, cd := range section.Cards {
			if eff.typeEnabled(cd.Type) {
				kept = append(kept, cd)
			}
		}
		if len(kept) > 0 {
			shown = append(shown, discoverSection{Name: section.Name, Cards: kept})
			all = append(all, kept...)
		}
	}
	d := Round(c, statusCalls(all), statusParse)

	out := configWarnings(c, cfg)
	if c.isAdmin() && !eff.Movies && !eff.TV && !eff.Music {
		out = append(out, alert("Add and finish setting up a Sonarr, Radarr or Lidarr server on the Servers tab to enable requests.", "info"))
	}
	if acc := resolveAccess(c.isAdmin(), eff, r1.Limit); !acc.CanRequest {
		out = append(out, alert("You are not allowed to make requests.", "info"))
	}
	for _, section := range shown {
		out = append(out, heading(sectionTitles[section.Name], 2), cardsGrid(section.Cards, d, "discover", ""))
	}
	if len(shown) == 0 {
		out = append(out, empty("Nothing to discover yet."))
	}
	return out
}

func searchContent(c *Ctx, cfg settings, q map[string]string) []node {
	term := truncate(strings.TrimSpace(q["q"]), 100)
	filter := q["type"]
	if !contains(mediaTypes, filter) {
		filter = ""
	}
	options := []option{{Value: "", Label: "All"}}
	for _, t := range mediaTypes {
		if cfg.typeEnabled(t) {
			options = append(options, option{Value: t, Label: map[string]string{"movie": "Movies", "tv": "Series", "music": "Albums"}[t]})
		}
	}
	out := []node{
		searchbox(term, "Search movies, series and albums", []string{"page", "view", "id"}),
		chips("type", options, filter, []string{"page"}),
	}
	out = append(out, configWarnings(c, cfg)...)
	if term == "" {
		return append(out, empty("Type something to search for."))
	}
	calls := []hostCall{dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0)}
	providerCalls, kinds := searchCalls(cfg, term, filter)
	calls = append(calls, providerCalls...)
	type searchR1 struct {
		Services []serviceRow `json:"services"`
		Result   cardsOut     `json:"result"`
	}
	r1 := Round(c, calls, func(rs []hostResult) searchR1 {
		out := searchR1{Services: rowsOf[serviceRow](rs[0]), Result: parseSearch(cfg, filter, kinds, rs[1:])}
		out.Result.Cards = trimCards(out.Result.Cards)
		return out
	})
	eff := cfg.effective(r1.Services)
	cards := []card{}
	for _, cd := range r1.Result.Cards {
		if eff.typeEnabled(cd.Type) {
			cards = append(cards, cd)
		}
	}
	d := Round(c, statusCalls(cards), statusParse)
	switch {
	case r1.Result.Error != "":
		return append(out, alert(r1.Result.Error, "danger"))
	case len(cards) == 0:
		return append(out, empty("No results."))
	}
	return append(out, cardsGrid(cards, d, "search", term))
}

// Requests lists.

type requestsR1 struct {
	Rows     []requestRow `json:"rows"`
	Total    int          `json:"total"`
	Services []serviceRow `json:"services"`
}

type usersR2 struct {
	Names map[string]string `json:"names"`
}

func requestsContent(c *Ctx, cfg settings, q map[string]string, all bool) []node {
	tab := "mine"
	if all {
		if !c.isAdmin() {
			return []node{alert("Only administrators can see all requests.", "danger")}
		}
		tab = "all"
	}
	status := q["status"]
	if !contains(allStatuses, status) {
		status = ""
	}
	page := queryPage(q)
	var where []map[string]any
	if !all {
		where = append(where, cond("user_id", "eq", c.userID()))
	}
	if status != "" {
		where = append(where, cond("status", "eq", status))
	}
	calls := []hostCall{
		dbFind(tableRequests, where, []order{{"created_at", "desc"}}, pageSize, (page-1)*pageSize),
		dbCount(tableRequests, where, ""),
	}
	if all {
		calls = append(calls, dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0))
	}
	r1 := Round(c, calls, func(rs []hostResult) requestsR1 {
		return requestsR1{Rows: rowsOf[requestRow](rs[0]), Total: rs[1].Count, Services: rowsOf[serviceRow](at(rs, 2))}
	})
	names := map[string]string{}
	if all && len(r1.Rows) > 0 {
		ids := []string{}
		seen := map[string]bool{}
		for _, row := range r1.Rows {
			if !seen[row.UserID] {
				seen[row.UserID] = true
				ids = append(ids, row.UserID)
			}
		}
		names = Round(c, []hostCall{usersGet(ids)}, func(rs []hostResult) usersR2 {
			out := usersR2{Names: map[string]string{}}
			for i := range rs[0].Users {
				out.Names[rs[0].Users[i].ID] = displayName(&rs[0].Users[i])
			}
			return out
		}).Names
	}

	options := []option{{Value: "", Label: "All"}}
	for _, s := range allStatuses {
		options = append(options, option{Value: s, Label: statusLabels[s]})
	}
	out := []node{chips("status", options, status, []string{"page"})}
	rows := make([]node, 0, len(r1.Rows))
	for _, row := range r1.Rows {
		rows = append(rows, requestRowNode(c, row, tab, names, r1.Services))
	}
	columns := []column2{{"title", "Title"}, {"type", "Type"}, {"status", "Status"}, {"date", "Requested"}}
	if all {
		columns = append(columns, column2{"user", "User"})
	}
	columns = append(columns, column2{"actions", ""})
	out = append(out, tableNode(columns, rows, "No requests yet."))
	if r1.Total > pageSize {
		out = append(out, pagerNode(page, pageSize, r1.Total))
	}
	return out
}

func seasonsText(seasons []int) string {
	if len(seasons) == 0 {
		return "All seasons"
	}
	parts := make([]string, len(seasons))
	for i, n := range seasons {
		parts[i] = strconv.Itoa(n)
	}
	return "Seasons " + strings.Join(parts, ", ")
}

func requestRowNode(c *Ctx, row requestRow, tab string, names map[string]string, services []serviceRow) node {
	details := []node{link(row.Title, detailsNav(row.Type, row.ExternalID, tab, ""))}
	if artist := str(row.Artist); artist != "" {
		details = append(details, textNode(artist, "muted"))
	}
	if row.Type == "tv" {
		details = append(details, textNode(seasonsText(row.Seasons), "muted"))
	}
	if reason := str(row.DeclineReason); row.Status == "declined" && reason != "" {
		details = append(details, textNode(reason, "muted"))
	}
	if msg := str(row.Error); row.Status == "failed" && msg != "" {
		details = append(details, textNode(msg, "danger"))
	}
	queued := row.Status == "approved" && row.SendAttempts != nil
	if queued {
		details = append(details, textNode("Queued - will be sent shortly.", "muted"))
	}
	date := row.CreatedAt
	if len(date) >= 10 {
		date = date[:10]
	}
	cells := map[string]any{
		"title":  column(1, details...),
		"type":   typeLabels[row.Type],
		"status": badge(statusLabel(row), statusTone(row.Status)),
		"date":   date,
	}
	var actions []node
	remove := button("Remove", action{Op: "cancel", Body: map[string]any{"id": row.ID}, Reload: true, Confirm: "Remove this request?"}, "danger")
	if c.isAdmin() {
		cells["user"] = names[row.UserID]
		hint := map[string]any{"id": row.ID, "type": row.Type, "externalId": row.ExternalID}
		if svc, _ := resolveService(services, kindOfType[row.Type], ""); svc != nil {
			hint["serviceId"] = svc.ID
		}
		switch row.Status {
		case "pending", "declined":
			actions = append(actions, button("Approve", action{Op: "approve", Body: hint, Reload: true}, "primary"))
		case "failed":
			actions = append(actions, button("Retry", action{Op: "retry", Body: hint, Reload: true}, "primary"))
		case "approved":
			if queued {
				actions = append(actions, button("Send now", action{Op: "retry", Body: hint, Reload: true}, "primary"))
			}
		}
		if row.Status == "pending" {
			actions = append(actions, form([]field{
				hidden("id", row.ID), {Name: "reason", Type: "text", Placeholder: "Reason (optional)"},
			}, "Decline", action{Op: "decline", Reload: true}, "quiet", "inline"))
		}
		actions = append(actions, remove)
	} else if row.Status == "pending" {
		actions = append(actions, button("Cancel", action{Op: "cancel", Body: map[string]any{"id": row.ID}, Reload: true, Confirm: "Cancel this request?"}, "quiet"))
	}
	cells["actions"] = row2(actions)
	return tableRow(row.ID, cells)
}

func row2(children []node) node { return row(2, children...) }

func labelOf(n int, name string, episodes int) string {
	label := fmt.Sprintf("Season %d", n)
	if name != "" && name != label {
		label = fmt.Sprintf("%s (%s)", label, name)
	}
	if episodes > 0 {
		label += fmt.Sprintf(" · %d episodes", episodes)
	}
	return label
}
