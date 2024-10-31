package idleclans

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"github.com/jirwin/idleclans/pkg/idleclans"
	"go.uber.org/zap"
)

func generateSkillGrid(playerName string, skills map[string]float64, maxWidth int) string {
	skillNames := make([]string, 0, len(skills))
	for skill := range skills {
		skillNames = append(skillNames, skill)
	}
	sort.Strings(skillNames)

	maxSkillLength := 0
	for _, skill := range skillNames {
		if len(skill) > maxSkillLength {
			maxSkillLength = len(skill)
		}
	}
	cellContentWidth := maxSkillLength + 3 + 2
	cellWidth := cellContentWidth + 2
	columns := maxWidth / cellWidth
	rows := int(math.Ceil(float64(len(skillNames)) / float64(columns)))

	horizontalBorder := strings.Repeat("-", cellContentWidth+2)
	rowSeparator := "+" + strings.Repeat(horizontalBorder+"+", columns) + "\n"

	var output strings.Builder
	output.WriteString("```\n")
	output.WriteString(fmt.Sprintf("%s:\n", playerName))
	output.WriteString(rowSeparator)
	i := 0
	for r := 0; r < rows; r++ {
		output.WriteString("|")
		for c := 0; c < columns; c++ {
			if i < len(skillNames) {
				skill := skillNames[i]
				experience := skills[skill]
				level, _ := idleclans.GetSkillLevel(int(experience))
				cellContent := fmt.Sprintf(" %-*s %3d ", maxSkillLength, skill, level)
				output.WriteString(cellContent + strings.Repeat(" ", len(cellContent)-cellContentWidth) + "|")
				i++
			} else {
				output.WriteString(strings.Repeat(" ", cellContentWidth+2) + "|")
			}
		}
		output.WriteString("\n" + rowSeparator)
	}
	output.WriteString("```\n")

	return output.String()
}

func (p *plugin) playerCmd(ctx context.Context) bot.MessageHandler {
	l := ctxzap.Extract(ctx)

	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}

		if strings.HasPrefix(m.Content, "!player") {
			playerName := strings.TrimSpace(strings.TrimPrefix(m.Content, "!player"))
			l.Info(
				"Processing player command",
				zap.String("player", playerName),
				zap.String("from", m.Author.Username),
				zap.String("channel", m.ChannelID),
			)

			player, err := p.client.GetSimplePlayer(ctx, playerName)
			if err != nil {
				l.Error("Error getting player profile", zap.Error(err))
				s.ChannelMessageSend(m.ChannelID, "Error getting player profile")
				return
			}

			s.ChannelMessageSend(
				m.ChannelID,
				generateSkillGrid(playerName, player.Skills, 65),
			)
		}
	}
}

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
