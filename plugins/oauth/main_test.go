package main

import (
	"net/url"
	"testing"
)

func TestAuthStartBuildsGitHubPKCEAuthorizationURL(t *testing.T) {
	input := request{Method: "auth-start", Settings: map[string]any{"clientId": "test-client"}}
	input.Request.Body = map[string]any{"state": "state-value", "codeChallenge": "challenge-value", "redirectUri": "https://flixer.example/auth/provider-callback"}
	result := response(input)
	value, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected authorization result, got %#v", result)
	}
	redirect, err := url.Parse(text(value["authorizationUrl"]))
	if err != nil {
		t.Fatal(err)
	}
	query := redirect.Query()
	for key, expected := range map[string]string{
		"client_id": "test-client", "redirect_uri": "https://flixer.example/auth/provider-callback", "response_type": "code",
		"scope": "read:user", "state": "state-value", "code_challenge": "challenge-value", "code_challenge_method": "S256",
	} {
		if query.Get(key) != expected {
			t.Errorf("%s = %q, want %q", key, query.Get(key), expected)
		}
	}
}

func TestAuthCompleteExchangesCodeAndReturnsStableGitHubIdentity(t *testing.T) {
	input := request{Method: "auth-complete", Settings: map[string]any{"clientId": "test-client", "clientSecret": "dummy-secret"}}
	input.Request.Body = map[string]any{"code": "test-code", "codeVerifier": "test-verifier", "redirectUri": "https://flixer.example/auth/provider-callback"}
	tokenCall := response(input)
	calls, ok := tokenCall["hostCalls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("expected one token host call, got %#v", tokenCall)
	}
	call := calls[0].(map[string]any)
	if call["url"] != "https://github.com/login/oauth/access_token" || call["method"] != "POST" {
		t.Fatalf("unexpected token request: %#v", call)
	}
	headers := call["headers"].(map[string]string)
	if headers["content-type"] != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected token request content type: %#v", headers)
	}
	form, err := url.ParseQuery(call["body"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("client_secret") != "dummy-secret" || form.Get("code_verifier") != "test-verifier" {
		t.Fatalf("token request omitted credentials or PKCE verifier: %#v", form)
	}

	input.Continuation.Step = "token"
	input.HostResults = []hostResult{{ID: "token", OK: true, Body: `{"access_token":"dummy-access-token"}`}}
	userCall := response(input)
	calls = userCall["hostCalls"].([]any)
	userRequest := calls[0].(map[string]any)
	userHeaders := userRequest["headers"].(map[string]string)
	if userRequest["url"] != "https://api.github.com/user" || userHeaders["authorization"] != "Bearer dummy-access-token" {
		t.Fatalf("unexpected GitHub identity request: %#v", userRequest)
	}

	input.Continuation.Step = "user"
	input.HostResults = []hostResult{{ID: "user", OK: true, Body: `{"id":123456,"login":"test-user"}`}}
	identity := response(input)["result"].(map[string]any)
	if identity["authenticated"] != true || identity["issuer"] != "https://github.com" || identity["subject"] != "123456" {
		t.Fatalf("unexpected external identity: %#v", identity)
	}
}

func TestAuthCompleteFailsClosedForFailedHostCallOrInvalidIdentity(t *testing.T) {
	input := request{Method: "auth-complete"}
	input.Continuation.Step = "token"
	input.HostResults = []hostResult{{ID: "token", OK: false, Error: "request_failed"}}
	if _, ok := response(input)["error"]; !ok {
		t.Fatal("expected failed token exchange to be rejected")
	}
	input.Continuation.Step = "user"
	input.HostResults = []hostResult{{ID: "user", OK: true, Body: `{"id":{"value":"invalid"}}`}}
	if _, ok := response(input)["error"]; !ok {
		t.Fatal("expected malformed GitHub identity to be rejected")
	}
}

func TestConfigurableOAuthProvider(t *testing.T) {
	input := request{Method: "auth-start", Settings: map[string]any{
		"clientId": "client", "clientSecret": "secret", "authorizationUrl": "https://login.example.com/authorize",
		"tokenUrl": "https://login.example.com/token", "userInfoUrl": "https://login.example.com/userinfo",
		"scope": "profile", "issuer": "https://login.example.com", "subjectField": "sub",
	}}
	input.Request.Body = map[string]any{"state": "state", "codeChallenge": "challenge", "redirectUri": "https://flixer.example/auth/provider-callback", "code": "code", "codeVerifier": "verifier"}
	authorization, err := url.Parse(text(response(input)["result"].(map[string]any)["authorizationUrl"]))
	if err != nil || authorization.Host != "login.example.com" || authorization.Query().Get("scope") != "profile" {
		t.Fatalf("unexpected authorization URL: %v", authorization)
	}
	input.Method = "auth-complete"
	if call := response(input)["hostCalls"].([]any)[0].(map[string]any); call["url"] != "https://login.example.com/token" {
		t.Fatalf("unexpected token request: %#v", call)
	}
	input.Continuation.Step = "token"
	input.HostResults = []hostResult{{ID: "token", OK: true, Body: `{"access_token":"token"}`}}
	if call := response(input)["hostCalls"].([]any)[0].(map[string]any); call["url"] != "https://login.example.com/userinfo" {
		t.Fatalf("unexpected identity request: %#v", call)
	}
	input.Continuation.Step = "user"
	input.HostResults = []hostResult{{ID: "user", OK: true, Body: `{"sub":"stable-subject"}`}}
	identity := response(input)["result"].(map[string]any)
	if identity["issuer"] != "https://login.example.com" || identity["subject"] != "stable-subject" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestTokenAuthenticationMethods(t *testing.T) {
	input := request{Method: "auth-complete", Settings: map[string]any{"clientId": "client", "clientSecret": "secret", "tokenAuthMethod": "client_secret_basic"}}
	input.Request.Body = map[string]any{"code": "code", "codeVerifier": "verifier", "redirectUri": "https://flixer.example/auth/provider-callback"}
	call := response(input)["hostCalls"].([]any)[0].(map[string]any)
	if call["headers"].(map[string]string)["authorization"] != "Basic Y2xpZW50OnNlY3JldA==" {
		t.Fatal("expected HTTP Basic client authentication")
	}
	form, err := url.ParseQuery(call["body"].(string))
	if err != nil || form.Get("client_secret") != "" {
		t.Fatal("client secret must not appear in the form for HTTP Basic authentication")
	}
	input.Settings["tokenAuthMethod"] = "none"
	delete(input.Settings, "clientSecret")
	if _, ok := response(input)["hostCalls"]; !ok {
		t.Fatal("expected public PKCE client to be supported")
	}
}
