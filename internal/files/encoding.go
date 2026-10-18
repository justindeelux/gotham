package files

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// Editor encodings (JUS-222). The editor opens any of these and saves back in
// the same charset, so a legacy file round-trips byte-identically unless the
// user actually edits it. UTF-8 is the only encoding a browser can produce
// natively; the rest are transcoded here.
const (
	EncodingUTF8   = "utf-8"
	EncodingASCII  = "ascii"
	EncodingGBK    = "gbk"
	EncodingGB2312 = "gb2312"
	EncodingBIG5   = "big5"
)

// ErrUnrepresentable reports a save body holding a rune the target encoding has
// no mapping for (an emoji into GB2312, a simplified-only character into BIG5).
// The write is refused instead of substituting '?' so a save never loses text.
var ErrUnrepresentable = errors.New("files: text cannot be represented in the save encoding")

// gbkEncoder, big5Encoder are the x/text encoders used for transcoding. Each is
// safe for concurrent use.
var (
	gbkEncoder  = simplifiedchinese.GBK.NewEncoder()
	big5Encoder = traditionalchinese.Big5.NewEncoder()
)

// normalizeEncoding canonicalises a requested save encoding. An empty value
// keeps the historical UTF-8 behaviour; the aaPanel alias "ansi" is coerced to
// UTF-8 like the legacy panel did.
func normalizeEncoding(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", EncodingUTF8, "utf8", "ansi":
		return EncodingUTF8, nil
	case EncodingASCII, "us-ascii":
		return EncodingASCII, nil
	case EncodingGBK:
		return EncodingGBK, nil
	case EncodingGB2312, "gb-2312":
		return EncodingGB2312, nil
	case EncodingBIG5, "big-5":
		return EncodingBIG5, nil
	default:
		return "", fmt.Errorf("%w: unsupported encoding %q", ErrValidation, name)
	}
}

// detectEncoding decodes one file body for the editor: BOM, then UTF-8
// validity, then the GB2312/GBK/BIG5 heuristic. It returns the canonical
// encoding and the UTF-8 text the editor shows. A NUL byte, or a body no
// candidate decodes, is ErrNotEditable (415) so the caller offers a download.
//
// Detection never guesses: a body that is valid UTF-8 is UTF-8, and a legacy
// body must decode cleanly (no replacement characters) under a candidate. The
// heuristic picks between GBK and BIG5 by how plausible the decoded text is
// (see hanScore); GB2312 is reported when every character also fits GB2312,
// which is byte-identical to GBK for those characters.
func detectEncoding(body []byte) (string, string, error) {
	if len(body) == 0 {
		return EncodingUTF8, "", nil
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return "", "", ErrNotEditable
	}
	// A UTF-8 BOM is kept in the text: the editor's splitDoc strips it and
	// re-applies it on save, so a BOM-prefixed file round-trips unchanged.
	if utf8.Valid(body) {
		return EncodingUTF8, string(body), nil
	}
	gbkText, gbkOK := decodeLegacy(simplifiedchinese.GBK, body)
	big5Text, big5OK := decodeLegacy(traditionalchinese.Big5, body)
	switch {
	case gbkOK && big5OK:
		// Both decode; prefer the more plausible text, and simplified Chinese
		// on a tie (the reference panel falls back UTF-8 -> GBK -> BIG5).
		if hanScore(gbkText) >= hanScore(big5Text) {
			return gbkLabel(gbkText), gbkText, nil
		}
		return EncodingBIG5, big5Text, nil
	case gbkOK:
		return gbkLabel(gbkText), gbkText, nil
	case big5OK:
		return EncodingBIG5, big5Text, nil
	default:
		return "", "", ErrNotEditable
	}
}

// gbkLabel reports GB2312 when every decoded character also fits the narrower
// GB2312 repertoire, and GBK otherwise. Both re-encode to the same bytes for a
// GB2312-subset file.
func gbkLabel(text string) string {
	encoded, err := gbkEncoder.Bytes([]byte(text))
	if err != nil || !isGB2312Bytes(encoded) {
		return EncodingGBK
	}
	return EncodingGB2312
}

// isGB2312Bytes reports whether every multi-byte character of a GBK-encoded
// body is inside the GB2312 byte ranges (lead 0xA1-0xF7, trail 0xA1-0xFE).
// GBK's extension area uses a lead below 0xA1 or a trail outside that range.
func isGB2312Bytes(encoded []byte) bool {
	for i := 0; i < len(encoded); {
		if encoded[i] < utf8.RuneSelf {
			i++
			continue
		}
		if i+1 >= len(encoded) {
			return false
		}
		if encoded[i] < 0xA1 || encoded[i] > 0xF7 {
			return false
		}
		if encoded[i+1] < 0xA1 || encoded[i+1] > 0xFE {
			return false
		}
		i += 2
	}
	return true
}

