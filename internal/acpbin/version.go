package acpbin

const ACPVersion = "0.22.0"

// Checksums contains the SHA256 hash of each platform's release archive.
// Computed from: gh release view v0.22.0 --repo zed-industries/claude-agent-acp --json assets
var Checksums = map[string]string{
	"darwin-arm64":     "f39d8c835cce445c8d3e65d1e8a5f1c3575b56fef4db5bcd5427cbd651dc4b4b",
	"darwin-x64":       "692c9ec3e03675d3bdc1faec0a8beeb7c825fc037c23ff66fc0474949526e237",
	"linux-x64":        "cc900f7899c2322d48eca7dff7d2d2e5dbda4362a4e754169226236bc5b1a5fd",
	"linux-arm64":      "cc18ec09a2ca2b3149b4ede49023eee9096248578d53b95472ad0b69e57ac276",
	"linux-x64-musl":   "2115f760f78d6effc5348b7c00d611efacbc1c9002ce8576f1723415636c6bf1",
	"linux-arm64-musl": "5f175684af250818182e789f8fc6e20e04ac514c2d5f5f4a8cc791ac1d7d0aa8",
	"windows-x64":      "7db131be5d9c9023cd54303d84655eee2d6f37fe6450bb10ec4b543dee65d606",
	"windows-arm64":    "f28bac02e2482e2ac6aea87c950cfce4a98241721015b50b5b7c38a77d85075f",
}
