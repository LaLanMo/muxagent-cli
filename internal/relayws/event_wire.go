package relayws

import (
	"encoding/json"

	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

func marshalEvent(event appwire.Event) ([]byte, error) {
	return json.Marshal(event)
}
