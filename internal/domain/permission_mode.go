package domain

const (
	ModeDefault           = "default"
	ModeAcceptEdits       = "acceptEdits"
	ModePlan              = "plan"
	ModeDontAsk           = "dontAsk"
	ModeBypassPermissions = "bypassPermissions"
)

func IsNonDefaultMode(mode string) bool {
	return mode != "" && mode != ModeDefault
}
