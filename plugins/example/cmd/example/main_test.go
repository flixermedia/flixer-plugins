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
