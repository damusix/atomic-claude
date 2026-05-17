package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var nonAlphanumDash = regexp.MustCompile(`[^a-z0-9-]+`)
var dashRun = regexp.MustCompile(`-{2,}`)

// asciiTable maps common accented Latin characters to their ASCII equivalents.
// Covers the most common cases without requiring an external dependency.
var asciiTable = strings.NewReplacer(
	"À", "a", "Á", "a", "Â", "a", "Ã", "a", "Ä", "a", "Å", "a", "à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"È", "e", "É", "e", "Ê", "e", "Ë", "e", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"Ì", "i", "Í", "i", "Î", "i", "Ï", "i", "ì", "i", "í", "i", "î", "i", "ï", "i",
	"Ò", "o", "Ó", "o", "Ô", "o", "Õ", "o", "Ö", "o", "Ø", "o", "ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"Ù", "u", "Ú", "u", "Û", "u", "Ü", "u", "ù", "u", "ú", "u", "û", "u", "ü", "u",
	"Ñ", "n", "ñ", "n",
	"Ç", "c", "ç", "c",
	"Ý", "y", "ý", "y", "ÿ", "y",
	"Æ", "ae", "æ", "ae",
	"Œ", "oe", "œ", "oe",
	"ß", "ss",
)

// Slug converts an arbitrary string into a kebab-case ASCII slug.
func Slug(s string) string {
	// Replace known accented chars with ASCII equivalents.
	s = asciiTable.Replace(s)

	lower := strings.ToLower(s)

	// Replace whitespace runs with a single dash, drop non-ASCII.
	var sb strings.Builder
	inSpace := false
	for _, r := range lower {
		if unicode.IsSpace(r) {
			if !inSpace {
				sb.WriteRune('-')
				inSpace = true
			}
		} else if r <= unicode.MaxASCII {
			inSpace = false
			sb.WriteRune(r)
		}
		// drop non-ASCII that wasn't mapped above
	}
	spaced := sb.String()

	// Remove anything that isn't [a-z0-9-].
	stripped := nonAlphanumDash.ReplaceAllString(spaced, "")

	// Collapse multiple dashes.
	collapsed := dashRun.ReplaceAllString(stripped, "-")

	// Trim leading/trailing dashes.
	return strings.Trim(collapsed, "-")
}

// prefixRe validates ShortID prefix values: must start with a lowercase letter
// and contain only lowercase letters and digits.
var prefixRe = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ShortID returns a short random id of the form "<prefix>-XXXX" where XXXX is
// 4 lowercase hex characters drawn from crypto/rand. prefix must match
// ^[a-z][a-z0-9]*$ (non-empty, starts with a letter, only lowercase
// alphanumeric). Returns an error for empty or invalid prefixes.
func ShortID(prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("ids.ShortID: prefix must not be empty")
	}
	if !prefixRe.MatchString(prefix) {
		return "", fmt.Errorf("ids.ShortID: prefix %q must match ^[a-z][a-z0-9]*$", prefix)
	}
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ids.ShortID: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}
