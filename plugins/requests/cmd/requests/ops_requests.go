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

type createR0 struct {
	Settings settingsR1   `json:"settings"`
	Existing []requestRow `json:"existing"`
	Services []serviceRow `json:"services"`
	Limit    *limitRow    `json:"limit"`
}

type createR1 struct {
	Detail detailOut `json:"detail"`
	TVDB   int       `json:"tvdb"`
	Key    string    `json:"key"`
	Used   int       `json:"used"`
	User   *hostUser `json:"user"`
}

type createR2 struct {
	Match  string     `json:"match"`
	Lookup lookupData `json:"lookup"`
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
// server) or a notification to the administrators. Approval that fits the host's four rounds sends at once;
// otherwise the request is stored as approved and queued, and the scheduled sync sends it.
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
	if _, err := a.uuid("serviceId"); err != nil {
		return nil, err
	}
	admin := c.isAdmin()
	chosenService, qualityOverride, rootOverride, err := overridesOf(admin, a)
	if err != nil {
		return nil, err
	}
	var iExisting, iServices, iLimit = -1, -1, -1
	var calls []hostCall
	add := func(call hostCall) int { calls = append(calls, call); return len(calls) - 1 }
	iSettings := len(calls)
	calls = append(calls, settingsCalls()...)
	iExisting = add(dbFind(tableRequests, []map[string]any{
		cond("type", "eq", mediaType), cond("external_id", "eq", externalID), cond("status", "in", []string{"pending", "approved", "available"}),
	}, nil, 200, 0))
	iServices = add(dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0))
	if !admin {
		iLimit = add(dbFind(tableLimits, []map[string]any{cond("user_id", "eq", c.userID())}, nil, 1, 0))
	}
	r0 := Round(c, calls, func(rs []hostResult) createR0 {
		out := createR0{Settings: settingsFrom(rs, iSettings), Existing: rowsOf[requestRow](at(rs, iExisting)), Services: rowsOf[serviceRow](at(rs, iServices))}
		if limits := rowsOf[limitRow](at(rs, iLimit)); len(limits) > 0 {
			out.Limit = &limits[0]
		}
		return out
	})
	cfg := buildSettings(c, r0.Settings)

	eff := cfg.effective(r0.Services)
	acc := resolveAccess(admin, eff, r0.Limit)
	if !acc.CanRequest {
		return nil, fail("You are not allowed to make requests.")
	}
	if !eff.typeEnabled(mediaType) {
		return nil, fail("This kind of request is turned off.")
	}
	if mediaType != "tv" && len(r0.Existing) > 0 {
		return nil, fail("This has already been requested.")
	}
	if admin && chosenService != "" {
		if _, err := resolveService(r0.Services, kindOfType[mediaType], chosenService); err != nil {
			return nil, err
		}
	}
	detailRequest, err := detailsCall(cfg, mediaType, externalID)
	if err != nil {
		return nil, err
	}

	// Round 2: details, the TVDB id, the server key, the quota and who to name in the admin notification.
	var svc *serviceRow
	svcFailure := ""
	if acc.AutoApprove {
		var chosen *string
		if chosenService != "" {
			chosen = &chosenService
		}
		svc, svcFailure = serviceFor(requestRow{Type: mediaType, ServiceID: chosen}, r0.Services)
	}
	calls = nil
	iDetail := add(detailRequest)
	iTVDB, iKey, iQuota, iUser := -1, -1, -1, -1
	if mediaType == "tv" && cfg.TMDBKey != "" {
		iTVDB = add(tvdbCall(cfg, externalID))
	}
	if svc != nil {
		iKey = add(secretGet(secretName(svc.ID)))
	}
	if !admin && acc.QuotaLimit > 0 {
		iQuota = add(quotaCall(c, c.userID(), acc.QuotaDays))
	}
	if cfg.NotifyAdmins {
		iUser = add(usersGet([]string{c.userID()}))
	}
	r1 := Round(c, calls, func(rs []hostResult) createR1 {
		out := createR1{Detail: parseDetails(mediaType, externalID, at(rs, iDetail)), Used: at(rs, iQuota).Count}
		if iTVDB >= 0 {
			out.TVDB = parseTVDB(rs[iTVDB])
		}
		if key := at(rs, iKey); key.OK {
			out.Key = key.Value
		}
		if users := at(rs, iUser).Users; len(users) > 0 {
			out.User = &users[0]
		}
		return out
	})
	if r1.Detail.Detail == nil {
		return nil, fail("%s", r1.Detail.Error)
	}
	det := r1.Detail.Detail
	if !admin && acc.QuotaLimit > 0 && r1.Used >= acc.QuotaLimit {
		return nil, fail("You have used all %d requests allowed every %d days.", acc.QuotaLimit, acc.QuotaDays)
	}

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
		taken := make([][]int, 0, len(r0.Existing))
		for _, existing := range r0.Existing {
			taken = append(taken, existing.Seasons)
		}
		seasons = remainingSeasons(wanted, taken, all)
		if len(seasons) == 0 {
			return nil, fail("Every chosen season has already been requested.")
		}
		if wanted == nil && len(seasons) == len(all) {
			seasons = nil
		}
	}

	row := requestRow{
		ID: newID(), UserID: c.userID(), Type: mediaType, Source: det.Source, ExternalID: externalID,
		Title: truncate(det.Title, 300), Year: yearPtr(det.Year), Seasons: seasons, Status: "pending",
	}
	if r1.TVDB > 0 {
		tvdb := r1.TVDB
		row.TVDB = &tvdb
	}
	if det.Poster != "" {
		poster := det.Poster
		row.Poster = &poster
	}
	if artist := truncate(det.Artist, 300); artist != "" {
		row.Artist = &artist
	}
	if admin {
		if chosenService != "" {
			row.ServiceID = &chosenService
		}
		row.QualityProfileID = qualityOverride
		if rootOverride != "" {
			row.RootFolder = &rootOverride
		}
	}

	// Round 3: the library check, and the server lookup when this request is approved right away.
	canSend := acc.AutoApprove && svc != nil && r1.Key != "" && (mediaType != "tv" || r1.TVDB > 0)
	var snd sender
	calls = nil
	iMatch, iLookup := -1, -1
	if mediaType != "tv" {
		iMatch = add(libraryMatch([]libraryCard{{Key: mediaType + ":" + externalID, Type: mediaType, Source: det.Source, ID: externalID, Title: det.Title, Year: yearPtr(det.Year)}}))
	}
	if canSend {
		snd = senderFor(row, *svc, r1.Key, r1.TVDB, qualityOverride, rootOverride)
		if lookup := snd.lookupCalls(); len(lookup) > 0 {
			iLookup = len(calls)
			calls = append(calls, lookup...)
		}
	}
	r2 := Round(c, calls, func(rs []hostResult) createR2 {
		out := createR2{}
		if match := at(rs, iMatch); match.OK {
			out.Match = match.Matches[mediaType+":"+externalID]
		}
		if iLookup >= 0 {
			out.Lookup = snd.parseLookup(rs[iLookup:])
		}
		return out
	})
	if r2.Match != "" {
		return nil, fail("This is already in the library.")
	}

	// Round 4: store the request and, when approved, send it. The result of that send is not stored (there is no
	// round left), so a request that was sent stays marked until the sync has seen it at the server.
	final := []hostCall{}
	var plan sendStep
	sent := false
	message := "Request submitted."
	if acc.AutoApprove {
		row.Status = "approved"
		message = "Request approved. It will be sent shortly."
		switch {
		case svc == nil:
			row.Status, row.Error, message = "failed", ptr(dispatchFailure(svcFailure)), "Request submitted, but it could not be sent: "+dispatchFailure(svcFailure)
		case r1.Key == "":
			failure := "The API key for this server is missing. Edit the server and enter it again."
			row.Status, row.Error, message = "failed", ptr(failure), "Request submitted, but it could not be sent: "+failure
		case !canSend:
			row.SendAttempts = ptr(0) // a show without a TVDB id: the sync looks it up and sends it
		default:
			plan = nextStep(snd, r2.Lookup, nil)
			switch {
			case plan.failed():
				failure := dispatchFailure(plan.Err)
				row.Status, row.Error, message = "failed", &failure, "Request submitted, but it could not be sent: "+failure
			case len(plan.Calls) > 0:
				row.SendAttempts = ptr(plan.Attempts)
				sent = true
			default:
				message = "Request approved and sent."
			}
		}
	}
	final = append(final, dbInsert(tableRequests, requestInsert(row, isoTime(c.now))))
	if cfg.NotifyAdmins {
		final = append(final, notifyAdmins("New request", displayName(r1.User)+" requested "+row.Title+".", pluginPagePath+"?tab=all&status=pending"))
	}
	applyAt := len(final)
	final = append(final, plan.Calls...)
	applied := Round(c, final, func(rs []hostResult) outcome {
		if !sent {
			return outcome{}
		}
		return snd.parseApply(applyPlan{Calls: plan.Calls}, rs[applyAt:])
	})
	if sent {
		if applied.OK {
			message = "Request approved and sent."
		} else {
			message = "Request approved. The server did not take it yet; it will be retried shortly."
		}
	}
	return done(message, ""), nil
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
		"processed_by": row.ProcessedBy, "tvdb_id": row.TVDB, "send_attempts": row.SendAttempts, "created_at": now, "updated_at": now,
	}
}

