package projects

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// ProjectIconUpload marks a project whose icon bytes live in project_icons.
const ProjectIconUpload = "upload"

// MaxProjectIconBytes caps an uploaded icon before re-encoding.
const MaxProjectIconBytes = 512 << 10

// maxProjectIconDimension keeps a decoded icon small enough to re-encode as PNG
// without a decompression bomb.
const maxProjectIconDimension = 512

// ProjectIconPresets is the built-in icon set; the web mirrors the keys in
// web/src/features/projects/icons.ts.
var ProjectIconPresets = []string{
	"rocket", "box", "globe", "database", "server", "terminal",
	"cloud", "layers", "code", "package", "zap", "star",
}

func IsProjectIconPreset(key string) bool {
	for _, preset := range ProjectIconPresets {
		if preset == key {
			return true
		}
	}
	return false
}

// normalizeProjectIcon sniffs the format, rejects oversized images and
// re-encodes accepted input as PNG so the served bytes are always ours.
func normalizeProjectIcon(data []byte) ([]byte, error) {
	if len(data) > MaxProjectIconBytes {
		return nil, fmt.Errorf("%w: icon must be at most %d KB", ErrValidation, MaxProjectIconBytes/1024)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: icon must be a PNG, JPEG or GIF image", ErrValidation)
	}
	switch format {
	case "png", "jpeg", "gif":
	default:
		return nil, fmt.Errorf("%w: icon must be a PNG, JPEG or GIF image", ErrValidation)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxProjectIconDimension || config.Height > maxProjectIconDimension {
		return nil, fmt.Errorf("%w: icon must be at most %dx%d pixels", ErrValidation, maxProjectIconDimension, maxProjectIconDimension)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: icon must be a PNG, JPEG or GIF image", ErrValidation)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return nil, fmt.Errorf("projects: encode icon: %w", err)
	}
	return encoded.Bytes(), nil
}
