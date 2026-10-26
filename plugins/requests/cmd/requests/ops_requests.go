package main

func at(rs []hostResult, i int) hostResult {
	if i < 0 || i >= len(rs) {
		return hostResult{}
	}
	return rs[i]
}

func yearPtr(year int) *int {
	if year <= 0 {
		return nil
	}
	return &year
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func infoOf(row requestRow, tvdb int) sendInfo {
	return sendInfo{Type: row.Type, ExternalID: row.ExternalID, Title: row.Title, Artist: str(row.Artist), Seasons: row.Seasons, TVDB: tvdb}
}

func dispatchFailure(msg string) string { return truncate(msg, 500) }

type createR1 struct {
	Existing []requestRow `json:"existing"`
	Services []serviceRow `json:"services"`
	Limit    *limitRow    `json:"limit"`
	Detail   detailOut    `json:"detail"`
	Key      string       `json:"key"`
	TVDB     int          `json:"tvdb"`
}

type createR2 struct {
	Match  string     `json:"match"`
	Used   int        `json:"used"`
	Lookup lookupData `json:"lookup"`
	User   *hostUser  `json:"user"`
}

func overridesOf(admin bool, a args) (serviceID string, quality *int, root string, err error) {
	if !admin {
		return "", nil, "", nil
	}
	if serviceID, err = a.uuid("serviceId"); err != nil {
		return
	}
	if quality, err = a.optInt("qualityProfileId"); err != nil {
		return
	}
	root, err = a.str("rootFolder", 500)
	return
}

// opRequest creates a request: the quota, duplicate and library checks, then either approval (sending it to the
// server) or a notification to the administrators.
func opRequest(c *Ctx) (any, error) {
	if err := requireMember(c); err != nil {
		return nil, err
	}
	a, err := parseArgs(c.env.Request.Body)
	if err != nil {
		return nil, err
	}
	mediaType, err := a.mediaType()
	if err != nil {
		return nil, err
	}
	externalID, err := a.externalID()
	if err != nil {
		return nil, err
	}
	wantedSeasons, seasonsGiven, err := a.seasons()
	if err != nil {
		return nil, err
	}
	hint, err := a.uuid("serviceId")
	if err != nil {
		return nil, err
	}
	admin := c.isAdmin()
	chosenService, qualityOverride, rootOverride, err := overridesOf(admin, a)
	if err != nil {
		return nil, err
	}
	cfg := parseSettings(c.env.Settings)
	detailRequest, err := detailsCall(cfg, mediaType, externalID)
	if err != nil {
		return nil, err
	}

	var iExisting, iServices, iLimit, iDetail, iSecret, iTVDB = -1, -1, -1, -1, -1, -1
	var calls []hostCall
	add := func(call hostCall) int { calls = append(calls, call); return len(calls) - 1 }
	iExisting = add(dbFind(tableRequests, []map[string]any{
		cond("type", "eq", mediaType), cond("external_id", "eq", externalID), cond("status", "in", []string{"pending", "approved", "available"}),
	}, nil, 200, 0))
	iServices = add(dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0))
	if !admin {
		iLimit = add(dbFind(tableLimits, []map[string]any{cond("user_id", "eq", c.userID())}, nil, 1, 0))
	}
	iDetail = add(detailRequest)
	if hint != "" {
		iSecret = add(secretGet(secretName(hint)))
		if mediaType == "tv" && cfg.TMDBKey != "" {
			iTVDB = add(tvdbCall(cfg, externalID))
		}
	}
	r1 := Round(c, calls, func(rs []hostResult) createR1 {
		out := createR1{Existing: rowsOf[requestRow](at(rs, iExisting)), Services: rowsOf[serviceRow](at(rs, iServices))}
		if limits := rowsOf[limitRow](at(rs, iLimit)); len(limits) > 0 {
			out.Limit = &limits[0]
		}
		out.Detail = parseDetails(mediaType, externalID, at(rs, iDetail))
		if secret := at(rs, iSecret); secret.OK {
			out.Key = secret.Value
		}
		if iTVDB >= 0 {
			out.TVDB = parseTVDB(at(rs, iTVDB))
		}
		return out
	})

	eff := cfg.effective(r1.Services)
	acc := resolveAccess(admin, eff, r1.Limit)
	if !acc.CanRequest {
		return nil, fail("You are not allowed to make requests.")
	}
	if !eff.typeEnabled(mediaType) {
		return nil, fail("This kind of request is turned off.")
	}
	if r1.Detail.Detail == nil {
		return nil, fail("%s", r1.Detail.Error)
	}
	det := r1.Detail.Detail

	var seasons []int
	if mediaType == "tv" {
		all := make([]int, 0, len(det.Seasons))
		for _, season := range det.Seasons {
			all = append(all, season.Number)
		}
		var wanted []int
		if seasonsGiven {
			for _, n := range wantedSeasons {
				if containsInt(all, n) {
					wanted = append(wanted, n)
				}
			}
			if len(wanted) == 0 {
				return nil, fail("Choose at least one valid season.")
			}
		}
		taken := make([][]int, 0, len(r1.Existing))
		for _, existing := range r1.Existing {
			taken = append(taken, existing.Seasons)
		}
		seasons = remainingSeasons(wanted, taken, all)
		if len(seasons) == 0 {
			return nil, fail("Every chosen season has already been requested.")
		}
		if wanted == nil && len(seasons) == len(all) {
			seasons = nil
		}
	} else if len(r1.Existing) > 0 {
		return nil, fail("This has already been requested.")
	}

	// Decide whether the request can be sent right away; that needs the key of the server named by the page.
	kind := kindOfType[mediaType]
	dispatch := false
	var svc *serviceRow
	if admin {
		if svc, err = resolveService(r1.Services, kind, chosenService); err != nil {
			return nil, err
		}
	} else if svc, err = resolveService(r1.Services, kind, ""); err != nil {
		return nil, err
	}
	if acc.AutoApprove && svc != nil && hint != "" && svc.ID == hint && r1.Key != "" {
		dispatch = true
	}

	row := requestRow{
		ID: newID(), UserID: c.userID(), Type: mediaType, Source: det.Source, ExternalID: externalID,
		Title: truncate(det.Title, 300), Year: yearPtr(det.Year), Seasons: seasons, Status: "pending",
	}
	if det.Poster != "" {
		poster := det.Poster
		row.Poster = &poster
	}
	if artist := truncate(det.Artist, 300); artist != "" {
		row.Artist = &artist
	}
	var snd sender
	if dispatch {
		target := targetOf(*svc, qualityOverride, rootOverride)
		snd = sender{Conn: arrConn{Kind: kind, URL: svc.URL, Key: r1.Key}, Info: infoOf(row, r1.TVDB), Target: target}
	}

	// Round 2: library, quota, the lookups for sending, and who to name in the admin notification.
	var iMatch, iQuota, iUser = -1, -1, -1
	calls = nil
	if mediaType != "tv" {
		year := yearPtr(det.Year)
		iMatch = add(libraryMatch([]libraryCard{{Key: mediaType + ":" + externalID, Type: mediaType, Source: det.Source, ID: externalID, Title: det.Title, Year: year}}))
	}
	if !admin && acc.QuotaLimit > 0 {
		iQuota = add(quotaCall(c, c.userID(), acc.QuotaDays))
	}
	lookups := []hostCall(nil)
	if dispatch {
		lookups = snd.lookupCalls()
		calls = append(calls, lookups...)
	} else if cfg.NotifyAdmins {
		iUser = add(usersGet([]string{c.userID()}))
	}
	lookupStart := len(calls) - len(lookups)
	if !dispatch && iUser >= 0 {
		lookupStart = len(calls)
	}
	r2 := Round(c, calls, func(rs []hostResult) createR2 {
		out := createR2{}
		if match := at(rs, iMatch); match.OK {
			out.Match = match.Matches[mediaType+":"+externalID]
		}
		out.Used = at(rs, iQuota).Count
		if users := at(rs, iUser).Users; len(users) > 0 {
			out.User = &users[0]
		}
		if dispatch {
			out.Lookup = snd.parseLookup(rs[lookupStart:])
		}
		return out
	})
	if r2.Match != "" {
		return nil, fail("This is already in the library.")
	}
	if !admin && acc.QuotaLimit > 0 && r2.Used >= acc.QuotaLimit {
		return nil, fail("You have used all %d requests allowed every %d days.", acc.QuotaLimit, acc.QuotaDays)
	}

	now := isoTime(c.now)
	if admin {
		if chosenService != "" {
			row.ServiceID = &chosenService
		}
		row.QualityProfileID = qualityOverride
		if rootOverride != "" {
			row.RootFolder = &rootOverride
		}
	}
	if !dispatch {
		insert := dbInsert(tableRequests, requestInsert(row, now))
		final := []hostCall{insert}
		if cfg.NotifyAdmins {
			final = append(final, notifyAdmins("New request", displayName(r2.User)+" requested "+row.Title+".", pluginPagePath+"?tab=all&status=pending"))
		}
		Round(c, final, func([]hostResult) struct{} { return struct{}{} })
		return done("Request submitted.", ""), nil
	}

	plan := snd.plan(r2.Lookup)
	result := Round(c, plan.Calls, func(rs []hostResult) outcome { return snd.parseApply(plan, rs) })
	processed := c.userID()
	row.ProcessedBy = &processed
	final := []hostCall{}
	message, tone := "Request submitted and approved.", ""
	if result.OK {
		row.Status = "approved"
		serviceID := svc.ID
		row.ServiceID = &serviceID
		final = append(final, result.Follow...)
	} else {
		row.Status = "failed"
		failure := dispatchFailure(result.Err)
		row.Error = &failure
		message, tone = "Request submitted, but it could not be sent: "+failure, "warning"
	}
	final = append([]hostCall{dbInsert(tableRequests, requestInsert(row, now))}, final...)
	if result.OK && cfg.NotifyUsers {
		final = append(final, notifyUser(row.UserID, "Request approved", row.Title+" was approved and is on its way.", pluginPagePath+"?tab=mine"))
	}
	Round(c, final, func([]hostResult) struct{} { return struct{}{} })
	return done(message, tone), nil
}

