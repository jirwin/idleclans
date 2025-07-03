package idleclans

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
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

func (p *plugin) setupWebhookRoutes(ctx context.Context, channelID string) bot.WebhookRouterSetup {
	return func(router *gin.RouterGroup, s *discordgo.Session) {
		// Base route - show available actions
		router.GET("", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"available_actions": []string{"price", "pvm"},
				"usage":             "Use POST /webhook/idleclans/price or POST /webhook/idleclans/pvm",
			})
		})

		// Price webhook route
		router.POST("/clan", func(c *gin.Context) {
			p.handleClanActionWebhook(ctx, s, c, channelID)
		})
	}
}

type webhookMetadata struct {
	PlayerName    string `json:"playerName"`
	GameMode      string `json:"gameMode"`
	ClanName      string `json:"clanName"`
	Timestamp     string `json:"timestamp"`
	ClientVersion string `json:"clientVersion"`
}

type clanActionRequest struct {
	Metadata webhookMetadata   `json:"metadata"`
	Params   map[string]string `json:"params"`
}

func (p *plugin) handleClanActionWebhook(ctx context.Context, s *discordgo.Session, c *gin.Context, channelID string) {
	l := ctxzap.Extract(ctx)

	if channelID == "" {
		c.JSON(400, gin.H{"error": "channel_id is required"})
		return
	}

	req := &clanActionRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": fmt.Sprintf("invalid request: %s", err.Error())})
		return
	}

	l.Info(
		"Processing clan action webhook",
		zap.String("player_name", req.Metadata.PlayerName),
		zap.String("clan_name", req.Metadata.ClanName),
		zap.String("game_mode", req.Metadata.GameMode),
		zap.String("client_version", req.Metadata.ClientVersion),
		zap.String("timestamp", req.Metadata.Timestamp),
		zap.String("channel_id", channelID),
	)

	s.ChannelMessageSend(channelID, "Clan action received")
	c.JSON(200, gin.H{"status": "success"})
}
