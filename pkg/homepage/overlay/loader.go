package overlay

// loader.go owns deterministic overlay template discovery and partial loading.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (every Go file declares its purpose).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func LoadTemplateSource(siteDir, locale, explicit string) (string, error) {
	candidates := make([]string, 0, 3)
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		candidates = append(candidates, trimmed)
	} else {
		candidates = append(candidates,
			"homepage."+locale+".template.html",
			"homepage.template.html",
			"homepage.html",
		)
	}

	mainTemplate, err := readFirstFile(siteDir, candidates)
	if err != nil {
		return "", err
	}

	partials, err := filepath.Glob(filepath.Join(siteDir, "partials", "*.html"))
	if err != nil {
		return "", fmt.Errorf("resolve overlay homepage partials in %s: %w", siteDir, err)
	}
	sort.Strings(partials)

	parts := []string{mainTemplate}
	for _, partial := range partials {
		body, err := os.ReadFile(partial)
		if err != nil {
			return "", fmt.Errorf("read overlay homepage partial %s: %w", partial, err)
		}
		parts = append(parts, string(body))
	}

	return strings.Join(parts, "\n"), nil
}

func LoadBundle(siteDir, locale string, files []string, fallback string) (string, error) {
	names, err := ResolveAssetNames(siteDir, locale, files, fallback)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(siteDir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read overlay homepage asset %s: %w", path, err)
		}
		parts = append(parts, string(body))
	}
	return strings.Join(parts, "\n"), nil
}

func ResolveAssetNames(siteDir, locale string, files []string, fallback string) ([]string, error) {
	candidates := make([]string, 0, len(files)+2)
	if len(files) > 0 {
		candidates = append(candidates, files...)
	} else {
		candidates = append(candidates, "homepage."+locale+"."+strings.TrimPrefix(fallback, "homepage."))
		candidates = append(candidates, fallback)
	}

	names := make([]string, 0, len(candidates))
	for _, name := range candidates {
		if strings.TrimSpace(name) == "" {
			continue
		}
		safeName, err := safeRelativeName(name)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(siteDir, safeName)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read overlay homepage asset %s: %w", path, err)
		}
		if info.IsDir() {
			continue
		}
		names = append(names, safeName)
	}
	return names, nil
}

func LoadStylesheetURLs(siteDir, locale, clientSlug string, files []string) ([]string, error) {
	names, err := ResolveAssetNames(siteDir, locale, files, "homepage.css")
	if err != nil {
		return nil, err
	}

	urls := make([]string, 0, len(names))
	for _, name := range names {
		urls = append(urls, PublicAssetURL(clientSlug, name))
	}
	return urls, nil
}

func readFirstFile(siteDir string, candidates []string) (string, error) {
	for _, name := range candidates {
		safeName, err := safeRelativeName(name)
		if err != nil {
			return "", err
		}
		path := filepath.Join(siteDir, safeName)
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read overlay homepage template %s: %w", path, err)
		}
		return string(body), nil
	}
	return "", fmt.Errorf("overlay homepage template missing in %s", siteDir)
}

func safeRelativeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("overlay homepage asset name is required")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("overlay homepage asset %q must be relative", name)
	}
	cleaned := filepath.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("overlay homepage asset %q must stay inside the site directory", name)
	}
	return cleaned, nil
}