type approveR1 struct {
	Settings settingsR1   `json:"settings"`
	Request  *requestRow  `json:"request"`
	Services []serviceRow `json:"services"`
	Key      string       `json:"key"`
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
		var calls []hostCall
		add := func(call hostCall) int { calls = append(calls, call); return len(calls) - 1 }
		iSettings := add(settingsCalls()[0])
		add(settingsCalls()[1])
		add(settingsCalls()[2])
		iRequest := add(dbFind(tableRequests, []map[string]any{cond("id", "eq", id)}, nil, 1, 0))
		iServices := add(dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0))
		iSecret := -1
		if hint != "" {
			iSecret = add(secretGet(secretName(hint)))
		}
		r1 := Round(c, calls, func(rs []hostResult) approveR1 {
			out := approveR1{Settings: settingsFrom(rs, iSettings), Services: rowsOf[serviceRow](at(rs, iServices))}
			if rows := rowsOf[requestRow](at(rs, iRequest)); len(rows) > 0 {
				out.Request = &rows[0]
			}
			if secret := at(rs, iSecret); secret.OK {
				out.Key = secret.Value
			}
			return out
		})
		cfg := buildSettings(c, r1.Settings)
		tvdb := 0
		if req := r1.Request; req != nil && req.TVDB != nil {
			tvdb = *req.TVDB
		}
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
			failure = "No " + kind + " server is configured. Add one on the Servers tab."
		case svc.ID != hint:
			return nil, fail("The server for this request changed. Reload the page and try again.")
		case r1.Key == "":
			failure = "The API key for this server is missing. Edit the server and enter it again."
		}
		if failure == "" && req.Type == "tv" && tvdb == 0 {
			return approveWithoutTVDB(c, cfg, *req, *svc, r1.Key, quality, root)
		}

		var snd sender
		result := outcome{Err: failure}
		if failure == "" {
			info := infoOf(*req, tvdb)
			snd = sender{Conn: arrConn{Kind: kind, URL: svc.URL, Key: r1.Key}, Info: info, Target: targetOf(*svc, quality, root)}
			ld := Round(c, snd.lookupCalls(), snd.parseLookup)
			plan := snd.plan(ld)
			result = Round(c, plan.Calls, func(rs []hostResult) outcome { return snd.parseApply(plan, rs) })
		}

		set := map[string]any{"updated_at": isoTime(c.now), "processed_by": c.userID(), "decline_reason": nil, "send_attempts": nil}
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