// decodeLegacy decodes body with one legacy charset. A decoder that emits the
// Unicode replacement character failed: the bytes are not clean text in that
// charset.
func decodeLegacy(enc encoding.Encoding, body []byte) (string, bool) {
	decoded, _, err := transform.Bytes(enc.NewDecoder(), body)
	if err != nil {
		return "", false
	}
	text := string(decoded)
	if strings.ContainsRune(text, utf8.RuneError) {
		return "", false
	}
	return text, true
}

// hanScore rates how plausible a legacy decoding is. Common CJK ideographs score
// positive; the radicals, compatibility forms and Bopomofo/kana ranges a wrong
// GBK<->BIG5 decoding tends to land in score negative. ASCII and the usual
// Chinese punctuation are neutral. The absolute number does not matter, only
// the comparison between two candidate decodings of the same bytes.
func hanScore(text string) int {
	score := 0
	for _, r := range text {
		switch {
		case r == utf8.RuneError:
			score -= 50
		case isJunkHan(r):
			score -= 3
		case unicode.Is(unicode.Han, r):
			score += 2
		case r < utf8.RuneSelf, isSinoPunctuation(r):
			// Neutral: ASCII and normal Chinese/ASCII punctuation legitimately
			// appear in both encodings.
		default:
			score--
		}
	}
	return score
}

// isJunkHan reports the ideograph-adjacent ranges a wrong legacy decoding
// produces: radicals, compatibility ideographs and compatibility forms.
func isJunkHan(r rune) bool {
	switch {
	case r >= 0x2E80 && r <= 0x2FDF: // CJK Radicals Supplement + Kangxi Radicals
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility Ideographs
		return true
	case r >= 0xFE30 && r <= 0xFE6F: // CJK Compatibility Forms + Small Form Variants
		return true
	case r >= 0x3100 && r <= 0x312F, r >= 0x31A0 && r <= 0x31BF: // Bopomofo
		return true
	case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
		return true
	}
	return false
}

// isSinoPunctuation reports the punctuation that routinely accompanies Chinese
// text, so a correct decoding is not penalised for it.
func isSinoPunctuation(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F: // CJK Symbols and Punctuation
		return true
	case r >= 0xFF01 && r <= 0xFF65: // Fullwidth forms (comma, colon, brackets…)
		return true
	case r == 0x2018, r == 0x2019, r == 0x201C, r == 0x201D: // curly quotes
		return true
	case r == 0x2013, r == 0x2014, r == 0x2026: // dashes, ellipsis
		return true
	case r == 0x00B7: // middle dot
		return true
	}
	return false
}

// encodeContent transcodes a UTF-8 editor body into enc. A rune the encoding
// cannot represent is ErrUnrepresentable with the offending character named,
// never a silent substitution.
func encodeContent(enc, text string) ([]byte, error) {
	switch enc {
	case EncodingUTF8:
		return []byte(text), nil
	case EncodingASCII:
		for _, r := range text {
			if r >= utf8.RuneSelf {
				return nil, unrepresentable(enc, r)
			}
		}
		return []byte(text), nil
	case EncodingGBK, EncodingGB2312:
		encoded, err := gbkEncoder.Bytes([]byte(text))
		if err != nil {
			return nil, firstUnrepresentable(enc, text, func(r rune) bool {
				_, err := gbkEncoder.Bytes([]byte(string(r)))
				return err == nil
			})
		}
		if enc == EncodingGB2312 && !isGB2312Bytes(encoded) {
			return nil, firstUnrepresentable(enc, text, func(r rune) bool {
				if r < utf8.RuneSelf {
					return true
				}
				b, err := gbkEncoder.Bytes([]byte(string(r)))
				return err == nil && isGB2312Bytes(b)
			})
		}
		return encoded, nil
	case EncodingBIG5:
		encoded, err := big5Encoder.Bytes([]byte(text))
		if err != nil {
			return nil, firstUnrepresentable(enc, text, func(r rune) bool {
				_, err := big5Encoder.Bytes([]byte(string(r)))
				return err == nil
			})
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("%w: unsupported encoding %q", ErrValidation, enc)
	}
}

// firstUnrepresentable finds the first rune enc cannot hold and builds the
// refusal error; a body whose bulk encode failed for a non-rune reason still
// yields a clear error naming the encoding.
func firstUnrepresentable(enc, text string, fits func(rune) bool) error {
	for _, r := range text {
		if !fits(r) {
			return unrepresentable(enc, r)
		}
	}
	return fmt.Errorf("%w: %s", ErrUnrepresentable, strings.ToUpper(enc))
}

// unrepresentable builds the refusal error naming the character. The rune is
// quoted so the API client can show it; the control plane never stores it.
func unrepresentable(enc string, r rune) error {
	return fmt.Errorf("%w: %q (U+%04X) has no mapping in %s", ErrUnrepresentable, r, r, strings.ToUpper(enc))
}
