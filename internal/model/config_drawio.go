package model

// DrawioConfig controls the on-demand draw.io editor.
//
// The editor itself is not embedded in the binary: it is downloaded once on
// first use and unpacked under <DataDir>/vendor/drawio/. The only knob is where
// that download comes from, which matters on a network that cannot reach
// github.com or the public proxies — such a deployment points this at its own
// mirror.
type DrawioConfig struct {
	// DownloadURL is the complete URL of the release archive, tried before any
	// built-in source. Empty means "use the built-in mirrors, then github.com".
	//
	// A full URL rather than a prefix on purpose: an internal mirror usually
	// hosts the file directly, and the built-in proxies are already covered by
	// the fallback list.
	DownloadURL string `yaml:"download_url"`
}
