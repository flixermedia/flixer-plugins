// Command skipmedb is the flixer.skipmedb plugin: it looks up crowd-sourced
// intro, recap, credits and preview timestamps from a SkipMe.db compatible API.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	durationToleranceMs = 5000
	hostCallID          = "skipme"
)

var segmentTypes = []string{"intro", "recap", "credits", "preview"}

var digits = regexp.MustCompile(`^\d+$`)

type envelope struct {
	APIVersion int    `json:"apiVersion"`
	Operation  string `json:"operation"`
	Request    struct {
		Body struct {
			Item    item   `json:"item"`
			BaseURL string `json:"baseUrl"`
		} `json:"body"`
	} `json:"request"`
	HostResults []hostResult `json:"hostResults"`
}

type item struct {
	Type        string            `json:"type"`
	DurationMs  int64             `json:"durationMs"`
	ProviderIDs map[string]string `json:"providerIds"`
	Season      *int              `json:"season"`
	Episode     *int              `json:"episode"`
}

type hostResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Body   string `json:"body"`
	Error  string `json:"error"`
}

type hostCall struct {
	ID   string `json:"id"`
	Op   string `json:"op"`
	URL  string `json:"url"`
	Body string `json:"body"`
}

type hostCallResponse struct {
	HostCalls []hostCall `json:"hostCalls"`
}

type lookupRequest struct {
	TmdbID     int64  `json:"tmdb_id,omitempty"`
	TvdbID     int64  `json:"tvdb_id,omitempty"`
	ImdbID     string `json:"imdb_id,omitempty"`
	Season     *int   `json:"season,omitempty"`
	Episode    *int   `json:"episode,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

type timestamp struct {
	StartMs     *float64 `json:"start_ms"`
	EndMs       *float64 `json:"end_ms"`
	DurationMs  float64  `json:"duration_ms"`
	Submissions float64  `json:"submissions"`
}

type segment struct {
	StartMs     int64 `json:"startMs"`
	EndMs       int64 `json:"endMs"`
	Submissions int64 `json:"submissions"`
}

type resultResponse struct {
	Result struct {
		Segments map[string]*segment `json:"segments"`
	} `json:"result"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	var request envelope
	if err := json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if request.APIVersion != 1 {
		return errors.New("unsupported plugin API version")
	}
	if request.Operation != "lookup" {
		return errors.New("unsupported operation")
	}

	encoder := json.NewEncoder(output)
	if len(request.HostResults) == 0 {
		call, err := buildHostCall(request)
		if err != nil {
			return err
		}
		return encoder.Encode(hostCallResponse{HostCalls: []hostCall{call}})
	}
	return encoder.Encode(buildResult(request.Request.Body.Item.DurationMs, request.HostResults[0]))
}

func buildHostCall(request envelope) (hostCall, error) {
	body := request.Request.Body
	base := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return hostCall{}, errors.New("baseUrl must be an https URL")
	}
	if body.Item.Type != "movie" && body.Item.Type != "episode" {
		return hostCall{}, errors.New("item type must be movie or episode")
	}

	lookup := lookupRequest{DurationMs: body.Item.DurationMs, ImdbID: body.Item.ProviderIDs["imdb"]}
	if value := body.Item.ProviderIDs["tmdb"]; digits.MatchString(value) {
		lookup.TmdbID, _ = strconv.ParseInt(value, 10, 64)
	}
	if value := body.Item.ProviderIDs["tvdb"]; digits.MatchString(value) {
		lookup.TvdbID, _ = strconv.ParseInt(value, 10, 64)
	}
	if lookup.TmdbID == 0 && lookup.TvdbID == 0 && lookup.ImdbID == "" {
		return hostCall{}, errors.New("item has no provider ids")
	}
	if body.Item.Type == "episode" {
		lookup.Season, lookup.Episode = body.Item.Season, body.Item.Episode
	}

	payload, err := json.Marshal([]lookupRequest{lookup})
	if err != nil {
		return hostCall{}, err
	}
	return hostCall{ID: hostCallID, Op: "http.post", URL: base + "/movies", Body: string(payload)}, nil
}

// buildResult maps the service reply to per-type segments. A failed or empty
// reply leaves stored data untouched; a reply without a usable entry clears it.
func buildResult(durationMs int64, result hostResult) resultResponse {
	var out resultResponse
	out.Result.Segments = map[string]*segment{}
	if !result.OK {
		return out
	}

	var payload []map[string][]timestamp
	if err := json.Unmarshal([]byte(result.Body), &payload); err != nil || len(payload) == 0 || payload[0] == nil {
		return out
	}
	for _, kind := range segmentTypes {
		out.Result.Segments[kind] = pick(payload[0][kind], durationMs)
	}
	return out
}

// pick chooses the entry recorded against a file of about the same length,
// favouring the most submissions.
func pick(entries []timestamp, durationMs int64) *segment {
	var best *timestamp
	for index := range entries {
		entry := &entries[index]
		if entry.StartMs == nil || entry.EndMs == nil || *entry.EndMs <= *entry.StartMs {
			continue
		}
		if durationMs > 0 && entry.DurationMs != 0 && math.Abs(entry.DurationMs-float64(durationMs)) > durationToleranceMs {
			continue
		}
		if best == nil || entry.Submissions > best.Submissions {
			best = entry
		}
	}
	if best == nil {
		return nil
	}
	return &segment{
		StartMs:     int64(math.Round(*best.StartMs)),
		EndMs:       int64(math.Round(*best.EndMs)),
		Submissions: int64(best.Submissions),
	}
}
