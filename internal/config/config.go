package config

import (
	"os"
	"strings"

	"codeberg.org/stelzo/dock/internal/themes"
)

var (
	IgnoredDirs     map[string]bool
	DocsTitle       string
	DefaultThemeIdx int
	GitURL          string
	GitRef          string
	PullInterval    string
	CachePath       string
)

const Version = "0.1.0"

// Config represents the structure of the dock configuration file (YAML/TOML).
type Config struct {
	SiteName     string        `yaml:"site_name" toml:"site_name"`
	Nav          []interface{} `yaml:"nav" toml:"nav"`
	DocsDir      string        `yaml:"docs_dir" toml:"docs_dir"`
	DockDir      string        `yaml:"dock_dir" toml:"dock_dir"`
	GitURL       string        `yaml:"git_url" toml:"git_url"`
	GitRef       string        `yaml:"git_ref" toml:"git_ref"`
	PullInterval string        `yaml:"pull_interval" toml:"pull_interval"`
	CachePath    string        `yaml:"cache_path" toml:"cache_path"`
	Project      struct {
		SiteName     string        `toml:"site_name"`
		Nav          []interface{} `toml:"nav"`
		DocsDir      string        `toml:"docs_dir"`
		DockDir      string        `toml:"dock_dir"`
		GitURL       string        `toml:"git_url"`
		GitRef       string        `toml:"git_ref"`
		PullInterval string        `toml:"pull_interval"`
		CachePath    string        `toml:"cache_path"`
	} `toml:"project"`
}

func Init() {
	ignoreEnv := os.Getenv("DOCK_IGNORE_DIRS")
	if ignoreEnv == "" {
		ignoreEnv = "assets,stylesheets"
	}
	IgnoredDirs = make(map[string]bool)
	for _, d := range strings.Split(ignoreEnv, ",") {
		if d = strings.TrimSpace(d); d != "" {
			IgnoredDirs[d] = true
		}
	}

	DocsTitle = os.Getenv("DOCK_TITLE")
	if v := os.Getenv("DOCK_THEME"); v != "" {
		if i, ok := themes.IdxFromName(v); ok {
			DefaultThemeIdx = i
		}
	}

	GitURL = os.Getenv("DOCK_GIT_URL")
	GitRef = os.Getenv("DOCK_GIT_REF")
	PullInterval = os.Getenv("DOCK_PULL_INTERVAL")
	CachePath = os.Getenv("DOCK_CACHE_PATH")
}
