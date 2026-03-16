package appwireconv

import (
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

func ContentBlocksFromWire(blocks []appwire.PromptContentBlock) []domain.ContentBlock {
	if len(blocks) == 0 {
		return nil
	}

	converted := make([]domain.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		converted = append(converted, domain.ContentBlock{
			Type:     block.Type,
			Text:     block.Text,
			MimeType: block.MimeType,
			Data:     block.Data,
			URI:      block.URI,
		})
	}
	return converted
}
