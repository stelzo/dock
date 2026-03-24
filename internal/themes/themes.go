package themes

import (
	"encoding/json"
	"strings"

	glamouransi "charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"
)

type Theme struct {
	Name         string
	GlamourStyle string
	GlamourJSON  string
	ChromaTheme  string // named chroma theme, bypasses glamour's "charm" registration bug

	NavItemFg      string
	NavSelBg       string
	NavSelFg       string
	SectionHdrFg   string
	BorderActive   string
	BorderInactive string
	StatusFg       string
	TagFg          string
	OverlaySelBg   string
	OverlaySelFg   string
	OverlayItemFg  string
}

const nordGlamourJSON = `{
  "document":   {"block_prefix":"\n","block_suffix":"\n","color":"#d8dee9","margin":1},
  "block_quote":{"indent":1,"indent_token":"│ "},
  "paragraph":  {},
  "list":       {"level_indent":2,"color":"#d8dee9"},
  "heading":    {"block_suffix":"\n","color":"#88c0d0","bold":true},
  "h1":         {"prefix":" ","suffix":" ","color":"#eceff4","background_color":"#3b4252","bold":true},
  "h2":         {"prefix":"## "},
  "h3":         {"prefix":"### "},
  "h4":         {"prefix":"#### "},
  "h5":         {"prefix":"##### "},
  "h6":         {"prefix":"###### ","color":"#81a1c1","bold":false},
  "text":       {},
  "strikethrough":{"crossed_out":true},
  "emph":       {"italic":true},
  "strong":     {"bold":true},
  "hr":         {"color":"#4c566a","format":"\n--------\n"},
  "item":       {"block_prefix":"• "},
  "enumeration":{"block_prefix":". ","color":"#88c0d0"},
  "task":       {"ticked":"[✓] ","unticked":"[ ] "},
  "link":       {"color":"#5e81ac","underline":true},
  "link_text":  {"color":"#88c0d0","bold":true},
  "image":      {"color":"#81a1c1","underline":true},
  "image_text": {"color":"#4c566a","format":"Image: {{.text}} →"},
  "code":       {"prefix":" ","suffix":" ","color":"#d08770","background_color":"#3b4252"},
  "code_block": {"color":"#d8dee9","margin":2,"chroma":{
    "text":                   {"color":"#d8dee9"},
    "error":                  {"color":"#eceff4","background_color":"#bf616a"},
    "comment":                {"color":"#4c566a"},
    "comment_preproc":        {"color":"#d08770"},
    "keyword":                {"color":"#81a1c1"},
    "keyword_reserved":       {"color":"#b48ead"},
    "keyword_namespace":      {"color":"#81a1c1"},
    "keyword_type":           {"color":"#8fbcbb"},
    "operator":               {"color":"#81a1c1"},
    "punctuation":            {"color":"#d8dee9"},
    "name":                   {"color":"#d8dee9"},
    "name_builtin":           {"color":"#88c0d0"},
    "name_tag":               {"color":"#81a1c1"},
    "name_attribute":         {"color":"#8fbcbb"},
    "name_class":             {"color":"#8fbcbb","bold":true},
    "name_constant":          {"color":"#b48ead"},
    "name_decorator":         {"color":"#ebcb8b"},
    "name_exception":         {},
    "name_function":          {"color":"#88c0d0"},
    "name_other":             {},
    "literal":                {},
    "literal_number":         {"color":"#b48ead"},
    "literal_date":           {},
    "literal_string":         {"color":"#a3be8c"},
    "literal_string_escape":  {"color":"#ebcb8b"},
    "generic_deleted":        {"color":"#bf616a"},
    "generic_emph":           {"italic":true},
    "generic_inserted":       {"color":"#a3be8c"},
    "generic_strong":         {"bold":true},
    "generic_subheading":     {"color":"#4c566a"},
    "background":             {"background_color":"#2e3440"}
  }},
  "table":              {},
  "definition_list":    {},
  "definition_term":    {},
  "definition_description":{"block_prefix":"\n🠶 "},
  "html_block":         {},
  "html_span":          {}
}`

