package main

import "fmt"

// node is one element of the server-driven UI tree (see the plugin UI protocol).
type node = map[string]any

type nav struct {
	Query map[string]string `json:"query"`
	Merge bool              `json:"merge,omitempty"`
}

type action struct {
	Op       string         `json:"op"`
	Body     map[string]any `json:"body,omitempty"`
	Navigate *nav           `json:"navigate,omitempty"`
	Reload   bool           `json:"reload,omitempty"`
	Confirm  string         `json:"confirm,omitempty"`
}

type option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type badgeSpec struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

func nodes(children ...node) []node {
	out := make([]node, 0, len(children))
	for _, child := range children {
		if child != nil {
			out = append(out, child)
		}
	}
	return out
}

func column(gap int, children ...node) node {
	return node{"type": "stack", "gap": gap, "children": nodes(children...)}
}

func row(gap int, children ...node) node {
	return node{"type": "stack", "direction": "row", "gap": gap, "wrap": true, "children": nodes(children...)}
}

func heading(value string, level int) node {
	return node{"type": "heading", "text": value, "level": level}
}

func textNode(value, tone string) node {
	n := node{"type": "text", "text": value}
	if tone != "" {
		n["tone"] = tone
	}
	return n
}

func badge(value, tone string) node {
	n := node{"type": "badge", "text": value}
	if tone != "" {
		n["tone"] = tone
	}
	return n
}

func alert(value, tone string) node { return node{"type": "alert", "text": value, "tone": tone} }

func empty(value string) node { return node{"type": "empty", "text": value} }

func link(label string, to nav) node { return node{"type": "link", "label": label, "to": to} }

func button(label string, act action, variant string) node {
	n := node{"type": "button", "label": label, "action": act}
	if variant != "" {
		n["variant"] = variant
	}
	if act.Confirm != "" {
		n["confirm"] = act.Confirm
	}
	return n
}

// isHTTPS reports whether an image URL is safe for the renderer.
func isHTTPS(src string) bool { return len(src) > 8 && src[:8] == "https://" }

func imageNode(src, alt, aspect string) node {
	if !isHTTPS(src) {
		return nil
	}
	return node{"type": "image", "src": src, "alt": alt, "aspect": aspect}
}

func cardNode(title, subtitle string, image node, badges []badgeSpec, to *nav, children []node, actions []node) node {
	n := node{"type": "card", "title": title}
	if subtitle != "" {
		n["subtitle"] = subtitle
	}
	if image != nil {
		n["image"] = node{"src": image["src"], "alt": image["alt"], "aspect": image["aspect"]}
	}
	if len(badges) > 0 {
		n["badges"] = badges
	}
	if to != nil {
		n["to"] = *to
	}
	if len(children) > 0 {
		n["children"] = children
	}
	if len(actions) > 0 {
		n["actions"] = actions
	}
	return n
}

func grid(minWidth int, children []node) node {
	return node{"type": "grid", "minWidth": minWidth, "children": children}
}

type field struct {
	Name        string   `json:"name"`
	Label       string   `json:"label,omitempty"`
	Type        string   `json:"type"`
	Value       any      `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []option `json:"options,omitempty"`
	Min         *int     `json:"min,omitempty"`
	Max         *int     `json:"max,omitempty"`
}

func hidden(name string, value any) field { return field{Name: name, Type: "hidden", Value: value} }

func form(fields []field, label string, act action, variant, layout string) node {
	n := node{
		"type":   "form",
		"fields": fields,
		"submit": node{"label": label, "action": act},
	}
	if variant != "" {
		n["submit"].(node)["variant"] = variant
	}
	if layout != "" {
		n["layout"] = layout
	}
	return n
}

func pagerNode(page, pageSize, total int) node {
	return node{"type": "pager", "param": "page", "page": page, "pageSize": pageSize, "total": total}
}

func chips(param string, options []option, value string, reset []string) node {
	n := node{"type": "chips", "param": param, "options": options, "value": value}
	if len(reset) > 0 {
		n["reset"] = reset
	}
	return n
}

func searchbox(value, placeholder string, reset []string) node {
	n := node{"type": "searchbox", "param": "q", "value": value, "placeholder": placeholder}
	if len(reset) > 0 {
		n["reset"] = reset
	}
	return n
}

type tabSpec struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Children []node `json:"children"`
}

func tabsNode(tabs []tabSpec, reset []string) node {
	return node{"type": "tabs", "param": "tab", "tabs": tabs, "reset": reset}
}

type column2 struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func tableNode(columns []column2, rows []node, emptyText string) node {
	return node{"type": "table", "columns": columns, "rows": rows, "emptyText": emptyText}
}

func tableRow(key string, cells map[string]any) node { return node{"key": key, "cells": cells} }

func statusTone(status string) string {
	switch status {
	case "pending":
		return "warning"
	case "approved":
		return "info"
	case "available":
		return "success"
	case "declined", "failed":
		return "danger"
	}
	return "default"
}

var statusLabels = map[string]string{
	"pending": "Pending", "approved": "Approved", "declined": "Declined", "failed": "Failed", "available": "Available", "none": "Not Requested",
}

var cardStatusLabels = map[string]string{
	"pending": "Requested", "approved": "Approved", "available": "Available", "declined": "Declined", "failed": "Failed",
}

var typeLabels = map[string]string{"movie": "Movie", "tv": "Series", "music": "Album"}

func yearSuffix(year int) string {
	if year <= 0 {
		return ""
	}
	return fmt.Sprintf(" · %d", year)
}
