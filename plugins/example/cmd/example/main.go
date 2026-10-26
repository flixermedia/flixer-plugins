package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type requestEnvelope struct {
	APIVersion int    `json:"apiVersion"`
	Method     string `json:"method"`
	Operation  string `json:"operation"`
	User       struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"user"`
	Request struct {
		Method string `json:"method"`
		Body   struct {
			Name string `json:"name"`
		} `json:"body"`
	} `json:"request"`
	Storage map[string]map[string]json.RawMessage `json:"storage"`
}

type response struct {
	Result  result          `json:"result"`
	Storage storageResponse `json:"storage"`
}

type result struct {
	Message               string `json:"message"`
	Operation             string `json:"operation"`
	Authenticated         bool   `json:"authenticated"`
	PluginInvocationCount int    `json:"pluginInvocationCount"`
	UserInvocationCount   int    `json:"userInvocationCount"`
}

type storageResponse struct {
	Plugin storagePatch `json:"plugin"`
	User   storagePatch `json:"user"`
}

type storagePatch struct {
	Set map[string]int `json:"set"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	decoder.DisallowUnknownFields()
	var request requestEnvelope
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if request.APIVersion != 1 {
		return errors.New("unsupported plugin API version")
	}
	if request.Operation != "hello" {
		return errors.New("unsupported operation")
	}
	if request.User.ID == "" {
		return errors.New("authenticated user is missing")
	}

	pluginCount, err := nextCount(request.Storage["plugin"], "invocations")
	if err != nil {
		return fmt.Errorf("read plugin count: %w", err)
	}
	userCount, err := nextCount(request.Storage["user"], "invocations")
	if err != nil {
		return fmt.Errorf("read user count: %w", err)
	}

	name := strings.TrimSpace(request.Request.Body.Name)
	if len(name) > 80 {
		return errors.New("name must be 80 characters or fewer")
	}
	if name == "" {
		name = "there"
	}

	out := response{
		Result: result{
			Message:               "Hello, " + name + "!",
			Operation:             request.Operation,
			Authenticated:         true,
			PluginInvocationCount: pluginCount,
			UserInvocationCount:   userCount,
		},
		Storage: storageResponse{
			Plugin: storagePatch{Set: map[string]int{"invocations": pluginCount}},
			User:   storagePatch{Set: map[string]int{"invocations": userCount}},
		},
	}
	encoder := json.NewEncoder(output)
	if err := encoder.Encode(out); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}

func nextCount(storage map[string]json.RawMessage, key string) (int, error) {
	raw, ok := storage[key]
	if !ok {
		return 1, nil
	}
	var current int
	if err := json.Unmarshal(raw, &current); err != nil || current < 0 {
		return 0, errors.New("stored counter is invalid")
	}
	if current == int(^uint(0)>>1) {
		return 0, errors.New("stored counter reached its maximum")
	}
	return current + 1, nil
}
