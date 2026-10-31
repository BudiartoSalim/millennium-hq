package domain

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// secretPattern is one high-confidence credential format.
type secretPattern struct {
	name string
	re   *regexp.Regexp
}

// Formats whose shape alone identifies a credential.
var (
	awsAccessKey   = regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)
	privateKey     = regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY( BLOCK)?-----`)
	githubToken    = regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{60,})\b`)
	slackToken     = regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}\b`)
	googleAPIKey   = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)
	stripeLiveKey  = regexp.MustCompile(`\b(sk|rk)_live_[0-9A-Za-z]{20,}\b`)
	anthropicKey   = regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)
	openAIKey      = regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9_-]{32,}\b`)
	jwtToken       = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	assignedSecret = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|client[_-]?secret)["']?\s*[:=]\s*["']([^"'\s]{12,})["']`)
	placeholder    = regexp.MustCompile(`(?i)example|changeme|change-me|placeholder|dummy|sample|your[_-]|xxx|\*\*\*|<.*>|\$\{|\{\{|test|fake|redacted`)
)

func secretPatterns() []secretPattern {
	return []secretPattern{
		{"AWS access key", awsAccessKey}, {"private key", privateKey}, {"GitHub token", githubToken},
		{"Slack token", slackToken}, {"Google API key", googleAPIKey}, {"Stripe live key", stripeLiveKey},
		{"Anthropic API key", anthropicKey}, {"OpenAI API key", openAIKey}, {"JSON Web Token", jwtToken},
	}
}

// minSecretEntropy separates generated secrets from words: random tokens
// measure well above it, "supersecretpassword" well below.
const minSecretEntropy = 3.5

// SecretFindings scans touched lines for credentials: known formats, and
// quoted values assigned to secret-sounding names that look random rather
// than like a placeholder. A "millennium:allow secret -- <reason>" waiver
// covers deliberate fixtures.
func SecretFindings(file string, content []byte, touched LineSet, waivers Waivers) []Finding {
	var findings []Finding
	for i, line := range strings.Split(string(content), "\n") {
		number := i + 1
		if !touched.Contains(number) || waivers.Covers("secret", number) {
			continue
		}
		if kind, ok := secretOnLine(line); ok {
			findings = append(findings, Finding{Rule: "secret", File: file, Line: number, Message: fmt.Sprintf(
				"this looks like a %s; never commit credentials: load it from the environment or a secret store, and rotate it if it was real", kind)})
		}
	}
	return findings
}

func secretOnLine(line string) (string, bool) {
	for _, p := range secretPatterns() {
		if p.re.MatchString(line) {
			return p.name, true
		}
	}
	for _, m := range assignedSecret.FindAllStringSubmatch(line, -1) {
		value := m[2]
		if !placeholder.MatchString(value) && shannonEntropy(value) >= minSecretEntropy {
			return "hard-coded " + strings.ToLower(m[1]), true
		}
	}
	return "", false
}

func shannonEntropy(s string) float64 {
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	entropy := 0.0
	n := float64(len([]rune(s)))
	for _, c := range counts {
		p := float64(c) / n
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// IsBinary reports whether content looks like binary data: a NUL byte in its
// first 8000 bytes, as git decides.
func IsBinary(content []byte) bool {
	head := content[:min(len(content), 8000)]
	return strings.IndexByte(string(head), 0) >= 0
}
