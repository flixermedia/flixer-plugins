package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

var operations = map[string]func(c *Ctx) (any, error){
	"page":            opPage,
	"request":         opRequest,
	"cancel":          opCancel,
	"approve":         opApprove(false),
	"retry":           opApprove(true),
	"decline":         opDecline,
	"service-save":    opServiceSave,
	"service-delete":  opServiceDelete,
	"service-test":    opServiceTest,
	"user-limit-save": opUserLimitSave,
	"sync":            opSync,
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	var env envelope
	if err := json.NewDecoder(io.LimitReader(input, 4<<20)).Decode(&env); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if env.APIVersion != 1 {
		return errors.New("unsupported plugin API version")
	}
	handler, ok := operations[env.Operation]
	if !ok {
		return errors.New("unsupported operation")
	}
	out, err := execute(&env, handler)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(out)
}