// approveWithoutTVDB approves a show made before 1.1.0, which has no TVDB id yet. Finding it costs a round, so
// the approval is stored together with the server lookup, the send follows, and the sync confirms it later.
func approveWithoutTVDB(c *Ctx, cfg settings, req requestRow, svc serviceRow, key string, quality *int, root string) (any, error) {
	if cfg.TMDBKey == "" {
		return nil, fail("%s", noTMDBText)
	}
	tvdb := Round(c, []hostCall{tvdbCall(cfg, req.ExternalID)}, func(rs []hostResult) int { return parseTVDB(rs[0]) })
	if tvdb == 0 {
		return nil, fail("This show has no TVDB id, which Sonarr needs.")
	}
	snd := senderFor(req, svc, key, tvdb, quality, root)
	set := map[string]any{
		"status": "approved", "tvdb_id": tvdb, "send_attempts": 1, "error": nil, "decline_reason": nil, "service_id": svc.ID,
		"processed_by": c.userID(), "updated_at": isoTime(c.now),
	}
	if quality != nil {
		set["quality_profile_id"] = *quality
	}
	if root != "" {
		set["root_folder"] = root
	}
	where := []map[string]any{cond("id", "eq", req.ID)}
	lookup := append([]hostCall{dbUpdate(tableRequests, where, set)}, snd.lookupCalls()...)
	ld := Round(c, lookup, func(rs []hostResult) lookupData { return snd.parseLookup(rs[1:]) })
	plan := nextStep(snd, ld, ptr(0))
	final := []hostCall{}
	message, tone := "Request approved and sent.", ""
	switch {
	case plan.failed():
		final = append(final, dbUpdate(tableRequests, where, map[string]any{"status": "failed", "error": dispatchFailure(plan.Err), "send_attempts": nil, "updated_at": isoTime(c.now)}))
		message, tone = "The request could not be sent: "+dispatchFailure(plan.Err), "danger"
	case len(plan.Calls) == 0:
		final = append(final, dbUpdate(tableRequests, where, map[string]any{"send_attempts": nil, "updated_at": isoTime(c.now)}))
		final = append(final, plan.Follow...)
	default:
		final = append(final, plan.Calls...)
	}
	if cfg.NotifyUsers && !plan.failed() {
		final = append(final, notifyUser(req.UserID, "Request approved", req.Title+" was approved and is on its way.", pluginPagePath+"?tab=mine"))
	}
	applied := Round(c, final, func(rs []hostResult) outcome {
		if len(plan.Calls) == 0 {
			return outcome{OK: true}
		}
		return snd.parseApply(applyPlan{Calls: plan.Calls}, rs[:len(plan.Calls)])
	})
	if !applied.OK {
		message, tone = "Request approved. The server did not take it yet; it will be retried shortly.", "warning"
	}
	return done(message, tone), nil
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
	type declineR1 struct {
		Settings settingsR1   `json:"settings"`
		Rows     []requestRow `json:"rows"`
	}
	loaded := Round(c, append(settingsCalls(), dbFind(tableRequests, []map[string]any{cond("id", "eq", id)}, nil, 1, 0)), func(rs []hostResult) declineR1 {
		return declineR1{Settings: settingsFrom(rs, 0), Rows: rowsOf[requestRow](rs[3])}
	})
	cfg := buildSettings(c, loaded.Settings)
	rows := loaded.Rows
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
	syncQueue    = 3  // queued requests sent per run; each costs several host calls
	syncApproved = 97 // sent requests checked against the library per run
	syncCards    = 50 // library cards per match call
)

