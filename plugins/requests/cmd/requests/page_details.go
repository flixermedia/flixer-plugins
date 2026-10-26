package main

import (
	"fmt"
	"strconv"
	"strings"
)

type detailsR1 struct {
	Detail   detailOut    `json:"detail"`
	Services []serviceRow `json:"services"`
	Limit    *limitRow    `json:"limit"`
	Refs     []reqRef     `json:"refs"`
}

func detailsView(c *Ctx, cfg settings, q map[string]string) node {
	from := q["from"]
	if from != "search" && from != "mine" && from != "all" {
		from = "discover"
	}
	backQuery := map[string]string{"tab": from}
	if term := strings.TrimSpace(q["q"]); term != "" && from == "search" {
		backQuery["q"] = truncate(term, 100)
	}
	back := link("Back", nav{Query: backQuery})
	problem := func(msg string) node { return column(3, back, alert(msg, "danger")) }

	mediaType, id := q["type"], q["id"]
	if !contains(mediaTypes, mediaType) || !numericID.MatchString(id) {
		return problem("Invalid title.")
	}
	detailCall, err := detailsCall(cfg, mediaType, id)
	if err != nil {
		return problem(err.Error())
	}
	calls := []hostCall{
		detailCall,
		dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0),
		dbFind(tableRequests, []map[string]any{
			cond("type", "eq", mediaType), cond("external_id", "eq", id), cond("status", "in", []string{"pending", "approved", "available"}),
		}, nil, 200, 0),
	}
	if !c.isAdmin() {
		calls = append(calls, dbFind(tableLimits, []map[string]any{cond("user_id", "eq", c.userID())}, nil, 1, 0))
	}
	r1 := Round(c, calls, func(rs []hostResult) detailsR1 {
		out := detailsR1{Detail: parseDetails(mediaType, id, rs[0]), Services: rowsOf[serviceRow](rs[1]), Refs: []reqRef{}}
		for _, row := range rowsOf[requestRow](rs[2]) {
			out.Refs = append(out.Refs, refOf(row))
		}
		if limits := rowsOf[limitRow](at(rs, 3)); len(limits) > 0 {
			out.Limit = &limits[0]
		}
		if out.Detail.Detail != nil {
			out.Detail.Detail.Cast = firstN(out.Detail.Detail.Cast, 12)
			out.Detail.Detail.Tracklist = firstN(out.Detail.Detail.Tracklist, 40)
			out.Detail.Detail.Overview = truncate(out.Detail.Detail.Overview, 1500)
		}
		return out
	})
	if r1.Detail.Detail == nil {
		return problem(r1.Detail.Error)
	}
	det := *r1.Detail.Detail

	var libraryCalls []hostCall
	if mediaType != "tv" {
		libraryCalls = append(libraryCalls, libraryMatch([]libraryCard{{Key: cardKey(mediaType, id), Type: mediaType, Source: det.Source, ID: id, Title: det.Title, Year: yearPtr(det.Year)}}))
	}
	inLibrary := Round(c, libraryCalls, func(rs []hostResult) bool {
		return len(rs) > 0 && rs[0].OK && rs[0].Matches[cardKey(mediaType, id)] != ""
	})

	eff := cfg.effective(r1.Services)
	acc := resolveAccess(c.isAdmin(), eff, r1.Limit)

	status := ""
	for _, ref := range r1.Refs {
		if statusRank[ref.Status] > statusRank[status] && mediaType != "tv" {
			status = ref.Status
		}
	}
	if inLibrary {
		status = "available"
	}

	badges := []node{badge(typeLabels[mediaType], "info")}
	if det.Year > 0 {
		badges = append(badges, badge(strconv.Itoa(det.Year), "default"))
	}
	if det.Runtime > 0 {
		badges = append(badges, badge(fmt.Sprintf("%d min", det.Runtime), "default"))
	}
	if det.Rating > 0 {
		badges = append(badges, badge(fmt.Sprintf("★ %.1f", det.Rating), "default"))
	}
	for _, genre := range firstN(det.Genres, 8) {
		badges = append(badges, badge(genre, "default"))
	}
	if label, ok := cardStatusLabels[status]; ok {
		badges = append(badges, badge(label, statusTone(status)))
	}

	body := []node{back, imageNode(det.Backdrop, det.Title, "wide"), heading(det.Title, 1)}
	if det.Artist != "" {
		body = append(body, textNode(det.Artist, "muted"))
	}
	body = append(body, row(1, badges...))
	if det.Tagline != "" {
		body = append(body, textNode(det.Tagline, "muted"))
	}
	if det.Overview != "" {
		body = append(body, textNode(det.Overview, ""))
	}
	if len(det.Cast) > 0 {
		body = append(body, textNode("Cast: "+strings.Join(det.Cast, ", "), "muted"))
	}
	if len(det.Tracklist) > 0 {
		body = append(body, textNode("Tracks: "+strings.Join(det.Tracklist, " · "), "muted"))
	}
	if poster := imageNode(det.Poster, det.Title, cardAspect(mediaType)); poster != nil {
		body = append([]node{back, row(3, column(1, poster))}, body[1:]...)
	}
	body = append(body, requestArea(c, eff, acc, r1, det, mediaType, status)...)
	return column(3, body...)
}

