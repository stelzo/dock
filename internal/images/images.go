package images

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"codeberg.org/stelzo/dock/internal/ui"
)

type ImageRef struct {
	Alt          string
	RawURL       string
	ResolvedPath string
	Line         int
}

var ImgMdRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^\s)]+)`)

func ExtractImageRefs(src, docFile, rendered string) []ImageRef {
	docDir := filepath.Dir(docFile)
	var refs []ImageRef
	for _, m := range ImgMdRe.FindAllStringSubmatch(src, -1) {
		alt, rawURL := m[1], m[2]
		resolved := ""
		if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
			sum := sha256.Sum256([]byte(rawURL))
			ext := filepath.Ext(strings.SplitN(rawURL, "?", 2)[0])
			if cacheDir, err := os.UserCacheDir(); err == nil {
				p := filepath.Join(cacheDir, "dock", fmt.Sprintf("%x%s", sum, ext))
				if _, err := os.Stat(p); err == nil {
					resolved = p
				}
			}
			if resolved == "" {
				resolved = rawURL
			}
		} else {
			abs := filepath.Join(docDir, rawURL)
			if _, err := os.Stat(abs); err == nil {
				resolved = abs
			}
		}
		refs = append(refs, ImageRef{Alt: alt, RawURL: rawURL, ResolvedPath: resolved})
	}
	rendLines := strings.Split(rendered, "\n")
	searchFrom := 0
	for ri := range refs {
		for li := searchFrom; li < len(rendLines); li++ {
			plain := ui.AnsiEscRe.ReplaceAllString(rendLines[li], "")
			if strings.Contains(plain, "[Image: ") {
				refs[ri].Line = li
				searchFrom = li + 1
				break
			}
		}
	}
	return refs
}

func CacheImage(rawURL string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	cacheDir = filepath.Join(cacheDir, "dock")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(rawURL))
	ext := filepath.Ext(strings.SplitN(rawURL, "?", 2)[0])
	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%x%s", sum, ext))
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}
	resp, err := http.Get(rawURL) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	f, err := os.Create(cachePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return cachePath, err
}

func ImageOverlaySize(origW, origH, maxW, maxH int) (w, h int) {
	w = maxW
	h = origH * w / (origW * 2)
	if h > maxH || h == 0 {
		h = maxH
		w = origW * maxH * 2 / origH
		if w > maxW {
			w = maxW
		}
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return
}