type syncR0 struct {
	Settings settingsR1   `json:"settings"`
	Queued   []requestRow `json:"queued"`
	Approved []requestRow `json:"approved"`
	Services []serviceRow `json:"services"`
}

type syncR1 struct {
	Keys    map[string]string `json:"keys"`
	TVDB    map[string]int    `json:"tvdb"`
	Matches map[string]string `json:"matches"`
}

// syncJob is one queued request on its way to a server.
type syncJob struct {
	Req     requestRow
	Svc     *serviceRow
	Key     string
	TVDB    int
	Failure string
	Lookup  lookupData
	Calls   int // lookup calls issued, 0 when none were possible
}

// opSync runs every few minutes as the system user. It sends requests that are approved but queued (or whose send
// has not been confirmed yet), then marks approved requests available once the title is in the library.
// The four host rounds are: read, keys + TVDB ids + library matches, server lookups, writes.
func opSync(c *Ctx) (any, error) {
	if c.role() != "system" {
		return nil, fail("Only the scheduler can run this.")
	}
	approved := []map[string]any{cond("status", "eq", "approved")}
	queuedWhere := append(append([]map[string]any{}, approved...), condNull("send_attempts", "notnull"))
	sentWhere := append(append([]map[string]any{}, approved...), condNull("send_attempts", "isnull"))
	r0 := Round(c, append(settingsCalls(),
		dbFind(tableRequests, queuedWhere, []order{{"created_at", "asc"}}, syncQueue, 0),
		dbFind(tableRequests, sentWhere, []order{{"created_at", "asc"}}, syncApproved, 0),
		dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0),
	), func(rs []hostResult) syncR0 {
		return syncR0{Settings: settingsFrom(rs, 0), Queued: rowsOf[requestRow](rs[3]), Approved: rowsOf[requestRow](rs[4]), Services: rowsOf[serviceRow](rs[5])}
	})
	cfg := buildSettings(c, r0.Settings)
	if len(r0.Queued) == 0 && len(r0.Approved) == 0 {
		return map[string]any{"changed": 0}, nil
	}

	jobs := make([]*syncJob, 0, len(r0.Queued))
	for _, req := range r0.Queued {
		job := &syncJob{Req: req, TVDB: tvdbOf(req)}
		job.Svc, job.Failure = serviceFor(req, r0.Services)
		jobs = append(jobs, job)
	}

	var calls []hostCall
	keyAt := map[string]int{}
	tvdbAt := map[string]int{}
	for _, job := range jobs {
		if job.Svc == nil {
			continue
		}
		if _, ok := keyAt[job.Svc.ID]; !ok {
			keyAt[job.Svc.ID] = len(calls)
			calls = append(calls, secretGet(secretName(job.Svc.ID)))
		}
		if job.Req.Type == "tv" && job.TVDB == 0 && cfg.TMDBKey != "" {
			tvdbAt[job.Req.ID] = len(calls)
			calls = append(calls, tvdbCall(cfg, job.Req.ExternalID))
		}
	}
	cards := make([]libraryCard, 0, len(r0.Queued)+len(r0.Approved))
	for _, req := range append(append([]requestRow{}, r0.Queued...), r0.Approved...) {
		cards = append(cards, libraryCard{Key: req.ID, Type: req.Type, Source: req.Source, ID: req.ExternalID, Title: req.Title, Year: req.Year})
	}
	matchFrom := len(calls)
	for start := 0; start < len(cards); start += syncCards {
		calls = append(calls, libraryMatch(cards[start:min(start+syncCards, len(cards))]))
	}
	r1 := Round(c, calls, func(rs []hostResult) syncR1 {
		out := syncR1{Keys: map[string]string{}, TVDB: map[string]int{}, Matches: map[string]string{}}
		for id, i := range keyAt {
			if r := at(rs, i); r.OK {
				out.Keys[id] = r.Value
			}
		}
		for id, i := range tvdbAt {
			out.TVDB[id] = parseTVDB(at(rs, i))
		}
		for i := matchFrom; i < len(rs); i++ {
			for key, value := range rs[i].Matches {
				out.Matches[key] = value
			}
		}
		return out
	})

	calls = nil
	senders := map[string]sender{}
	for _, job := range jobs {
		job.Key = r1.Keys[serviceIDOf(job.Svc)]
		if job.TVDB == 0 {
			job.TVDB = r1.TVDB[job.Req.ID]
		}
		if r1.Matches[job.Req.ID] != "" || job.Svc == nil {
			continue
		}
		switch {
		case job.Key == "":
			job.Failure = "The API key for this server is missing. Edit the server and enter it again."
		case job.Req.Type == "tv" && job.TVDB == 0 && cfg.TMDBKey == "":
			job.Failure = noTMDBText
		case job.Req.Type == "tv" && job.TVDB == 0:
			job.Failure = "This show has no TVDB id, which Sonarr needs."
		default:
			snd := senderFor(job.Req, *job.Svc, job.Key, job.TVDB, job.Req.QualityProfileID, str(job.Req.RootFolder))
			senders[job.Req.ID] = snd
			if lookup := snd.lookupCalls(); len(lookup) > 0 {
				job.Calls = len(lookup)
				calls = append(calls, lookup...)
			} else {
				job.Failure = snd.preflight()
			}
		}
	}
	lookups := Round(c, calls, func(rs []hostResult) map[string]lookupData {
		out := map[string]lookupData{}
		next := 0
		for _, job := range jobs {
			snd, ok := senders[job.Req.ID]
			if !ok || job.Calls == 0 {
				continue
			}
			out[job.Req.ID] = snd.parseLookup(rs[next : next+job.Calls])
			next += job.Calls
		}
		return out
	})

	final := []hostCall{}
	changed := 0
	now := isoTime(c.now)
	for _, job := range jobs {
		where := []map[string]any{cond("id", "eq", job.Req.ID), cond("status", "eq", "approved")}
		set := map[string]any{"updated_at": now}
		var group []hostCall
		switch snd, sending := senders[job.Req.ID]; {
		case r1.Matches[job.Req.ID] != "":
			set["status"], set["send_attempts"] = "available", nil
			group = []hostCall{dbUpdate(tableRequests, where, set)}
			if cfg.NotifyUsers {
				group = append(group, notifyUser(job.Req.UserID, "Now available", job.Req.Title+" is now in your library.", "/media/"+r1.Matches[job.Req.ID]))
			}
		case job.Failure != "" || !sending:
			failure := job.Failure
			if failure == "" {
				failure = "The request could not be sent."
			}
			set["status"], set["error"], set["send_attempts"] = "failed", dispatchFailure(failure), nil
			group = []hostCall{dbUpdate(tableRequests, where, set)}
		default:
			if job.TVDB > 0 && job.Req.TVDB == nil {
				set["tvdb_id"] = job.TVDB
			}
			step := nextStep(snd, lookups[job.Req.ID], job.Req.SendAttempts)
			switch {
			case step.failed():
				set["status"], set["error"], set["send_attempts"] = "failed", dispatchFailure(step.Err), nil
				group = []hostCall{dbUpdate(tableRequests, where, set)}
			case len(step.Calls) > 0:
				set["send_attempts"] = step.Attempts
				group = append([]hostCall{dbUpdate(tableRequests, where, set)}, step.Calls...)
			default:
				set["send_attempts"], set["error"] = nil, nil
				group = append([]hostCall{dbUpdate(tableRequests, where, set)}, step.Follow...)
			}
		}
		if len(final)+len(group) > maxCallsPerRound {
			continue // left for the next run
		}
		final = append(final, group...)
		changed++
	}

	matched := []requestRow{}
	for _, req := range r0.Approved {
		if r1.Matches[req.ID] != "" {
			matched = append(matched, req)
		}
	}
	if room := maxCallsPerRound - len(final); cfg.NotifyUsers && len(matched) > room-1 {
		matched = matched[:max(room-1, 0)]
	} else if room < 1 {
		matched = nil
	}
	if len(matched) > 0 {
		ids := make([]string, 0, len(matched))
		for _, req := range matched {
			ids = append(ids, req.ID)
		}
		final = append(final, dbUpdate(tableRequests, []map[string]any{cond("id", "in", ids), cond("status", "eq", "approved")},
			map[string]any{"status": "available", "updated_at": now}))
		if cfg.NotifyUsers {
			for _, req := range matched {
				final = append(final, notifyUser(req.UserID, "Now available", req.Title+" is now in your library.", "/media/"+r1.Matches[req.ID]))
			}
		}
		changed += len(matched)
	}
	Round(c, final, func([]hostResult) struct{} { return struct{}{} })
	return map[string]any{"changed": changed}, nil
}

func serviceIDOf(svc *serviceRow) string {
	if svc == nil {
		return ""
	}
	return svc.ID
}
