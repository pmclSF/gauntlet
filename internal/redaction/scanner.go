package redaction

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ScanResult is the result of scanning a file for sensitive content.
type ScanResult struct {
	File    string
	Line    int
	Pattern string
	Match   string
}

// ScanOptions controls scanner detector behavior.
type ScanOptions struct {
	PromptInjectionDenylist bool
}

// DefaultScanOptions returns the default scanner policy.
func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		PromptInjectionDenylist: true,
	}
}

type leakDetector interface {
	Name() string
	DetectText(text string) []string
	DetectBinary(data []byte) []string
}

type regexDetector struct {
	name    string
	pattern *regexp.Regexp
}

func (d regexDetector) Name() string { return d.name }

func (d regexDetector) DetectText(text string) []string {
	if d.pattern == nil {
		return nil
	}
	return d.pattern.FindAllString(text, -1)
}

func (d regexDetector) DetectBinary(data []byte) []string {
	for _, segment := range printableSegments(data, 20) {
		if matches := d.DetectText(segment); len(matches) > 0 {
			return matches
		}
	}
	return nil
}

type creditCardLuhnDetector struct {
	pattern *regexp.Regexp
}

func (d creditCardLuhnDetector) Name() string { return "credit_card_luhn" }

func (d creditCardLuhnDetector) DetectText(text string) []string {
	if d.pattern == nil {
		return nil
	}
	candidates := d.pattern.FindAllString(text, -1)
	matches := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		digits := extractDigits(candidate)
		if len(digits) < 13 || len(digits) > 19 {
			continue
		}
		if luhnValid(digits) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func (d creditCardLuhnDetector) DetectBinary(data []byte) []string { return nil }

type entropyDetector struct {
	pattern   *regexp.Regexp
	minLen    int
	threshold float64
}

func (d entropyDetector) Name() string { return "high_entropy_token" }

func (d entropyDetector) DetectText(text string) []string {
	if d.pattern == nil {
		return nil
	}
	candidates := d.pattern.FindAllString(text, -1)
	matches := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !d.isLikelySecretToken(candidate) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func (d entropyDetector) DetectBinary(data []byte) []string {
	matches := make([]string, 0)
	for _, segment := range printableSegments(data, d.minLen) {
		matches = append(matches, d.DetectText(segment)...)
	}
	return matches
}

func (d entropyDetector) isLikelySecretToken(token string) bool {
	if len(token) < d.minLen || len(token) > 512 {
		return false
	}
	if isAllDigits(token) {
		return false
	}
	if shannonEntropy(token) < d.threshold {
		return false
	}
	return true
}

type contextualSecretDetector struct {
	pattern *regexp.Regexp
}

func (d contextualSecretDetector) Name() string { return "contextual_secret_keyword" }

func (d contextualSecretDetector) DetectText(text string) []string {
	if d.pattern == nil {
		return nil
	}
	matches := make([]string, 0)
	for _, groups := range d.pattern.FindAllStringSubmatch(text, -1) {
		if len(groups) < 3 {
			continue
		}
		value := strings.TrimSpace(groups[2])
		if isLikelyContextualSecretValue(value) {
			matches = append(matches, groups[0])
		}
	}
	return matches
}

func (d contextualSecretDetector) DetectBinary(data []byte) []string { return nil }

type tokenFormatDetector struct {
	pattern *regexp.Regexp
}

func (d tokenFormatDetector) Name() string { return "token_format" }

func (d tokenFormatDetector) DetectText(text string) []string {
	if d.pattern == nil {
		return nil
	}
	return d.pattern.FindAllString(text, -1)
}

func (d tokenFormatDetector) DetectBinary(data []byte) []string {
	return d.DetectText(string(data))
}

type promptInjectionDetector struct {
	pattern *regexp.Regexp
}

func (d promptInjectionDetector) Name() string { return "prompt_injection_marker" }

func (d promptInjectionDetector) DetectText(text string) []string {
	if d.pattern == nil {
		return nil
	}
	return d.pattern.FindAllString(text, -1)
}

func (d promptInjectionDetector) DetectBinary(data []byte) []string {
	matches := make([]string, 0)
	for _, segment := range printableSegments(data, 24) {
		matches = append(matches, d.DetectText(segment)...)
	}
	return matches
}

// ScanDirectory recursively scans a directory for sensitive content.
func ScanDirectory(dir string, r *Redactor) ([]ScanResult, error) {
	return ScanDirectoryWithOptions(dir, r, DefaultScanOptions())
}

// ScanDirectoryWithOptions recursively scans a directory for sensitive content
// using explicit scan options.
func ScanDirectoryWithOptions(dir string, r *Redactor, opts ScanOptions) ([]ScanResult, error) {
	var results []ScanResult
	detectors := defaultLeakDetectors(r, opts)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		seen := map[string]bool{}
		if isTextFile(filepath.Ext(path)) || looksLikeText(data) {
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				for _, detector := range detectors {
					for _, match := range detector.DetectText(line) {
						appendScanResult(&results, seen, ScanResult{
							File:    path,
							Line:    i + 1,
							Pattern: detector.Name(),
							Match:   maskScanMatch(match),
						})
					}
				}
			}
			return nil
		}

		// Binary-safe scan path: run byte-level detectors over printable slices.
		for _, detector := range detectors {
			for _, match := range detector.DetectBinary(data) {
				appendScanResult(&results, seen, ScanResult{
					File:    path,
					Line:    1,
					Pattern: detector.Name(),
					Match:   maskScanMatch(match),
				})
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

func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	if !utf8.Valid(sample) {
		return false
	}
	nonPrintable := 0
	for _, b := range sample {
		if b == 0x00 {
			return false
		}
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			nonPrintable++
		}
	}
	// Treat content as text unless control bytes dominate the sample.
	return float64(nonPrintable)/float64(len(sample)) < 0.1
}

func defaultLeakDetectors(r *Redactor, opts ScanOptions) []leakDetector {
	detectors := []leakDetector{
		creditCardLuhnDetector{
			pattern: regexp.MustCompile(`\b(?:\d[\s-]?){13,19}\b`),
		},
		regexDetector{
			name:    "ssn_pattern",
			pattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
		tokenFormatDetector{
			pattern: regexp.MustCompile(`\b(?:sk-[A-Za-z0-9]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z\-_]{35})\b`),
		},
		contextualSecretDetector{
			pattern: regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|authorization|bearer|private[_-]?key|access[_-]?key)\b\s*[:=]\s*["']?([A-Za-z0-9+/_=\-\.]{8,})["']?`),
		},
	}
	if opts.PromptInjectionDenylist {
		detectors = append(detectors, promptInjectionDetector{
			pattern: regexp.MustCompile(`(?i)\b(ignore|disregard|forget)\s+(all\s+)?(previous|prior)\s+instructions\b|\b(reveal|show|print)\s+(your\s+)?system\s+prompt\b|\bdo\s+anything\s+now\b|\bjailbreak\b`),
		})
	}
	detectors = append(detectors, entropyDetector{
		pattern:   regexp.MustCompile(`[A-Za-z0-9+/_=\-]{20,}`),
		minLen:    20,
		threshold: 3.6,
	})

	if r == nil {
		return detectors
	}
	for _, pattern := range r.Patterns {
		if pattern == nil {
			continue
		}
		if isDefaultRedactionPattern(pattern.String()) {
			continue
		}
		detectors = append(detectors, regexDetector{
			name:    "regex_pattern",
			pattern: pattern,
		})
	}
	return detectors
}

func isDefaultRedactionPattern(raw string) bool {
	return raw == `\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{1,7}\b` ||
		raw == `\b\d{3}-\d{2}-\d{4}\b`
}

func appendScanResult(results *[]ScanResult, seen map[string]bool, candidate ScanResult) {
	key := fmt.Sprintf("%s:%d:%s:%s", candidate.File, candidate.Line, candidate.Pattern, candidate.Match)
	if seen[key] {
		return
	}
	seen[key] = true
	*results = append(*results, candidate)
}

func maskScanMatch(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

func isLikelyContextualSecretValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "", "true", "false", "null", "none", "redacted", "[redacted]":
		return false
	}
	if len(value) >= 20 {
		return true
	}
	if len(value) >= 12 && shannonEntropy(value) >= 3.0 {
		return true
	}
	return false
}

func printableSegments(data []byte, minLen int) []string {
	segments := make([]string, 0)
	start := -1
	for i, b := range data {
		if (b >= 32 && b <= 126) || b == '\t' {
			if start == -1 {
				start = i
			}
			continue
		}
		if start != -1 && i-start >= minLen {
			segments = append(segments, string(data[start:i]))
		}
		start = -1
	}
	if start != -1 && len(data)-start >= minLen {
		segments = append(segments, string(data[start:]))
	}
	return segments
}

func extractDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d, err := strconv.Atoi(string(digits[i]))
		if err != nil {
			return false
		}
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum > 0 && sum%10 == 0
}

func isAllDigits(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	length := float64(len(s))
	var entropy float64
	for _, count := range counts {
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}
	return entropy
}