const catppuccinMochaGlamourJSON = `{
  "document":   {"block_prefix":"\n","block_suffix":"\n","color":"#cdd6f4","margin":1},
  "block_quote":{"indent":1,"indent_token":"│ "},
  "paragraph":  {},
  "list":       {"level_indent":2,"color":"#cdd6f4"},
  "heading":    {"block_suffix":"\n","color":"#89b4fa","bold":true},
  "h1":         {"prefix":" ","suffix":" ","color":"#cdd6f4","background_color":"#313244","bold":true},
  "h2":         {"prefix":"## "},
  "h3":         {"prefix":"### "},
  "h4":         {"prefix":"#### "},
  "h5":         {"prefix":"##### "},
  "h6":         {"prefix":"###### ","color":"#74c7ec","bold":false},
  "text":       {},
  "strikethrough":{"crossed_out":true},
  "emph":       {"italic":true},
  "strong":     {"bold":true},
  "hr":         {"color":"#6c7086","format":"\n--------\n"},
  "item":       {"block_prefix":"• "},
  "enumeration":{"block_prefix":". ","color":"#89b4fa"},
  "task":       {"ticked":"[✓] ","unticked":"[ ] "},
  "link":       {"color":"#74c7ec","underline":true},
  "link_text":  {"color":"#89b4fa","bold":true},
  "image":      {"color":"#cba6f7","underline":true},
  "image_text": {"color":"#6c7086","format":"Image: {{.text}} →"},
  "code":       {"prefix":" ","suffix":" ","color":"#f38ba8","background_color":"#313244"},
  "code_block": {"color":"#cdd6f4","margin":2,"chroma":{
    "text":                   {"color":"#cdd6f4"},
    "error":                  {"color":"#cdd6f4","background_color":"#f38ba8"},
    "comment":                {"color":"#6c7086"},
    "comment_preproc":        {"color":"#fab387"},
    "keyword":                {"color":"#cba6f7"},
    "keyword_reserved":       {"color":"#f38ba8"},
    "keyword_namespace":      {"color":"#cba6f7"},
    "keyword_type":           {"color":"#89b4fa"},
    "operator":               {"color":"#89dceb"},
    "punctuation":            {"color":"#cdd6f4"},
    "name":                   {"color":"#cdd6f4"},
    "name_builtin":           {"color":"#89b4fa"},
    "name_tag":               {"color":"#cba6f7"},
    "name_attribute":         {"color":"#94e2d5"},
    "name_class":             {"color":"#f9e2af","bold":true},
    "name_constant":          {"color":"#fab387"},
    "name_decorator":         {"color":"#a6e3a1"},
    "name_exception":         {},
    "name_function":          {"color":"#89b4fa"},
    "name_other":             {},
    "literal":                {},
    "literal_number":         {"color":"#fab387"},
    "literal_date":           {},
    "literal_string":         {"color":"#a6e3a1"},
    "literal_string_escape":  {"color":"#94e2d5"},
    "generic_deleted":        {"color":"#f38ba8"},
    "generic_emph":           {"italic":true},
    "generic_inserted":       {"color":"#a6e3a1"},
    "generic_strong":         {"bold":true},
    "generic_subheading":     {"color":"#6c7086"},
    "background":             {"background_color":"#1e1e2e"}
  }},
  "table":              {},
  "definition_list":    {},
  "definition_term":    {},
  "definition_description":{"block_prefix":"\n🠶 "},
  "html_block":         {},
  "html_span":          {}
}`

