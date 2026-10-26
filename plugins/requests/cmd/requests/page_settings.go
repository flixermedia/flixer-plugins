package main

import (
	"fmt"
	"strings"
)

func serversTab() nav { return nav{Query: map[string]string{"tab": "services"}} }

func providerField(name, label, help string, saved bool) []field {
	return []field{
		{Name: name, Label: label, Type: "password", HasValue: saved, Help: help, Placeholder: "Paste to replace the saved value"},
		{Name: "clear" + strings.ToUpper(name[:1]) + name[1:], Label: "Remove the saved " + strings.ToLower(label), Type: "switch", Value: false, Help: "Turn on and save to delete it. Leave off to keep it."},
	}
}

// settingsContent is the administrators' configuration page.
func settingsContent(c *Ctx, cfg settings) []node {
	if !c.isAdmin() {
		return []node{alert("Only administrators can change settings.", "danger")}
	}
	services := Round(c, []hostCall{dbFind(tableServices, nil, []order{{"kind", "asc"}, {"name", "asc"}}, 200, 0)},
		func(rs []hostResult) serviceListR1 { return serviceListR1{Services: rowsOf[serviceRow](rs[0])} }).Services

	general := form([]field{
		{Name: "moviesEnabled", Label: "Movies", Type: "switch", Value: cfg.Movies, Help: "Let people request movies (needs a Radarr server)."},
		{Name: "tvEnabled", Label: "Series", Type: "switch", Value: cfg.TV, Help: "Let people request TV series (needs a Sonarr server)."},
		{Name: "musicEnabled", Label: "Music", Type: "switch", Value: cfg.Music, Help: "Let people request albums (needs a Lidarr server)."},
		{Name: "defaultCanRequest", Label: "Users can request by default", Type: "switch", Value: cfg.DefaultCanRequest, Help: "Individual users can be allowed or blocked on the Users tab."},
		{Name: "autoApprove", Label: "Auto-approve requests", Type: "switch", Value: cfg.AutoApprove, Help: "Skip the approval queue. Administrators' own requests never wait."},
		{Name: "quotaLimit", Label: "Requests per period", Type: "number", Value: cfg.QuotaLimit, Min: ptr(0), Max: ptr(maxQuotaLimit), Help: "0 means unlimited. Administrators have no limit."},
		{Name: "quotaDays", Label: "Period in days", Type: "number", Value: cfg.QuotaDays, Min: ptr(1), Max: ptr(maxQuotaDays)},
		{Name: "notifyAdmins", Label: "Notify administrators about new requests", Type: "switch", Value: cfg.NotifyAdmins},
		{Name: "notifyUsers", Label: "Notify people when their request is approved or available", Type: "switch", Value: cfg.NotifyUsers},
		{Name: "language", Label: "Metadata language", Type: "text", Value: cfg.Language, Required: true, Placeholder: "en", Help: "Language for titles and descriptions, for example en, de or pt-BR."},
	}, "Save general settings", action{Op: "settings-save", Reload: true}, "primary", "column")

	providers := []node{}
	if cfg.TMDBKey == "" && (cfg.Movies || cfg.TV) {
		providers = append(providers, alert("Movies and series need a TMDB API key to be discovered and requested.", "warning"))
	}
	if cfg.DiscogsToken == "" && cfg.Music {
		providers = append(providers, alert("Albums need a Discogs token to be discovered and requested.", "warning"))
	}
	fields := providerField("tmdbApiKey", "TMDB API key", "Free at themoviedb.org under Settings, API. Used for movies and series.", cfg.TMDBKey != "")
	fields = append(fields, providerField("discogsToken", "Discogs token", "Create a personal access token under Settings, Developers on discogs.com. Used for albums.", cfg.DiscogsToken != "")...)
	providers = append(providers, form(fields, "Save metadata keys", action{Op: "settings-save", Reload: true}, "primary", "column"))

	return []node{
		section("General", "Which kinds of requests are open, who may make them and how people are told.", nil, general),
		section("Metadata providers", "Keys are stored encrypted, are never shown again and are only used to look titles up.", nil, providers...),
		section("Servers", "Approved requests are sent to your Radarr, Sonarr and Lidarr servers.", []node{link("Manage servers", serversTab())}, serverSummary(services)),
	}
}

func serverSummary(services []serviceRow) node {
	if len(services) == 0 {
		return alert("No server is connected yet. Add Radarr, Sonarr or Lidarr to start sending requests.", "info")
	}
	lines := []node{}
	for _, kind := range serviceKinds {
		total, ready := 0, 0
		for _, s := range services {
			if s.Kind == kind {
				total++
				if s.usable() {
					ready++
				}
			}
		}
		label := kindLabel(kind)
		switch {
		case total == 0:
			lines = append(lines, row(2, badge(label, "default"), textNode("Not connected", "muted")))
		case ready == 0:
			lines = append(lines, row(2, badge(label, "warning"), textNode(fmt.Sprintf("%d connected, none ready. Finish setting it up and enable it.", total), "muted")))
		default:
			lines = append(lines, row(2, badge(label, "success"), textNode(fmt.Sprintf("%d of %d ready", ready, total), "muted")))
		}
	}
	return column(2, lines...)
}

func kindLabel(kind string) string {
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}
