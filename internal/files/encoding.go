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

const (
	EncodingUTF8   = "utf-8"
	EncodingASCII  = "ascii"
	EncodingGBK    = "gbk"
	EncodingGB2312 = "gb2312"
	EncodingBIG5   = "big5"
)

var ErrUnrepresentable = errors.New("files: text cannot be represented in the save encoding")

var (
	gbkEncoding  = simplifiedchinese.GBK
	big5Encoding = traditionalchinese.Big5
)

var binarySignatures = [][]byte{
	[]byte("\x89PNG\r\n\x1a\n"),
	[]byte("GIF87a"),
	[]byte("GIF89a"),
	{0xFF, 0xD8, 0xFF},
	[]byte("%PDF-"),
	[]byte("PK\x03\x04"),
	{0x1F, 0x8B},
	{0x7F, 'E', 'L', 'F'},
	[]byte("Rar!\x1a\x07"),
	[]byte("\xFD7zXZ\x00"),
	[]byte("BZh"),
	[]byte("SQLite format 3"),
	[]byte("\x00asm"),
	[]byte("OggS"),
	[]byte("ID3"),
}

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

func detectEncoding(body []byte) (string, string, error) {
	if len(body) == 0 {
		return EncodingUTF8, "", nil
	}
	if isBinaryBody(body) {
		return "", "", ErrNotEditable
	}
	if utf8.Valid(body) {
		return EncodingUTF8, string(body), nil
	}
	if hasLegacyControl(body) {
		return "", "", ErrNotEditable
	}
	gbkText, gbkOK := decodeLegacy(gbkEncoding, body)
	big5Text, big5OK := decodeLegacy(big5Encoding, body)
	switch {
	case gbkOK && big5OK:
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

func isBinaryBody(body []byte) bool {
	if bytes.IndexByte(body, 0) >= 0 {
		return true
	}
	for _, signature := range binarySignatures {
		if bytes.HasPrefix(body, signature) {
			return true
		}
	}
	return false
}

func hasLegacyControl(body []byte) bool {
	for _, b := range body {
		switch b {
		case '\t', '\n', '\r', '\f':
			continue
		}
		if b < 0x20 || b == 0x7f {
			return true
		}
	}
	return false
}

func gbkLabel(text string) string {
	encoded, err := gbkEncoding.NewEncoder().Bytes([]byte(text))
	if err != nil || !isGB2312Bytes(encoded) {
		return EncodingGBK
	}
	return EncodingGB2312
}

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

func decodeLegacy(enc encoding.Encoding, body []byte) (string, bool) {
	decoded, _, err := transform.Bytes(enc.NewDecoder(), body)
	if err != nil {
		return "", false
	}
	text := string(decoded)
	if strings.ContainsRune(text, utf8.RuneError) {
		return "", false
	}
	reencoded, _, err := transform.Bytes(enc.NewEncoder(), decoded)
	if err != nil || !bytes.Equal(reencoded, body) {
		return "", false
	}
	return text, true
}

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
		default:
			score--
		}
	}
	return score
}

func isJunkHan(r rune) bool {
	switch {
	case r >= 0x2E80 && r <= 0x2FDF:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0x3100 && r <= 0x312F, r >= 0x31A0 && r <= 0x31BF:
		return true
	case r >= 0x3040 && r <= 0x30FF:
		return true
	}
	return false
}

func isSinoPunctuation(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F:
		return true
	case r >= 0xFF01 && r <= 0xFF65:
		return true
	case r == 0x2018, r == 0x2019, r == 0x201C, r == 0x201D:
		return true
	case r == 0x2013, r == 0x2014, r == 0x2026:
		return true
	case r == 0x00B7:
		return true
	}
	return false
}

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
		encoded, err := gbkEncoding.NewEncoder().Bytes([]byte(text))
		if err != nil {
			return nil, firstUnrepresentable(enc, text, func(r rune) bool {
				_, err := gbkEncoding.NewEncoder().Bytes([]byte(string(r)))
				return err == nil
			})
		}
		if enc == EncodingGB2312 && !isGB2312Bytes(encoded) {
			return nil, firstUnrepresentable(enc, text, func(r rune) bool {
				if r < utf8.RuneSelf {
					return true
				}
				b, err := gbkEncoding.NewEncoder().Bytes([]byte(string(r)))
				return err == nil && isGB2312Bytes(b)
			})
		}
		return encoded, nil
	case EncodingBIG5:
		encoded, err := big5Encoding.NewEncoder().Bytes([]byte(text))
		if err != nil {
			return nil, firstUnrepresentable(enc, text, func(r rune) bool {
				_, err := big5Encoding.NewEncoder().Bytes([]byte(string(r)))
				return err == nil
			})
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("%w: unsupported encoding %q", ErrValidation, enc)
	}
}

func firstUnrepresentable(enc, text string, fits func(rune) bool) error {
	for _, r := range text {
		if !fits(r) {
			return unrepresentable(enc, r)
		}
	}
	return fmt.Errorf("%w: %s", ErrUnrepresentable, strings.ToUpper(enc))
}

func unrepresentable(enc string, r rune) error {
	return fmt.Errorf("%w: %q (U+%04X) has no mapping in %s", ErrUnrepresentable, r, r, strings.ToUpper(enc))
}
