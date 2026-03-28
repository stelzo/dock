package navigation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/log/v2"
	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"go.steado.tech/dock/internal/config"
)

var syncMap = make(map[string]bool)

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w (output: %s)", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func SyncGitRepo(url, ref, subfolder, cachePath string) error {
	absCache, _ := filepath.Abs(cachePath)

	if strings.HasPrefix(url, "git://") && strings.Contains(strings.TrimPrefix(url, "git://"), ":") {
		return fmt.Errorf("invalid git URL: %s (did you mean to use a '/' instead of ':'?)", url)
	}

	if _, err := os.Stat(filepath.Join(absCache, ".git")); err != nil {
		log.Debug("initializing sparse blobless clone", "url", url, "cache", cachePath)
		if err := os.MkdirAll(filepath.Dir(absCache), 0755); err != nil {
			return err
		}

		if fi, err := os.Stat(absCache); err == nil && fi.IsDir() {
			entries, _ := os.ReadDir(absCache)
			if len(entries) > 0 {
				log.Warn("cache directory is not empty and not a git repo", "cache", cachePath)
			}
		}

		cloneArgs := []string{"clone", "--filter=blob:none", "--sparse", "--depth", "1"}
		if ref != "" {
			cloneArgs = append(cloneArgs, "-b", ref)
		}
		cloneArgs = append(cloneArgs, url, absCache)

		if err := runGit("", cloneArgs...); err != nil {
			return err
		}

		if subfolder != "" && subfolder != "." {
			if err := runGit(absCache, "sparse-checkout", "set", subfolder); err != nil {
				return err
			}
		}
	} else {
		log.Debug("updating git repo", "cache", cachePath)
		if err := runGit(absCache, "fetch", "origin", "--depth", "1"); err != nil {
			return err
		}

		targetRef := "origin/HEAD"
		if ref != "" {
			targetRef = ref
		}
		if err := runGit(absCache, "checkout", targetRef); err != nil {
			_ = runGit(absCache, "checkout", "origin/HEAD")
		}

		if subfolder != "" && subfolder != "." {
			_ = runGit(absCache, "sparse-checkout", "set", subfolder)
		}
	}

	log.Debug("git sync successful", "url", url)
	return nil
}

func StartGitBackgroundSync(url, ref, subfolder, cachePath, intervalStr string) {
	key := url + ref + subfolder + cachePath
	if syncMap[key] {
		return
	}
	syncMap[key] = true

	interval := time.Hour
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		}
	}

	log.Debug("starting background git sync", "url", url, "interval", interval)

	go func() {
		ticker := time.NewTicker(interval)
		for range ticker.C {
			if err := SyncGitRepo(url, ref, subfolder, cachePath); err != nil {
				log.Error("background git sync error", "url", url, "err", err)
			}
		}
	}()
}

func findConfig(root string) (string, string) {
	rootAbs, _ := filepath.Abs(root)
	paths := []string{
		rootAbs,
		filepath.Dir(rootAbs),
	}
	names := []struct {
		name string
		ext  string
	}{
		{"dock.toml", "toml"},
		{".dock.toml", "toml"},
		{"zensical.toml", "toml"},
		{"mkdocs.yml", "yaml"},
		{"mkdocs.yaml", "yaml"},
	}

	for _, p := range paths {
		for _, n := range names {
			cp := filepath.Join(p, n.name)
			if _, err := os.Stat(cp); err == nil {
				return cp, n.ext
			}
		}
	}
	return "", ""
}

func parseConfig(path, ext string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg config.Config
	if ext == "toml" {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return nil, err
		}
	} else {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func LoadNavConfig(root string) []NavEntry {
	cp, ext := findConfig(root)
	if cp == "" {
		for _, sub := range []string{"docs", "doc"} {
			if cp, ext = findConfig(filepath.Join(root, sub)); cp != "" {
				root = filepath.Join(root, sub)
				break
			}
		}
	}

	if cp != "" {
		cfg, err := parseConfig(cp, ext)
		if err == nil && cfg != nil {
			applyConfig(cfg)

			nav := cfg.Nav
			if len(nav) == 0 {
				nav = cfg.Project.Nav
			}
			if len(nav) > 0 {
				docsDir := cfg.DockDir
				if docsDir == "" {
					docsDir = cfg.DocsDir
				}
				if docsDir == "" {
					docsDir = cfg.Project.DockDir
				}
				if docsDir == "" {
					docsDir = cfg.Project.DocsDir
				}
				newRoot := root
				if docsDir != "" {
					newRoot = filepath.Join(filepath.Dir(cp), docsDir)
				}
				return buildFromConfig(newRoot, nav, 0)
			}
		}
	}

	return nil
}

func applyConfig(cfg *config.Config) {
	if config.DocsTitle == "" {
		if cfg.SiteName != "" {
			config.DocsTitle = cfg.SiteName
		} else if cfg.Project.SiteName != "" {
			config.DocsTitle = cfg.Project.SiteName
		}
	}

	if config.GitURL == "" {
		if cfg.GitURL != "" {
			config.GitURL = cfg.GitURL
		} else if cfg.Project.GitURL != "" {
			config.GitURL = cfg.Project.GitURL
		}
	}

	if config.GitRef == "" {
		if cfg.GitRef != "" {
			config.GitRef = cfg.GitRef
		} else if cfg.Project.GitRef != "" {
			config.GitRef = cfg.Project.GitRef
		}
	}

	if config.PullInterval == "" {
		if cfg.PullInterval != "" {
			config.PullInterval = cfg.PullInterval
		} else if cfg.Project.PullInterval != "" {
			config.PullInterval = cfg.Project.PullInterval
		}
	}

	if config.CachePath == "" {
		if cfg.CachePath != "" {
			config.CachePath = cfg.CachePath
		} else if cfg.Project.CachePath != "" {
			config.CachePath = cfg.Project.CachePath
		}
	}
}

func buildFromConfig(root string, nav []any, depth int) []NavEntry {
	var out []NavEntry
	for _, item := range nav {
		switch v := item.(type) {
		case string:
			p := resolvePath(root, v)
			out = append(out, NavEntry{
				Title:    FileTitle(p),
				FilePath: p,
				Depth:    depth,
			})
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, title := range keys {
				val := v[title]
				switch vv := val.(type) {
				case string:
					p := resolvePath(root, vv)
					out = append(out, NavEntry{
						Title:    title,
						FilePath: p,
						Depth:    depth,
					})
				case []any:
					out = append(out, NavEntry{
						Title: title,
						Depth: depth,
					})
					out = append(out, buildFromConfig(root, vv, depth+1)...)
				case map[string]any:
					out = append(out, NavEntry{
						Title: title,
						Depth: depth,
					})
					out = append(out, buildFromConfig(root, []any{vv}, depth+1)...)
				}
			}
		}
	}
	return out
}

func resolvePath(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	cp := filepath.Join(root, p)
	if _, err := os.Stat(cp); err == nil {
		return cp
	}
	return p
}
