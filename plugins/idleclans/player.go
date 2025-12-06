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
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

			// Define valid skill names based on actual API response
			// Skills come from API with capitalized names, but we'll normalize to lowercase for comparison
			validSkills := map[string]bool{
				"rigour":        true,
				"strength":      true,
				"defence":       true,
				"archery":       true,
				"magic":         true,
				"health":        true,
				"crafting":      true,
				"woodcutting":   true,
				"carpentry":     true,
				"fishing":       true,
				"cooking":       true,
				"mining":        true,
				"smithing":      true,
				"foraging":      true,
				"farming":       true,
				"agility":       true,
				"plundering":    true,
				"enchanting":    true,
				"brewing":       true,
				"exterminating": true,
			}

			// Organize skills into groups for better display
			combatSkills := []string{"rigour", "strength", "defence", "archery", "magic", "health"}
			gatheringSkills := []string{"woodcutting", "mining", "fishing", "foraging", "farming"}
			craftingSkills := []string{"crafting", "carpentry", "cooking", "smithing", "brewing", "enchanting"}
			otherSkills := []string{"agility", "plundering", "exterminating"}

			// Build fields for each skill category
			fields := make([]*discordgo.MessageEmbedField, 0)
			titleCaser := cases.Title(language.English)

			// Helper function to get skill experience, handling case variations
			getSkillExp := func(skillName string) (float64, bool) {
				// Try lowercase first
				if exp, ok := player.Skills[skillName]; ok {
					return exp, true
				}
				// Try capitalized version
				if exp, ok := player.Skills[titleCaser.String(skillName)]; ok {
					return exp, true
				}
				// Try as-is (in case it's already in the map with different casing)
				for k, v := range player.Skills {
					if strings.EqualFold(k, skillName) {
						return v, true
					}
				}
				return 0, false
			}

			// Combat skills
			var combatValue strings.Builder
			for _, skillName := range combatSkills {
				if exp, ok := getSkillExp(skillName); ok {
					level, _ := idleclans.GetSkillLevel(int(exp))
					combatValue.WriteString(fmt.Sprintf("**%s**: %d\n", titleCaser.String(skillName), level))
				}
			}
			if combatValue.Len() > 0 {
				fields = append(fields, &discordgo.MessageEmbedField{
					Name:   "Combat",
					Value:  combatValue.String(),
					Inline: true,
				})
			}

			// Gathering skills
			var gatheringValue strings.Builder
			for _, skillName := range gatheringSkills {
				if exp, ok := getSkillExp(skillName); ok {
					level, _ := idleclans.GetSkillLevel(int(exp))
					gatheringValue.WriteString(fmt.Sprintf("**%s**: %d\n", titleCaser.String(skillName), level))
				}
			}
			if gatheringValue.Len() > 0 {
				fields = append(fields, &discordgo.MessageEmbedField{
					Name:   "Gathering",
					Value:  gatheringValue.String(),
					Inline: true,
				})
			}

			// Crafting skills
			var craftingValue strings.Builder
			for _, skillName := range craftingSkills {
				if exp, ok := getSkillExp(skillName); ok {
					level, _ := idleclans.GetSkillLevel(int(exp))
					craftingValue.WriteString(fmt.Sprintf("**%s**: %d\n", titleCaser.String(skillName), level))
				}
			}
			if craftingValue.Len() > 0 {
				fields = append(fields, &discordgo.MessageEmbedField{
					Name:   "Crafting",
					Value:  craftingValue.String(),
					Inline: true,
				})
			}

			// Other skills
			var otherValue strings.Builder
			for _, skillName := range otherSkills {
				if exp, ok := getSkillExp(skillName); ok {
					level, _ := idleclans.GetSkillLevel(int(exp))
					otherValue.WriteString(fmt.Sprintf("**%s**: %d\n", titleCaser.String(skillName), level))
				}
			}

			// Also include any skills not in the predefined lists
			// Only include skills that are valid according to the API structure
			for skillName, exp := range player.Skills {
				// Normalize skill name to lowercase for validation
				normalizedSkill := strings.ToLower(skillName)

				// Skip invalid skill names
				if !validSkills[normalizedSkill] {
					l.Warn("Unknown skill name from API", zap.String("skill", skillName))
					continue
				}

				// Check if already in a category
				found := false
				for _, list := range [][]string{combatSkills, gatheringSkills, craftingSkills, otherSkills} {
					for _, s := range list {
						if normalizedSkill == s {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					level, _ := idleclans.GetSkillLevel(int(exp))
					otherValue.WriteString(fmt.Sprintf("**%s**: %d\n", titleCaser.String(normalizedSkill), level))
				}
			}
			if otherValue.Len() > 0 {
				fields = append(fields, &discordgo.MessageEmbedField{
					Name:   "Other",
					Value:  otherValue.String(),
					Inline: true,
				})
			}

			embed := &discordgo.MessageEmbed{
				Title:       fmt.Sprintf("Player: %s", playerName),
				Description: "Skill Levels",
				Color:       0x3498db, // Blue color
				Fields:      fields,
			}

			s.ChannelMessageSendEmbeds(m.ChannelID, []*discordgo.MessageEmbed{embed})
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