const pinkGlamourJSON = `{
  "document":   {"block_prefix":"\n","block_suffix":"\n","margin":1},
  "block_quote":{"indent":1,"indent_token":"│ "},
  "paragraph":  {},
  "list":       {"level_indent":2},
  "heading":    {"block_suffix":"\n","color":"212","bold":true},
  "h1":         {},
  "h2":         {"prefix":"▌ "},
  "h3":         {"prefix":"┃ "},
  "h4":         {"prefix":"│ "},
  "h5":         {"prefix":"┆ "},
  "h6":         {"prefix":"┊ ","bold":false},
  "text":       {},
  "strikethrough":{"crossed_out":true},
  "emph":       {"italic":true},
  "strong":     {"bold":true},
  "hr":         {"color":"212","format":"\n──────\n"},
  "item":       {"block_prefix":"• "},
  "enumeration":{"block_prefix":". "},
  "task":       {"ticked":"[✓] ","unticked":"[ ] "},
  "link":       {"color":"99","underline":true},
  "link_text":  {"bold":true},
  "image":      {"underline":true},
  "image_text": {"format":"Image: {{.text}}"},
  "code":       {"prefix":" ","suffix":" ","color":"212","background_color":"236"},
  "code_block": {"margin":2,"chroma":{
    "text":                   {"color":"#f8d7f0"},
    "error":                  {"color":"#fff0f5","background_color":"#c3006b"},
    "comment":                {"color":"#c397b8"},
    "comment_preproc":        {"color":"#ffb347"},
    "keyword":                {"color":"#ff79c6"},
    "keyword_reserved":       {"color":"#ff5c8d"},
    "keyword_namespace":      {"color":"#ff79c6"},
    "keyword_type":           {"color":"#d4a0ff"},
    "operator":               {"color":"#ffcef3"},
    "punctuation":            {"color":"#f8d7f0"},
    "name":                   {"color":"#f8d7f0"},
    "name_builtin":           {"color":"#d4a0ff"},
    "name_tag":               {"color":"#ff79c6"},
    "name_attribute":         {"color":"#c9f0a0"},
    "name_class":             {"color":"#ffe066","bold":true},
    "name_constant":          {"color":"#ffb3de"},
    "name_decorator":         {"color":"#ffe066"},
    "name_exception":         {},
    "name_function":          {"color":"#d4a0ff"},
    "name_other":             {},
    "literal":                {},
    "literal_number":         {"color":"#ffb3de"},
    "literal_date":           {},
    "literal_string":         {"color":"#c9f0a0"},
    "literal_string_escape":  {"color":"#ffe066"},
    "generic_deleted":        {"color":"#ff6b6b"},
    "generic_emph":           {"italic":true},
    "generic_inserted":       {"color":"#c9f0a0"},
    "generic_strong":         {"bold":true},
    "generic_subheading":     {"color":"#c397b8"},
    "background":             {"background_color":"#2d1b2e"}
  }},
  "table":              {},
  "definition_list":    {},
  "definition_term":    {},
  "definition_description":{"block_prefix":"\n🠶 "},
  "html_block":         {},
  "html_span":          {}
}`

const specialGlamourJSON = `{
  "document":   {"block_prefix":"\n","block_suffix":"\n","color":"#f5f5f5","margin":1},
  "block_quote":{"indent":1,"indent_token":"│ ","color":"#55cdfc"},
  "paragraph":  {},
  "list":       {"level_indent":2},
  "heading":    {"block_suffix":"\n","color":"#f7a8b8","bold":true},
  "h1":         {"prefix":" ","suffix":" ","color":"#ffffff","background_color":"#366d82","bold":true},
  "h2":         {"prefix":"## ","color":"#55cdfc"},
  "h3":         {"prefix":"### ","color":"#f7a8b8"},
  "h4":         {"prefix":"#### ","color":"#ffffff"},
  "h5":         {"prefix":"##### ","color":"#55cdfc"},
  "h6":         {"prefix":"###### ","color":"#f7a8b8","bold":false},
  "hr":         {"color":"#55cdfc","format":"\n──────\n"},
  "item":       {"block_prefix":"• "},
  "link":       {"color":"#55cdfc","underline":true},
  "link_text":  {"color":"#f7a8b8","bold":true},
  "code":       {"prefix":" ","suffix":" ","color":"#f7a8b8","background_color":"#1e1e2e"},
  "code_block": {"margin":2,"chroma":{
    "keyword":                {"color":"#55cdfc"},
    "comment":                {"color":"#6c7086"},
    "literal_string":         {"color":"#f7a8b8"},
    "name_function":          {"color":"#ffffff"},
    "background":             {"background_color":"#1e1e2e"}
  }}
}`

