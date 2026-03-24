package navigation

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codeberg.org/stelzo/dock/internal/config"
)

type NavEntry struct {
	Title    string
	FilePath string // empty = section header
	Depth    int
}

func BuildNav(root string) []NavEntry {
	if cfgNav := LoadNavConfig(root); len(cfgNav) > 0 {
		return cfgNav
	}
	var out []NavEntry
	walkDir(root, root, 0, &out)
	return out
}

func walkDir(root, dir string, depth int, out *[]NavEntry) {
	if depth > 0 {
		*out = append(*out, NavEntry{
			Title: TitleCase(filepath.Base(dir)),
			Depth: depth - 1,
		})
	}
	fis, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.Slice(fis, func(i, j int) bool {
		ni, nj := fis[i].Name(), fis[j].Name()
		if ni == "index.md" {
			return true
		}
		if nj == "index.md" {
			return false
		}
		di, dj := fis[i].IsDir(), fis[j].IsDir()
		if !di && dj {
			return true
		}
		if di && !dj {
			return false
		}
		return ni < nj
	})
	for _, fi := range fis {
		p := filepath.Join(dir, fi.Name())
		if fi.IsDir() {
			if config.IgnoredDirs[fi.Name()] {
				continue
			}
			walkDir(root, p, depth+1, out)
		} else if strings.HasSuffix(fi.Name(), ".md") {
			*out = append(*out, NavEntry{
				Title:    FileTitle(p),
				FilePath: p,
				Depth:    depth,
			})
		}
	}
}

func FileTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return strings.TrimSuffix(filepath.Base(path), ".md")
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if t, ok := strings.CutPrefix(sc.Text(), "# "); ok {
			return t
		}
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

func TitleCase(s string) string {
	var words []string
	for _, w := range strings.Fields(strings.ReplaceAll(s, "_", " ")) {
		if len(w) > 0 {
			words = append(words, strings.ToUpper(w[:1])+w[1:])
		}
	}
	return strings.Join(words, " ")
}
