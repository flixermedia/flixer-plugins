package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRunReturnsGreetingAndScopedCounters(t *testing.T) {
	input := `{
		"apiVersion": 1,
		"operation": "hello",
		"user": {"id": "user-1", "role": "user"},
		"request": {"method": "POST", "body": {"name": " Ada "}},
		"storage": {
			"plugin": {"invocations": 4},
			"user": {"invocations": 1}
		}
	}`
	var output bytes.Buffer
	if err := run(bytes.NewBufferString(input), &output); err != nil {
		t.Fatal(err)
	}

	var got response
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.Message != "Hello, Ada!" || got.Result.PluginInvocationCount != 5 || got.Result.UserInvocationCount != 2 {
		t.Fatalf("unexpected result: %+v", got.Result)
	}
	if got.Storage.Plugin.Set["invocations"] != 5 || got.Storage.User.Set["invocations"] != 2 {
		t.Fatalf("unexpected storage patch: %+v", got.Storage)
	}
}

func TestRunRejectsUnknownOperation(t *testing.T) {
	input := `{"apiVersion":1,"operation":"other","user":{"id":"user-1"}}`
	if err := run(bytes.NewBufferString(input), &bytes.Buffer{}); err == nil {
		t.Fatal("expected unsupported operation error")
	}
}

func TestRunRejectsOversizedName(t *testing.T) {
	name, _ := json.Marshal(string(bytes.Repeat([]byte("a"), 81)))
	input := `{"apiVersion":1,"operation":"hello","user":{"id":"user-1"},"request":{"body":{"name":` + string(name) + `}}}`
	if err := run(bytes.NewBufferString(input), &bytes.Buffer{}); err == nil {
		t.Fatal("expected name validation error")
	}
}

func TestFetchRequestsHostCallThenReportsResult(t *testing.T) {
	first := `{"apiVersion":1,"operation":"fetch","user":{"id":"user-1"}}`
	var out bytes.Buffer
	if err := run(bytes.NewBufferString(first), &out); err != nil {
		t.Fatal(err)
	}
	var call hostCallResponse
	if err := json.Unmarshal(out.Bytes(), &call); err != nil {
		t.Fatal(err)
	}
	if len(call.HostCalls) != 1 || call.HostCalls[0].Op != "http.get" || call.HostCalls[0].URL != "https://example.com/" {
		t.Fatalf("unexpected host calls: %+v", call.HostCalls)
	}

	second := `{"apiVersion":1,"operation":"fetch","user":{"id":"user-1"},"continuation":{"stage":1},"hostResults":[{"id":"page","ok":true,"status":200,"body":"hello"}]}`
	out.Reset()
	if err := run(bytes.NewBufferString(second), &out); err != nil {
		t.Fatal(err)
	}
	var done fetchResponse
	if err := json.Unmarshal(out.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	if !done.Result.OK || done.Result.Status != 200 || done.Result.Bytes != 5 {
		t.Fatalf("unexpected result: %+v", done.Result)
	}
}
