package main

import "strings"

const searchLimit = 20

var searchTypes = map[string]string{"movie": "movie", "series": "tv", "album": "music"}

type searchR0 struct {
	Settings settingsR1   `json:"settings"`
	Services []serviceRow `json:"services"`
	Limit    *limitRow    `json:"limit"`
}

type searchR1 struct {
	Result cardsOut `json:"result"`
	Used   int      `json:"used"`
}

type searchBadge struct {
	Text string `json:"text"`
	Tone string `json:"tone"`
}

type searchItem struct {
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle,omitempty"`
	Poster   string            `json:"poster,omitempty"`
	Shape    string            `json:"shape"`
	Badge    *searchBadge      `json:"badge,omitempty"`
	Page     string            `json:"page"`
	Query    map[string]string `json:"query"`
}

type searchSection struct {
	Title string       `json:"title"`
	Items []searchItem `json:"items"`
}

func emptySearch() map[string]any { return map[string]any{"sections": []searchSection{}} }

// opSearch feeds Flixer's main search: titles from TMDB and Discogs that are not in the library yet, each linking
// to the request details page. Three rounds: settings and servers, provider search (and quota), request statuses.
func opSearch(c *Ctx) (any, error) {
	if err := requireMember(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	term, err := a.str("q", 200)
	if err != nil {
		return nil, err
	}
	term = strings.TrimSpace(term)
	kind, err := a.str("type", 16)
	if err != nil {
		return nil, err
	}
	filter := ""
	if kind != "" {
		mapped, ok := searchTypes[kind]
		if !ok {
			return emptySearch(), nil
		}
		filter = mapped
	}
	if len([]rune(term)) < 2 {
		return emptySearch(), nil
	}

	var calls []hostCall
	iServices, iLimit := -1, -1
	calls = append(calls, settingsCalls()...)
	iServices = len(calls)
	calls = append(calls, dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0))
	if !c.isAdmin() {
		iLimit = len(calls)
		calls = append(calls, dbFind(tableLimits, []map[string]any{cond("user_id", "eq", c.userID())}, nil, 1, 0))
	}
	r0 := Round(c, calls, func(rs []hostResult) searchR0 {
		out := searchR0{Settings: settingsFrom(rs, 0), Services: rowsOf[serviceRow](at(rs, iServices))}
		if limits := rowsOf[limitRow](at(rs, iLimit)); len(limits) > 0 {
			out.Limit = &limits[0]
		}
		return out
	})
	eff := buildSettings(c, r0.Settings).effective(r0.Services)
	acc := resolveAccess(c.isAdmin(), eff, r0.Limit)
	if !acc.CanRequest {
		return emptySearch(), nil
	}
	providerCalls, kinds := searchCalls(eff, term, filter)
	if len(providerCalls) == 0 {
		return emptySearch(), nil
	}
	iQuota := -1
	limited := !c.isAdmin() && acc.QuotaLimit > 0
	if limited {
		iQuota = len(providerCalls)
		providerCalls = append(providerCalls, quotaCall(c, c.userID(), acc.QuotaDays))
	}
	r1 := Round(c, providerCalls, func(rs []hostResult) searchR1 {
		out := searchR1{Result: parseSearch(eff, filter, kinds, rs[:len(kinds)])}
		out.Result.Cards = trimCards(out.Result.Cards)
		out.Used = at(rs, iQuota).Count
		return out
	})
	if r1.Result.Error != "" || (limited && r1.Used >= acc.QuotaLimit) {
		return emptySearch(), nil
	}
	cards := []card{}
	for _, cd := range r1.Result.Cards {
		if eff.typeEnabled(cd.Type) {
			cards = append(cards, cd)
		}
	}
	d := Round(c, statusCalls(cards), statusParse)

	items := []searchItem{}
	for _, cd := range cards {
		status := d.statusOf(cd.Type, cd.ID)
		if status == "available" {
			continue
		}
		subtitle := typeLabels[cd.Type] + yearSuffix(cd.Year)
		if cd.Type == "music" && cd.Overview != "" {
			subtitle = cd.Overview + yearSuffix(cd.Year)
		}
		item := searchItem{
			Title: cd.Title, Subtitle: subtitle, Shape: cardAspect(cd.Type), Page: "requests",
			Query: detailsNav(cd.Type, cd.ID, "search", term).Query,
		}
		if strings.HasPrefix(cd.Poster, "https://") {
			item.Poster = cd.Poster
		}
		if label, ok := cardStatusLabels[status]; ok {
			item.Badge = &searchBadge{Text: label, Tone: searchTone(status)}
		}
		items = append(items, item)
		if len(items) == searchLimit {
			break
		}
	}
	if len(items) == 0 {
		return emptySearch(), nil
	}
	return map[string]any{"sections": []searchSection{{Title: "Not in library", Items: items}}}, nil
}

// searchTone maps a request status to the tones Flixer's search page knows.
func searchTone(status string) string {
	switch status {
	case "pending":
		return "warning"
	case "approved":
		return "info"
	case "failed", "declined":
		return "danger"
	}
	return "neutral"
}
