package domain

import (
	"bufio"
	"bytes"
	"strings"
)

// strictModeWindow is how many lines from the top a script has to switch
// strict mode on: it belongs before the work starts.
const strictModeWindow = 40

// StrictModeMissing lists the shell options an executable script must
// enable and does not: errexit and nounset always, and pipefail for bash and
// ksh, whose pipelines otherwise hide a failing command. A script without a
// shebang is a library meant to be sourced, so it is exempt: it inherits its
// caller's options.
func StrictModeMissing(content []byte) []string {
	dialect, ok := ShellDialect(content)
	if !ok {
		return nil
	}
	required := []string{"errexit", "nounset"}
	if dialect == "bash" || dialect == "ksh" {
		required = append(required, "pipefail")
	}
	enabled := enabledOptions(content)
	var missing []string
	for _, option := range required {
		if !enabled[option] {
			missing = append(missing, option)
		}
	}
	return missing
}

// enabledOptions reads the `set` commands near the top of a script:
// `set -euo pipefail`, `set -Eeuo pipefail`, `set -o errexit -o nounset`.
func enabledOptions(content []byte) map[string]bool {
	enabled := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for line := 0; line < strictModeWindow && scanner.Scan(); line++ {
		for _, command := range strings.FieldsFunc(scanner.Text(), func(r rune) bool { return r == ';' }) {
			fields := strings.Fields(command)
			if len(fields) > 0 && fields[0] == "set" {
				readSetFlags(fields[1:], enabled)
			}
		}
	}
	return enabled
}

func readSetFlags(args []string, enabled map[string]bool) {
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") || args[i] == "--" {
			continue
		}
		next := ""
		if i+1 < len(args) {
			next = args[i+1]
		}
		if readFlagGroup(args[i][1:], next, enabled) {
			i++
		}
	}
}

// readFlagGroup applies one group of short flags such as "euo"; with an
// "o", next names the option. It reports whether next was consumed.
func readFlagGroup(group, next string, enabled map[string]bool) bool {
	consumed := false
	for _, flag := range group {
		switch {
		case flag == 'e':
			enabled["errexit"] = true
		case flag == 'u':
			enabled["nounset"] = true
		case flag == 'o' && next != "":
			enabled[next] = true
			consumed = true
		}
	}
	return consumed
}
