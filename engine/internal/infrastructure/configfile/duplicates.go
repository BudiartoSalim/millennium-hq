package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// rejectDuplicateKeys fails on an object that names the same key twice.
// encoding/json keeps the last value, so a duplicate could hide the effective
// setting from whoever reviews the file. Keys are compared case-insensitively
// because encoding/json also matches field names that way.
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return walkValue(dec, "$")
}

func walkValue(dec *json.Decoder, at string) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	switch tok {
	case json.Delim('{'):
		return walkObject(dec, at)
	case json.Delim('['):
		return walkArray(dec, at)
	}
	return nil
}

func walkObject(dec *json.Decoder, at string) error {
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("parse: %w", err)
		}
		key, _ := tok.(string)
		folded := foldKey(key)
		if seen[folded] {
			return fmt.Errorf("parse: key %q appears more than once in %s", key, at)
		}
		seen[folded] = true
		if err := walkValue(dec, at+"."+key); err != nil {
			return err
		}
	}
	return closeDelim(dec)
}

func walkArray(dec *json.Decoder, at string) error {
	for i := 0; dec.More(); i++ {
		if err := walkValue(dec, fmt.Sprintf("%s[%d]", at, i)); err != nil {
			return err
		}
	}
	return closeDelim(dec)
}

func closeDelim(dec *json.Decoder) error {
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return nil
}

// foldKey approximates the case folding encoding/json uses to match keys:
// upper-casing first maps letters such as the long s and the Kelvin sign to
// the ASCII letter they fold to.
func foldKey(key string) string {
	return strings.ToLower(strings.ToUpper(key))
}
