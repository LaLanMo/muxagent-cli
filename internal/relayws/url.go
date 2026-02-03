package relayws

import "strings"

func HTTPURLFromWS(wsURL string) string {
	httpURL := wsURL
	if strings.HasPrefix(httpURL, "ws://") {
		httpURL = "http://" + strings.TrimPrefix(httpURL, "ws://")
	}
	if strings.HasPrefix(httpURL, "wss://") {
		httpURL = "https://" + strings.TrimPrefix(httpURL, "wss://")
	}
	return strings.TrimSuffix(httpURL, "/ws")
}
