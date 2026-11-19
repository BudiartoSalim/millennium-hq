package domain

import (
	"slices"
	"strings"
)

// optionSyntax is how a program reads the short options among its
// arguments.
type optionSyntax string

const (
	// bundledShorts: short options bundle in one word (`-in`), and the
	// first that takes a value takes the rest of the word as its value,
	// or the next word when nothing follows it (`-nA3`, `-e x`), as rg
	// reads them.
	bundledShorts optionSyntax = "bundled"
	// separateShorts: each short option is a word of its own, its value
	// attached (`-n5`, `-U3`), and `-<n>` is a number (`git log -3`), as
	// git's revision and diff options are read.
	separateShorts optionSyntax = "separate"
)

// readOnlyOptions are the options of a program, or of one of its
// commands, that keep it read-only; any other option makes it not
// read-only. An option that takes a value is listed as valued only when
// the program reads the next word as its value in one pass with its
// other options: reading that word as a value where the program reads it
// as an option would let an option through unchecked. Where it may not
// (git, see attachedValue), the value must be attached.
type readOnlyOptions struct {
	syntax optionSyntax
	// flags take no value.
	flags []string
	// attached take a value only attached to them (`--color=always`,
	// `-uno`), or none; a short one only in separateShorts.
	attached []string
	// attachedValue take a value, and are read-only only with it attached
	// (`--since=x`, `-n5`); a short one only in separateShorts. Written
	// alone, git takes the next word as the value, but git log reads its
	// own options (`--decorate`) in a pass before its revision options, so
	// which word that is cannot be told from the text: `git log --before
	// --decorate --after --output=x` writes x (round 10, R10-CR-1).
	attachedValue []string
	// valued take a value, attached (`--glob=x`, `-A3`) or as the next
	// word (`--glob x`, `-A 3`); a short one only in bundledShorts. A next
	// word that starts with a dash is not read-only, though the program
	// reads it as the value: in doubt, it is not.
	valued []string
}

// with is o with the options of more added, in o's syntax.
func (o readOnlyOptions) with(more readOnlyOptions) readOnlyOptions {
	return readOnlyOptions{
		syntax:        o.syntax,
		flags:         slices.Concat(o.flags, more.flags),
		attached:      slices.Concat(o.attached, more.attached),
		attachedValue: slices.Concat(o.attachedValue, more.attachedValue),
		valued:        slices.Concat(o.valued, more.valued),
	}
}

// allow reports whether every option among args, up to a `--` after
// which every word is an operand, is one of o, and no value an option
// takes as the next word starts with a dash. A word that starts with a
// dash and is not the value of the option before it is an option; `-`
// alone is an operand, standard input.
func (o readOnlyOptions) allow(args []string) bool {
	for i := 0; i < len(args) && args[i] != "--"; i++ {
		if len(args[i]) < 2 || args[i][0] != '-' {
			continue
		}
		values, ok := o.option(args[i])
		if !ok || (values > 0 && i+1 < len(args) && strings.HasPrefix(args[i+1], "-")) {
			return false
		}
		i += values
	}
	return true
}

// option reports whether arg is one of o, and how many words after it
// are its value: none or one.
func (o readOnlyOptions) option(arg string) (int, bool) {
	switch {
	case strings.HasPrefix(arg, "--"):
		return o.longOption(arg)
	case o.syntax == bundledShorts:
		return o.shortBundle(arg[1:])
	}
	return 0, o.shortOption(arg)
}

// longOption is option for a long option, arg, with its value attached
// after `=` or not. Abbreviations are not read: only the whole name is
// one of o.
func (o readOnlyOptions) longOption(arg string) (int, bool) {
	name, _, hasValue := strings.Cut(arg, "=")
	switch {
	case slices.Contains(o.valued, name):
		return valueWords(arg[len(name):]), true
	case slices.Contains(o.attachedValue, name):
		return 0, hasValue
	case slices.Contains(o.attached, name):
		return 0, true
	}
	return 0, !hasValue && slices.Contains(o.flags, name)
}

// shortOption reports whether a short option, arg, as separateShorts
// reads it, is one of o; it takes no word after it as its value.
func (o readOnlyOptions) shortOption(arg string) bool {
	name := arg[:2]
	switch {
	case slices.Contains(o.attachedValue, name):
		return len(arg) > len(name)
	case slices.Contains(o.attached, name), slices.Contains(o.flags, arg):
		return true
	}
	return isDigits(arg[1:])
}

// shortBundle is option for a bundle of short options, letters, as
// bundledShorts reads it.
func (o readOnlyOptions) shortBundle(letters string) (int, bool) {
	for i := range len(letters) {
		name := "-" + letters[i:i+1]
		if slices.Contains(o.valued, name) {
			return valueWords(letters[i+1:]), true
		}
		if !slices.Contains(o.flags, name) {
			return 0, false
		}
	}
	return 0, true
}

// valueWords is how many words after an option that takes a value are
// its value, given rest, what follows the option's name in its word: the
// next word when nothing does, else none, its value being attached (`=x`,
// `3`).
func valueWords(rest string) int {
	if rest == "" {
		return 1
	}
	return 0
}
