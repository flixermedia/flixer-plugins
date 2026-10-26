package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runJSON(t *testing.T, input string) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := run(strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestEpisodeLookupBecomesHostPostWithSeasonAndEpisode(t *testing.T) {
	out := runJSON(t, `{"apiVersion":1,"operation":"lookup","request":{"body":{"baseUrl":"https://db.example/v3/","item":{"type":"episode","durationMs":1320000,"providerIds":{"tmdb":"42","imdb":"tt1"},"season":1,"episode":0}}}}`)
	var got hostCallResponse
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	call := got.HostCalls[0]
	if call.Op != "http.post" || call.URL != "https://db.example/v3/movies" {
		t.Fatalf("unexpected call: %+v", call)
	}
	if call.Body != `[{"tmdb_id":42,"imdb_id":"tt1","season":1,"episode":0,"duration_ms":1320000}]` {
		t.Fatalf("unexpected body: %s", call.Body)
	}
}

func TestLookupRejectsInsecureUrlAndMissingIds(t *testing.T) {
	for _, input := range []string{
		`{"apiVersion":1,"operation":"lookup","request":{"body":{"baseUrl":"http://db.example","item":{"type":"movie","providerIds":{"tmdb":"1"}}}}}`,
		`{"apiVersion":1,"operation":"lookup","request":{"body":{"baseUrl":"https://db.example","item":{"type":"movie","providerIds":{"tmdb":"abc"}}}}}`,
	} {
		if err := run(strings.NewReader(input), &bytes.Buffer{}); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

func TestResultPicksMostSubmittedMatchingDurationAndClearsOthers(t *testing.T) {
	reply := `[{"intro":[{"start_ms":7000.4,"end_ms":8000,"duration_ms":1320000,"submissions":5},{"start_ms":1,"end_ms":2,"duration_ms":1320000,"submissions":2},{"start_ms":9,"end_ms":99,"duration_ms":5000000,"submissions":99}],"credits":[{"start_ms":10,"end_ms":null}]}]`
	body, _ := json.Marshal(reply)
	input := `{"apiVersion":1,"operation":"lookup","request":{"body":{"item":{"durationMs":1320500}}},"hostResults":[{"id":"skipme","ok":true,"status":200,"body":` + string(body) + `}]}`
	var got resultResponse
	if err := json.Unmarshal(runJSON(t, input), &got); err != nil {
		t.Fatal(err)
	}
	intro := got.Result.Segments["intro"]
	if intro == nil || intro.StartMs != 7000 || intro.EndMs != 8000 || intro.Submissions != 5 {
		t.Fatalf("unexpected intro: %+v", intro)
	}
	for _, kind := range []string{"recap", "credits", "preview"} {
		value, present := got.Result.Segments[kind]
		if !present || value != nil {
			t.Fatalf("%s should be present and null, got %+v", kind, value)
		}
	}
}

func TestFailedOrEmptyReplyLeavesStoredDataUntouched(t *testing.T) {
	for _, result := range []string{
		`{"id":"skipme","ok":false,"error":"request_failed"}`,
		`{"id":"skipme","ok":true,"status":200,"body":"[null]"}`,
		`{"id":"skipme","ok":true,"status":200,"body":"nope"}`,
	} {
		var got resultResponse
		input := `{"apiVersion":1,"operation":"lookup","request":{"body":{"item":{"durationMs":1}}},"hostResults":[` + result + `]}`
		if err := json.Unmarshal(runJSON(t, input), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Result.Segments) != 0 {
			t.Fatalf("expected no changes, got %+v", got.Result.Segments)
		}
	}
}