func firstN[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}

func requestArea(c *Ctx, eff settings, acc access, r1 detailsR1, det detail, mediaType, status string) []node {
	switch {
	case status == "available":
		return []node{alert("This is already in the library.", "success")}
	case !acc.CanRequest:
		return []node{alert("You are not allowed to make requests.", "info")}
	case !eff.typeEnabled(mediaType):
		return []node{alert("This kind of request is turned off.", "info")}
	case status != "":
		return []node{alert("This has already been requested.", "info")}
	}

	fields := []field{hidden("type", mediaType), hidden("externalId", det.ID)}
	var out []node
	if mediaType == "tv" {
		all := make([]int, 0, len(det.Seasons))
		for _, s := range det.Seasons {
			all = append(all, s.Number)
		}
		taken := make([][]int, 0, len(r1.Refs))
		for _, ref := range r1.Refs {
			taken = append(taken, ref.Seasons)
		}
		left := remainingSeasons(nil, taken, all)
		if len(left) == 0 {
			return []node{alert("Every season has already been requested.", "info")}
		}
		if len(left) < len(all) {
			out = append(out, textNode("Some seasons were already requested; only the remaining ones can be chosen.", "muted"))
		}
		byNumber := map[int]seasonInfo{}
		for _, s := range det.Seasons {
			byNumber[s.Number] = s
		}
		for _, n := range left {
			s := byNumber[n]
			fields = append(fields, field{Name: fmt.Sprintf("season_%d", n), Label: labelOf(n, s.Name, s.EpisodeCount), Type: "checkbox", Value: true})
		}
	}

	svc, _ := resolveService(r1.Services, kindOfType[mediaType], "")
	var choices []option
	for _, s := range r1.Services {
		if s.Kind == kindOfType[mediaType] && s.usable() {
			choices = append(choices, option{Value: s.ID, Label: s.Name})
		}
	}
	switch {
	case svc == nil:
	case c.isAdmin() && len(choices) > 1:
		fields = append(fields, field{Name: "serviceId", Label: "Server", Type: "select", Value: svc.ID, Options: choices})
	default:
		fields = append(fields, hidden("serviceId", svc.ID))
	}
	if !c.isAdmin() && acc.QuotaLimit > 0 {
		out = append(out, textNode(fmt.Sprintf("You can make %d requests every %d days.", acc.QuotaLimit, acc.QuotaDays), "muted"))
	}
	if acc.AutoApprove {
		out = append(out, textNode("Your request is approved automatically.", "muted"))
	}
	return append(out, form(fields, "Request", action{Op: "request", Reload: true}, "primary", "column"))
}
