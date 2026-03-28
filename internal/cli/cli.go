package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/log/v2"
	"github.com/spf13/cobra"
	"go.steado.tech/dock/internal/config"
	"go.steado.tech/dock/internal/navigation"
	dockssh "go.steado.tech/dock/internal/ssh"
	"go.steado.tech/dock/internal/themes"
	"go.steado.tech/dock/internal/tui"
)

var (
	port         string
	raw          bool
	title        string
	theme        string
	ignoreDirs   string
	gitURL       string
	gitRef       string
	pullInt      string
	cache        string
	forwardAgent bool
)

func gitSync(subfolder string) string {
	if config.GitURL == "" {
		return subfolder
	}
	cache := config.CachePath
	if cache == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			userCache = os.TempDir()
		}
		hash := sha256.Sum256([]byte(config.GitURL))
		cache = filepath.Join(userCache, "dock", hex.EncodeToString(hash[:])[:12])
	}
	if err := navigation.SyncGitRepo(config.GitURL, config.GitRef, subfolder, cache); err != nil {
		log.Error("fatal: error syncing git repo", "err", err)
		os.Exit(1)
	}

	// Try to load config from the synced repo to populate global settings (like site title)
	navigation.LoadNavConfig(filepath.Join(cache, subfolder))

	navigation.StartGitBackgroundSync(config.GitURL, config.GitRef, subfolder, cache, config.PullInterval)
	return filepath.Join(cache, subfolder)
}

var rootCmd = &cobra.Command{
	Use:   "dock [docs-path|user@host]",
	Short: "An SSH-based markdown documentation viewer",
	Args:  cobra.MaximumNArgs(1),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if title != "" {
			config.DocsTitle = title
		}
		if theme != "" {
			if i, ok := themes.IdxFromName(theme); ok {
				config.DefaultThemeIdx = i
			}
		}
		if ignoreDirs != "" {
			config.IgnoredDirs = make(map[string]bool)
			for d := range strings.SplitSeq(ignoreDirs, ",") {
				if d = strings.TrimSpace(d); d != "" {
					config.IgnoredDirs[d] = true
				}
			}
		}
		if gitURL != "" {
			config.GitURL = gitURL
		}
		if gitRef != "" {
			config.GitRef = gitRef
		}
		if pullInt != "" {
			config.PullInterval = pullInt
		}
		if cache != "" {
			config.CachePath = cache
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		docs := "."
		if len(args) > 0 {
			docs = args[0]
		}

		if strings.Contains(docs, "@") || (!filepath.IsAbs(docs) && docs != "." && !strings.HasPrefix(docs, "./") && !strings.HasPrefix(docs, "../")) {
			if _, err := os.Stat(docs); os.IsNotExist(err) {
				remoteConnect(docs)
				return
			}
		}

		docs = gitSync(docs)
		tui.Run(docs)
	},
}

func remoteConnect(target string) {
	user := ""
	host := target
	if before, after, ok := strings.Cut(target, "@"); ok {
		user = before
		host = after
	}

	effectiveTheme := theme
	if effectiveTheme == "" {
		effectiveTheme = os.Getenv("DOCK_THEME")
	}

	if effectiveTheme != "" {
		if user == "" {
			user = effectiveTheme
		} else {
			user = effectiveTheme + "." + user
		}
	}

	sshArgs := []string{}
	if port != "" {
		sshArgs = append(sshArgs, "-p", port)
	}

	if user != "" {
		sshArgs = append(sshArgs, user+"@"+host)
	} else {
		sshArgs = append(sshArgs, host)
	}

	sshCmdArgs := []string{"-t"}
	if forwardAgent {
		sshCmdArgs = append(sshCmdArgs, "-A")
	}

	log.Debug("executing remote connect", "args", strings.Join(append(sshCmdArgs, sshArgs...), " "))

	cmd := exec.Command("ssh", append(sshCmdArgs, sshArgs...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Error("ssh connection failed", "err", err)
		os.Exit(1)
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of dock",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dock %s\n", config.Version)
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve [docs-path]",
	Short: "Start the SSH server",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		docs := gitSync(args[0])
		dockssh.RunServe(docs, port)
	},
}

var listCmd = &cobra.Command{
	Use:   "list [docs-path]",
	Short: "List available documents",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		docs := gitSync(args[0])
		os.Exit(dockssh.CmdList(os.Stdout, os.Stderr, docs))
	},
}

var getCmd = &cobra.Command{
	Use:   "get [docs-path] [title-or-path]",
	Short: "Get and render a specific document",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		docs := gitSync(args[0])
		getArgs := []string{args[1]}
		if raw {
			getArgs = append(getArgs, "--raw")
		}
		os.Exit(dockssh.CmdGet(os.Stdout, os.Stderr, docs, getArgs))
	},
}

var searchCmd = &cobra.Command{
	Use:   "search [docs-path] [query]",
	Short: "Search for documents matching a query",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		docs := gitSync(args[0])
		os.Exit(dockssh.CmdSearch(os.Stdout, os.Stderr, docs, args[1:]))
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all cached repositories",
	Run: func(cmd *cobra.Command, args []string) {
		userCache, err := os.UserCacheDir()
		if err == nil {
			dockCache := filepath.Join(userCache, "dock")
			if _, err := os.Stat(dockCache); err == nil {
				log.Info("cleaning user cache", "path", dockCache)
				if err := os.RemoveAll(dockCache); err != nil {
					log.Error("failed to clean user cache", "err", err)
				}
			}
		}

		if cache != "" {
			log.Info("cleaning specified cache path", "path", cache)
			if err := os.RemoveAll(cache); err != nil {
				log.Error("failed to clean specified cache", "err", err)
			}
		}

		// Also check for the legacy .dock-cache in current directory
		if _, err := os.Stat(".dock-cache"); err == nil {
			log.Info("cleaning legacy .dock-cache")
			_ = os.RemoveAll(".dock-cache")
		}

		log.Info("cache cleanup complete")
	},
}

func init() {
	defaultPort := os.Getenv("DOCK_SSH_PORT")
	if defaultPort == "" {
		defaultPort = "22"
	}

	rootCmd.PersistentFlags().StringVarP(&title, "title", "t", "", "Site title")
	rootCmd.PersistentFlags().StringVar(&theme, "theme", "", "Default theme")
	rootCmd.PersistentFlags().StringVarP(&port, "port", "p", defaultPort, "SSH listen port")
	rootCmd.PersistentFlags().StringVar(&ignoreDirs, "ignore-dirs", "", "Comma-separated list of directories to ignore")
	rootCmd.PersistentFlags().StringVar(&gitURL, "git-url", "", "Git URL to sync from (ssh, https, git)")
	rootCmd.PersistentFlags().StringVar(&gitRef, "git-ref", "", "Git branch or tag to use")
	rootCmd.PersistentFlags().StringVar(&pullInt, "pull-interval", "", "Sync pull interval (e.g. 1h)")
	rootCmd.PersistentFlags().StringVar(&cache, "cache-path", "", "Local cache path for sync")
	rootCmd.PersistentFlags().BoolVarP(&forwardAgent, "forward-agent", "A", false, "Forward SSH agent")

	getCmd.Flags().BoolVar(&raw, "raw", false, "output raw markdown without styling")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(cleanCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
