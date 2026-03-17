package codexbin

const ACPVersion = "0.10.0"

// Checksums contains the SHA256 hash of each platform's release archive.
// Computed from: GitHub release asset digests for zed-industries/codex-acp v0.10.0.
var Checksums = map[string]string{
	"aarch64-apple-darwin":       "691f2a3fca24e6f2b9b3bde1a1181f3122be17fcce990a9f7f1c750fb3668422",
	"x86_64-apple-darwin":        "0eb29de065f73334016d2b1046e4f2b52529b769d324d45a805e316d73d7e4ba",
	"x86_64-unknown-linux-gnu":   "f5d0c1bcbbb361a92c4f52168625fe5fbc845cc9e48ae1c3fd150115cd11b415",
	"aarch64-unknown-linux-gnu":  "bb20efa584ad7f89cd0eaac09ec8fd1181cd8e818ad08ef22c2b0db3d1c736dd",
	"x86_64-unknown-linux-musl":  "6e87fa19d33890b54ac428ff328dc0d1d91a2b522d8298a38a394e12dacda0b0",
	"aarch64-unknown-linux-musl": "1567e0e090157393faa54db6afe17b6df188d1c77924aa89655c8cf6295cc541",
	"x86_64-pc-windows-msvc":     "197a4daf5c163f3b491b19073c18d7177d67bf5179212811caa5f88b3e92d93e",
	"aarch64-pc-windows-msvc":    "9ed9af77c6fd6458149fd328f7e4b007691d8cf973aac3737c47b9fdbf1a9780",
}
