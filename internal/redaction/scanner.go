package redaction

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanResult is the result of scanning a file for sensitive content.
type ScanResult struct {
	File     string
	Line     int
	Pattern  string
	Match    string
}

// ScanDirectory recursively scans a directory for sensitive content.
func ScanDirectory(dir string, r *Redactor) ([]ScanResult, error) {
	var results []ScanResult

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Only scan text files
		ext := filepath.Ext(path)
		if !isTextFile(ext) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			for _, pattern := range r.Patterns {
				if match := pattern.FindString(line); match != "" {
					results = append(results, ScanResult{
						File:    path,
						Line:    i + 1,
						Pattern: pattern.String(),
						Match:   maskScanMatch(match),
					})
				}
			}
		}

		return nil
	})

	return results, err
}

func isTextFile(ext string) bool {
	textExts := map[string]bool{
		".json": true, ".yaml": true, ".yml": true,
		".txt": true, ".md": true, ".py": true,
		".go": true, ".js": true, ".ts": true,
	}
	return textExts[ext]
}

func maskScanMatch(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