func requestInsert(row requestRow, now string) map[string]any {
	seasons := any(nil)
	if row.Seasons != nil {
		seasons = row.Seasons
	}
	return map[string]any{
		"id": row.ID, "user_id": row.UserID, "type": row.Type, "source": row.Source, "external_id": row.ExternalID,
		"title": row.Title, "year": row.Year, "poster": row.Poster, "artist": row.Artist, "seasons": seasons,
		"status": row.Status, "service_id": row.ServiceID, "quality_profile_id": row.QualityProfileID,
		"root_folder": row.RootFolder, "decline_reason": row.DeclineReason, "error": row.Error,
		"processed_by": row.ProcessedBy, "created_at": now, "updated_at": now,
	}
}

type approveR1 struct {
	Request  *requestRow  `json:"request"`
	Services []serviceRow `json:"services"`
	Key      string       `json:"key"`
	TVDB     int          `json:"tvdb"`
}

// opApprove approves a pending or declined request, or retries a failed one, by sending it to its server.
// The page names the server (and, for shows, the title) it expects so that the key and the show's TVDB id can be
// fetched in the first round; the names are checked against the stored request.
func opApprove(retry bool) func(c *Ctx) (any, error) {
	return func(c *Ctx) (any, error) {
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
		hint, err := a.uuid("serviceId")
		if err != nil {
			return nil, err
		}
		hintType, _ := a.str("type", 8)
		hintExternal, _ := a.str("externalId", 12)
		quality, err := a.optInt("qualityProfileId")
		if err != nil {
			return nil, err
		}
		root, err := a.str("rootFolder", 500)
		if err != nil {
			return nil, err
		}
		cfg := parseSettings(c.env.Settings)

		var calls []hostCall
		add := func(call hostCall) int { calls = append(calls, call); return len(calls) - 1 }
		iRequest := add(dbFind(tableRequests, []map[string]any{cond("id", "eq", id)}, nil, 1, 0))
		iServices := add(dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0))
		iSecret, iTVDB := -1, -1
		if hint != "" {
			iSecret = add(secretGet(secretName(hint)))
		}
		if hintType == "tv" && numericID.MatchString(hintExternal) && cfg.TMDBKey != "" {
			iTVDB = add(tvdbCall(cfg, hintExternal))
		}
		r1 := Round(c, calls, func(rs []hostResult) approveR1 {
			out := approveR1{Services: rowsOf[serviceRow](at(rs, iServices))}
			if rows := rowsOf[requestRow](at(rs, iRequest)); len(rows) > 0 {
				out.Request = &rows[0]
			}
			if secret := at(rs, iSecret); secret.OK {
				out.Key = secret.Value
			}
			if iTVDB >= 0 {
				out.TVDB = parseTVDB(at(rs, iTVDB))
			}
			return out
		})
		req := r1.Request
		if req == nil {
			return nil, fail("Request not found.")
		}
		if retry && req.Status != "failed" && req.Status != "approved" {
			return nil, fail("Only failed requests can be retried.")
		}
		if !retry && req.Status != "pending" && req.Status != "declined" {
			return nil, fail("Only pending or declined requests can be approved.")
		}
		if hintType == "tv" && (req.Type != "tv" || req.ExternalID != hintExternal) {
			return nil, fail("This request changed. Reload the page and try again.")
		}
		if quality != nil && *quality < 1 {
			return nil, fail("Invalid qualityProfileId.")
		}

		kind := kindOfType[req.Type]
		svc, resolveErr := resolveService(r1.Services, kind, hint)
		failure := ""
		switch {
		case resolveErr != nil:
			return nil, resolveErr
		case svc == nil:
			failure = "No " + kind + " server is configured. Add one on the Services tab."
		case svc.ID != hint:
			return nil, fail("The server for this request changed. Reload the page and try again.")
		case r1.Key == "":
			failure = "The API key for this server is missing. Edit the server and enter it again."
		}
		if failure == "" && req.Type == "tv" && r1.TVDB == 0 && iTVDB < 0 {
			return nil, fail("This request changed. Reload the page and try again.")
		}

		var snd sender
		result := outcome{Err: failure}
		if failure == "" {
			info := infoOf(*req, r1.TVDB)
			snd = sender{Conn: arrConn{Kind: kind, URL: svc.URL, Key: r1.Key}, Info: info, Target: targetOf(*svc, quality, root)}
			ld := Round(c, snd.lookupCalls(), snd.parseLookup)
			plan := snd.plan(ld)
			result = Round(c, plan.Calls, func(rs []hostResult) outcome { return snd.parseApply(plan, rs) })
		}

		set := map[string]any{"updated_at": isoTime(c.now), "processed_by": c.userID(), "decline_reason": nil}
		if quality != nil {
			set["quality_profile_id"] = *quality
		}
		if root != "" {
			set["root_folder"] = root
		}
		final := []hostCall{}
		message, tone := "Request approved.", ""
		if result.OK {
			set["status"], set["error"], set["service_id"] = "approved", nil, svc.ID
			final = append(final, result.Follow...)
		} else {
			set["status"], set["error"] = "failed", dispatchFailure(result.Err)
			message, tone = "The request could not be sent: "+dispatchFailure(result.Err), "danger"
		}
		final = append([]hostCall{dbUpdate(tableRequests, []map[string]any{cond("id", "eq", id)}, set)}, final...)
		if result.OK && cfg.NotifyUsers {
			final = append(final, notifyUser(req.UserID, "Request approved", req.Title+" was approved and is on its way.", pluginPagePath+"?tab=mine"))
		}
		Round(c, final, func([]hostResult) struct{} { return struct{}{} })
		return done(message, tone), nil
	}
}

