package acpbin

const ACPVersion = "0.21.0"

// Checksums contains the SHA256 hash of each platform's release archive.
// Computed from: gh release view v0.21.0 --repo zed-industries/claude-agent-acp --json assets
var Checksums = map[string]string{
	"darwin-arm64":     "fa3a71ab2afff9dc9e1e91bcf3f43c7e9fd05c63bef9e951b3d4815e2e8a9f32",
	"darwin-x64":       "50b89851269c750ed24db2e0415cf3b80df19f96990c82989d57b3d70c0fd533",
	"linux-x64":        "dfbf051f93a233281791acdb61e06955ada49f51a1c6bb5ae800f4b74055a3d0",
	"linux-arm64":      "11df5fc67e80b3d52d0c99ded85866bf9cb7849a73e08cf3dbf09ee6cadb8005",
	"linux-x64-musl":   "f9df08f70003c995b43056c7d03fd7eebbb3a1eb27506470eec5c39c54848343",
	"linux-arm64-musl": "4c82e69677af4d2fc898891f13423b712f0e9d54afcc42d797acab7cb76e076d",
	"windows-x64":      "3fe1edbf1c10e05419a1f4d9446f4fe7405b1dba07a20e213814f0ca700a7b38",
	"windows-arm64":    "da2199be1dbe1a1332a58affb9dc2dda1dd0552cfb3047fae42f0c002923dd32",
}
