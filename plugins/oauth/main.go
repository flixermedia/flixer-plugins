package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
)

type hostResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Body  string `json:"body"`
	Error string `json:"error"`
}

type request struct {
	Method  string `json:"method"`
	Request struct {
		Body map[string]any `json:"body"`
	} `json:"request"`
	Settings     map[string]any `json:"settings"`
	Continuation struct {
		Step string `json:"step"`
	} `json:"continuation"`
	HostResults []hostResult `json:"hostResults"`
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func setting(input request, name, fallback string) string {
	if value := text(input.Settings[name]); value != "" {
		return value
	}
	return fallback
}

func failed() map[string]any {
	return map[string]any{"error": map[string]any{"code": "authentication_failed", "message": "OAuth authentication failed."}}
}

func hostResultFor(input request, id string) (hostResult, bool) {
	for _, result := range input.HostResults {
		if result.ID == id {
			return result, result.OK && result.Error == ""
		}
	}
	return hostResult{}, false
}

func authStart(input request) map[string]any {
	clientID := text(input.Settings["clientId"])
	state := text(input.Request.Body["state"])
	challenge := text(input.Request.Body["codeChallenge"])
	redirectURI := text(input.Request.Body["redirectUri"])
	if clientID == "" || state == "" || challenge == "" || redirectURI == "" {
		return failed()
	}
	endpoint, err := url.Parse(setting(input, "authorizationUrl", "https://github.com/login/oauth/authorize"))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return failed()
	}
	query := endpoint.Query()
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", setting(input, "scope", "read:user"))
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	endpoint.RawQuery = query.Encode()
	return map[string]any{"result": map[string]any{"authorizationUrl": endpoint.String()}}
}

func authComplete(input request) map[string]any {
	switch input.Continuation.Step {
	case "":
		clientID := text(input.Settings["clientId"])
		clientSecret := text(input.Settings["clientSecret"])
		code := text(input.Request.Body["code"])
		codeVerifier := text(input.Request.Body["codeVerifier"])
		redirectURI := text(input.Request.Body["redirectUri"])
		if clientID == "" || code == "" || codeVerifier == "" || redirectURI == "" {
			return failed()
		}
		form := url.Values{}
		headers := map[string]string{"accept": "application/json", "content-type": "application/x-www-form-urlencoded"}
		switch setting(input, "tokenAuthMethod", "client_secret_post") {
		case "client_secret_post":
			if clientSecret == "" {
				return failed()
			}
			form.Set("client_id", clientID)
			form.Set("client_secret", clientSecret)
		case "client_secret_basic":
			if clientSecret == "" {
				return failed()
			}
			headers["authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(clientID)+":"+url.QueryEscape(clientSecret)))
		case "none":
			form.Set("client_id", clientID)
		default:
			return failed()
		}
		form.Set("code", code)
		form.Set("code_verifier", codeVerifier)
		form.Set("redirect_uri", redirectURI)
		return map[string]any{
			"hostCalls": []any{map[string]any{
				"id": "token", "op": "http.request", "url": setting(input, "tokenUrl", "https://github.com/login/oauth/access_token"), "method": "POST",
				"headers": headers,
				"body":    form.Encode(),
			}},
			"continuation": map[string]string{"step": "token"},
		}
	case "token":
		result, ok := hostResultFor(input, "token")
		if !ok {
			return failed()
		}
		var token struct {
			AccessToken string `json:"access_token"`
		}
		if json.Unmarshal([]byte(result.Body), &token) != nil || token.AccessToken == "" {
			return failed()
		}
		return map[string]any{
			"hostCalls": []any{map[string]any{
				"id": "user", "op": "http.request", "url": setting(input, "userInfoUrl", "https://api.github.com/user"), "method": "GET",
				"headers": map[string]string{"accept": "application/json", "authorization": "Bearer " + token.AccessToken},
			}},
			"continuation": map[string]string{"step": "user"},
		}
	case "user":
		result, ok := hostResultFor(input, "user")
		if !ok {
			return failed()
		}
		var user map[string]any
		decoder := json.NewDecoder(bytes.NewReader([]byte(result.Body)))
		decoder.UseNumber()
		if decoder.Decode(&user) != nil {
			return failed()
		}
		var subject string
		switch value := user[setting(input, "subjectField", "id")].(type) {
		case string:
			subject = value
		case json.Number:
			if _, err := strconv.ParseInt(string(value), 10, 64); err != nil {
				return failed()
			}
			subject = string(value)
		}
		if subject == "" {
			return failed()
		}
		return map[string]any{"result": map[string]any{"authenticated": true, "issuer": setting(input, "issuer", "https://github.com"), "subject": subject}}
	default:
		return failed()
	}
}

func response(input request) map[string]any {
	switch input.Method {
	case "auth-start":
		return authStart(input)
	case "auth-complete":
		return authComplete(input)
	default:
		return map[string]any{"error": map[string]any{"code": "not_found", "message": "Unsupported operation."}}
	}
}

func main() {
	var input request
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response(input)); err != nil {
		os.Exit(1)
	}
}
