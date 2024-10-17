package idleclans

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"go.uber.org/zap"
)

func (p *plugin) pvmCmd(ctx context.Context) bot.MessageHandler {
	l := ctxzap.Extract(ctx)

	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}

		if strings.HasPrefix(m.Content, "!pvm") {
			playerName := strings.TrimSpace(strings.TrimPrefix(m.Content, "!pvm"))
			l.Info(
				"Processing pvm command",
				zap.String("player", playerName),
				zap.String("from", m.Author.Username),
				zap.String("channel", m.ChannelID),
			)

			price, err := p.client.GetPlayer(ctx, playerName)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Error getting player profile")
				return
			}

			s.ChannelMessageSend(
				m.ChannelID,
				fmt.Sprintf("```"+`
PvM Stats for %s:
+-----------------+------------------+----------------+
| Griffin: %4d   | Devil:    %4d   | Hades:   %4d  |
+-----------------+------------------+----------------+
| Zeus:    %4d   | Medusa:   %4d   | Chimera: %4d  |
+-----------------+------------------+----------------+
| Kronos:  %4d   | RotG:     %4d   | GotC:    %4d  |
+-----------------+------------------+----------------+
| Spider:  %4d   | Skeleton: %4d   | Golem:   %4d  |
+-----------------+------------------+----------------+
`+"```",
					playerName,
					price.PvmStats.Griffin,
					price.PvmStats.Devil,
					price.PvmStats.Hades,
					price.PvmStats.Zeus,
					price.PvmStats.Medusa,
					price.PvmStats.Chimera,
					price.PvmStats.Kronos,
					price.PvmStats.ReckoningOfTheGods,
					price.PvmStats.GuardiansOfTheCitadel,
					price.PvmStats.MalignantSpider,
					price.PvmStats.SkeletonWarrior,
					price.PvmStats.OtherworldlyGolem,
				),
			)
		}
	}
}
