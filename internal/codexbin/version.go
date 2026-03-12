package codexbin

const ACPVersion = "0.9.5"

// Checksums contains the SHA256 hash of each platform's release archive.
// Computed from: GitHub release asset digests for zed-industries/codex-acp v0.9.5.
var Checksums = map[string]string{
	"aarch64-apple-darwin":       "cf330c8d5cf8d5f1a155a92978172ad00e0001f982c5bcd92b5483a16966364d",
	"x86_64-apple-darwin":        "34d965fa51c0eecfa895cf18b71372d2e3bc44561f7eaac52e17904db46dee9b",
	"x86_64-unknown-linux-gnu":   "49ef481a78836384a4c0aa994acf39822c549ef0fa20d4d610ce002e3b9808e0",
	"aarch64-unknown-linux-gnu":  "3dbf57dcec027a61c8f24e40e952e6405b1a0ee30bf728ad409c77d25bd05a71",
	"x86_64-unknown-linux-musl":  "8cb879c2bcc3d8a9178f3751253eb6ee88cb9330c6172db53620d8a7610b2524",
	"aarch64-unknown-linux-musl": "1547d193121e393ab4b57097c2206b5d519dba7a147d07fb271cfbcbf4e5d290",
	"x86_64-pc-windows-msvc":     "97659d6043ae83948e49c0abd98e29aa7ce6e1ebe1182fbda73d4864ec0db812",
	"aarch64-pc-windows-msvc":    "a4515154d0d9b6a33b6b7196d59233cf153d5c24469c1f30992408c346386ed1",
}
