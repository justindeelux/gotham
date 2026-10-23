package updatecore

import "time"

type Release struct {
	Version              string
	Tag                  string
	Channel              string
	Prerelease           bool
	Notes                string
	PublishedAt          time.Time
	Arch                 string
	AssetName            string
	AssetURL             string
	ManifestName         string
	ManifestURL          string
	ManifestSignatureURL string
	AssetSize            int64
	HTMLURL              string
	SHA256               string
}
