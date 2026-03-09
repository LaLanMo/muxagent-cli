package main

import "github.com/LaLanMo/muxagent-cli/cmd/muxagent/update"

func main() {
	update.CleanupUpdatedBackup()
	Execute()
}
