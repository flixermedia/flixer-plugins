package main

// A request that is approved but not yet confirmed at its server carries send_attempts: 0 means it was never
// tried, 1 or more means a send went out whose result nobody has checked. The scheduled sync looks such
// requests up at the server and either clears the marker (the server has it), sends again, or marks the
// request failed. Requests approved before 1.1.0 have no marker and count as sent.
const maxSendAttempts = 3

// serviceFor picks the server a request goes to; the text is why there is none.
func serviceFor(req requestRow, services []serviceRow) (*serviceRow, string) {
	kind := kindOfType[req.Type]
	svc, err := resolveService(services, kind, str(req.ServiceID))
	if err != nil {
		return nil, err.Error()
	}
	if svc == nil {
		return nil, "No " + kind + " server is configured. Add one on the Servers tab."
	}
	return svc, ""
}

func senderFor(req requestRow, svc serviceRow, key string, tvdb int, quality *int, root string) sender {
	return sender{Conn: arrConn{Kind: svc.Kind, URL: svc.URL, Key: key}, Info: infoOf(req, tvdb), Target: targetOf(svc, quality, root)}
}

func tvdbOf(req requestRow) int {
	if req.TVDB != nil {
		return *req.TVDB
	}
	return 0
}

// sendStep is what to do for a request after its server was looked up.
type sendStep struct {
	Err      string
	Calls    []hostCall // writes to run now
	Follow   []hostCall // searches that are safe to run once the item exists
	Attempts int        // send_attempts to store when Calls run
}

func (s sendStep) failed() bool { return s.Err != "" }

// nextStep decides what a lookup leads to. attempts is the request's current send_attempts, nil when it has none.
func nextStep(snd sender, ld lookupData, attempts *int) sendStep {
	if ld.Err != "" {
		return sendStep{Err: ld.Err}
	}
	tried := 0
	if attempts != nil {
		tried = *attempts
	}
	plan := snd.plan(ld)
	if tried >= 1 {
		if ld.Existing != nil {
			return sendStep{Follow: plan.Follow}
		}
		if tried >= maxSendAttempts {
			return sendStep{Err: "The server did not confirm the request after several tries. Check it, then use Retry."}
		}
	}
	if plan.Err != "" {
		return sendStep{Err: plan.Err}
	}
	if len(plan.Calls) == 0 {
		return sendStep{Follow: plan.Follow}
	}
	return sendStep{Calls: plan.Calls, Attempts: tried + 1}
}
