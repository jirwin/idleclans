package idleclans

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"go.uber.org/zap"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func (p *plugin) priceCmd(ctx context.Context) bot.MessageHandler {
	l := ctxzap.Extract(ctx)
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}

		if strings.HasPrefix(m.Content, "!price") {
			itemID := strings.TrimSpace(strings.TrimPrefix(m.Content, "!price"))

			l.Info(
				"Processing pvm command",
				zap.String("item_id", itemID),
				zap.String("from", m.Author.Username),
				zap.String("channel", m.ChannelID),
			)

			price, err := p.client.GetLatestPrice(ctx, itemID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting price: %s", err.Error()))
				return
			}
			p := message.NewPrinter(language.English)
			s.ChannelMessageSend(
				m.ChannelID,
				p.Sprintf(
					"Lowest Sell: %dg (%d)\nHighest Buy: %dg (%d)",
					price.LowestSellPrice,
					price.LowestPriceVolume,
					price.HighestBuyPrice,
					price.HighestPriceVolume,
				),
			)
		}
	}
}