func opDecline(c *Ctx) (any, error) {
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
	reason, err := a.str("reason", 500)
	if err != nil {
		return nil, err
	}
	cfg := parseSettings(c.env.Settings)
	rows := Round(c, []hostCall{dbFind(tableRequests, []map[string]any{cond("id", "eq", id)}, nil, 1, 0)}, func(rs []hostResult) []requestRow {
		return rowsOf[requestRow](rs[0])
	})
	if len(rows) == 0 {
		return nil, fail("Request not found.")
	}
	req := rows[0]
	if req.Status == "available" {
		return nil, fail("This request has already been fulfilled.")
	}
	set := map[string]any{
		"status": "declined", "decline_reason": nilIfEmpty(reason), "processed_by": c.userID(), "error": nil, "updated_at": isoTime(c.now),
	}
	final := []hostCall{dbUpdate(tableRequests, []map[string]any{cond("id", "eq", id)}, set)}
	if cfg.NotifyUsers {
		message := req.Title + " was declined."
		if reason != "" {
			message = req.Title + " was declined: " + reason
		}
		final = append(final, notifyUser(req.UserID, "Request declined", message, pluginPagePath+"?tab=mine"))
	}
	Round(c, final, func([]hostResult) struct{} { return struct{}{} })
	return done("Request declined.", ""), nil
}