var Themes = []Theme{
	{
		Name:           "Dark",
		GlamourStyle:   "dark",
		ChromaTheme:    "monokai",
		NavSelBg:       "63",
		NavSelFg:       "230",
		SectionHdrFg:   "243",
		BorderActive:   "63",
		BorderInactive: "238",
		StatusFg:       "243",
		TagFg:          "86",
		OverlaySelBg:   "63",
		OverlaySelFg:   "230",
		OverlayItemFg:  "252",
	},
	{
		Name:           "Tokyo Night",
		GlamourStyle:   "tokyo-night",
		ChromaTheme:    "tokyonight-storm",
		NavSelBg:       "#33467c",
		NavSelFg:       "#c0caf5",
		SectionHdrFg:   "#565f89",
		BorderActive:   "#7aa2f7",
		BorderInactive: "#24283b",
		StatusFg:       "#565f89",
		TagFg:          "#7dcfff",
		OverlaySelBg:   "#33467c",
		OverlaySelFg:   "#c0caf5",
		OverlayItemFg:  "#a9b1d6",
	},
	{
		Name:           "Dracula",
		GlamourStyle:   "dracula",
		ChromaTheme:    "dracula",
		NavSelBg:       "#44475a",
		NavSelFg:       "#f8f8f2",
		SectionHdrFg:   "#6272a4",
		BorderActive:   "#bd93f9",
		BorderInactive: "#44475a",
		StatusFg:       "#6272a4",
		TagFg:          "#50fa7b",
		OverlaySelBg:   "#44475a",
		OverlaySelFg:   "#f8f8f2",
		OverlayItemFg:  "#f8f8f2",
	},
	{
		Name:           "Nord",
		GlamourJSON:    nordGlamourJSON,
		ChromaTheme:    "nord",
		NavItemFg:      "#d8dee9",
		NavSelBg:       "#5e81ac",
		NavSelFg:       "#eceff4",
		SectionHdrFg:   "#616e88",
		BorderActive:   "#88c0d0",
		BorderInactive: "#3b4252",
		StatusFg:       "#616e88",
		TagFg:          "#88c0d0",
		OverlaySelBg:   "#5e81ac",
		OverlaySelFg:   "#eceff4",
		OverlayItemFg:  "#d8dee9",
	},
	{
		Name:           "Catppuccin Mocha",
		GlamourJSON:    catppuccinMochaGlamourJSON,
		ChromaTheme:    "catppuccin-mocha",
		NavSelBg:       "#313244",
		NavSelFg:       "#cdd6f4",
		SectionHdrFg:   "#6c7086",
		BorderActive:   "#89b4fa",
		BorderInactive: "#1e1e2e",
		StatusFg:       "#6c7086",
		TagFg:          "#74c7ec",
		OverlaySelBg:   "#313244",
		OverlaySelFg:   "#cdd6f4",
		OverlayItemFg:  "#bac2de",
	},
	{
		Name:           "Pink",
		GlamourJSON:    pinkGlamourJSON,
		ChromaTheme:    "catppuccin-macchiato",
		NavSelBg:       "#c97fc4",
		NavSelFg:       "#ffffff",
		SectionHdrFg:   "#c97fc4",
		BorderActive:   "#ff87c3",
		BorderInactive: "#4b4453",
		StatusFg:       "#c97fc4",
		TagFg:          "#ff87c3",
		OverlaySelBg:   "#c97fc4",
		OverlaySelFg:   "#ffffff",
		OverlayItemFg:  "#f8d7f0",
	},
	{
		Name:           "Trans",
		GlamourJSON:    specialGlamourJSON,
		ChromaTheme:    "catppuccin-mocha",
		NavSelBg:       "#366d82",
		NavSelFg:       "#ffffff",
		SectionHdrFg:   "#f7a8b8",
		BorderActive:   "#55cdfc",
		BorderInactive: "#2d2d2d",
		StatusFg:       "#f7a8b8",
		TagFg:          "#55cdfc",
		OverlaySelBg:   "#366d82",
		OverlaySelFg:   "#ffffff",
		OverlayItemFg:  "#f5f5f5",
	},
	{
		Name:           "Light",
		GlamourStyle:   "light",
		ChromaTheme:    "catppuccin-latte",
		NavItemFg:      "#212121",
		NavSelBg:       "#5c6bc0",
		NavSelFg:       "#ffffff",
		SectionHdrFg:   "#757575",
		BorderActive:   "#5c6bc0",
		BorderInactive: "#bdbdbd",
		StatusFg:       "#757575",
		TagFg:          "#00897b",
		OverlaySelBg:   "#5c6bc0",
		OverlaySelFg:   "#ffffff",
		OverlayItemFg:  "#424242",
	},
}

func (t Theme) GlamourConfig() (glamouransi.StyleConfig, bool) {
	var cfg glamouransi.StyleConfig
	if t.GlamourJSON != "" {
		if err := json.Unmarshal([]byte(t.GlamourJSON), &cfg); err != nil {
			return cfg, false
		}
	} else if base := glamourstyles.DefaultStyles[t.GlamourStyle]; base != nil {
		cfg = *base
	}
	cfg.CodeBlock.Chroma = nil
	cfg.CodeBlock.Theme = t.ChromaTheme
	return cfg, true
}

func IdxFromName(name string) (int, bool) {
	norm := func(s string) string {
		return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(s))
	}
	needle := norm(name)
	for i, t := range Themes {
		tName := norm(t.Name)
		if tName == needle || strings.Contains(needle, tName) {
			return i, true
		}
	}
	return 0, false
}
