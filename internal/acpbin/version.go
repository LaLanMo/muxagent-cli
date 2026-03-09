package acpbin

const ACPVersion = "0.20.2"

// Checksums contains the SHA256 hash of each platform's release archive.
// Computed from: gh release download v0.20.2 --repo zed-industries/claude-agent-acp
var Checksums = map[string]string{
	"darwin-arm64":     "6e9e07d3c882184557f78036cf40a3cc509ddbd83aae5909d4fd8f18883016b1",
	"darwin-x64":       "cba934fdcbc9cf390dd2c1f144d0a5d912a596938f8166bcc7ec7da66af9cc80",
	"linux-x64":        "56e46a980ff82938e7715a88e9778a1c89e67ccbcb0a36c7a5f4a0a9354ad4da",
	"linux-arm64":      "7738b07d908d9c72bb3ebd4308ec7e822da357ba5b2dd28d068fe4fb50dc5c52",
	"linux-x64-musl":   "a8b0a587a16b70eea52350dca4ce7ec3cc5c441180ab03e1505951263a1796bc",
	"linux-arm64-musl": "2fd6b8ce4d6f2c16a36430ffcea1ddbccf9261fb1069e3d3c0dd5b424a26b208",
	"windows-x64":      "e857b716c46816e6e2a167e3597d29f5f07260462a95e2063c1a111fe1689b00",
	"windows-arm64":    "359a376c048c9ea8938a952e993daaa34f5047ff25c29a24bf11e735b1a2482c",
}