// opCancel deletes a request: administrators any, everyone else only their own pending ones.
func opCancel(c *Ctx) (any, error) {
	if err := requireMember(c); err != nil {
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
	rows := Round(c, []hostCall{dbFind(tableRequests, []map[string]any{cond("id", "eq", id)}, nil, 1, 0)}, func(rs []hostResult) []requestRow {
		return rowsOf[requestRow](rs[0])
	})
	if len(rows) == 0 {
		return nil, fail("Request not found.")
	}
	if !c.isAdmin() && (rows[0].UserID != c.userID() || rows[0].Status != "pending") {
		return nil, fail("You can only cancel your own pending requests.")
	}
	Round(c, []hostCall{dbDelete(tableRequests, []map[string]any{cond("id", "eq", id)})}, func([]hostResult) struct{} { return struct{}{} })
	return actionResult{Toast: &toast{Text: "Request removed."}, Reload: true, Navigate: &nav{Query: map[string]string{"tab": "mine"}}}, nil
}

const (
	syncBatch  = 200
	syncNotify = 7
)

type syncR1 struct {
	Approved []requestRow `json:"approved"`
}

type syncR2 struct {
	Matches map[string]string `json:"matches"`
}

// opSync marks approved requests as available once the title is in the library. It runs as the system user.
func opSync(c *Ctx) (any, error) {
	if c.role() != "system" {
		return nil, fail("Only the scheduler can run this.")
	}
	cfg := parseSettings(c.env.Settings)
	r1 := Round(c, []hostCall{dbFind(tableRequests, []map[string]any{cond("status", "eq", "approved")}, []order{{"created_at", "asc"}}, syncBatch, 0)},
		func(rs []hostResult) syncR1 { return syncR1{Approved: rowsOf[requestRow](rs[0])} })
	if len(r1.Approved) == 0 {
		return map[string]any{"changed": 0}, nil
	}
	var calls []hostCall
	for start := 0; start < len(r1.Approved) && len(calls) < maxCallsPerRound; start += 50 {
		end := min(start+50, len(r1.Approved))
		cards := make([]libraryCard, 0, end-start)
		for _, req := range r1.Approved[start:end] {
			cards = append(cards, libraryCard{Key: req.ID, Type: req.Type, Source: req.Source, ID: req.ExternalID, Title: req.Title, Year: req.Year})
		}
		calls = append(calls, libraryMatch(cards))
	}
	r2 := Round(c, calls, func(rs []hostResult) syncR2 {
		out := syncR2{Matches: map[string]string{}}
		for _, r := range rs {
			for key, value := range r.Matches {
				out.Matches[key] = value
			}
		}
		return out
	})
	matched := []requestRow{}
	for _, req := range r1.Approved {
		if _, ok := r2.Matches[req.ID]; ok && len(matched) < syncNotify {
			matched = append(matched, req)
		}
	}
	if len(matched) == 0 {
		return map[string]any{"changed": 0}, nil
	}
	ids := make([]string, 0, len(matched))
	for _, req := range matched {
		ids = append(ids, req.ID)
	}
	final := []hostCall{dbUpdate(tableRequests, []map[string]any{cond("id", "in", ids), cond("status", "eq", "approved")},
		map[string]any{"status": "available", "updated_at": isoTime(c.now)})}
	if cfg.NotifyUsers {
		for _, req := range matched {
			final = append(final, notifyUser(req.UserID, "Now available", req.Title+" is now in your library.", "/media/"+r2.Matches[req.ID]))
		}
	}
	Round(c, final, func([]hostResult) struct{} { return struct{}{} })
	return map[string]any{"changed": len(matched)}, nil
}
