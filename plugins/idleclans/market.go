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

func (p *plugin) webhookHandler(ctx context.Context) bot.WebhookHandler {
	return func(s *discordgo.Session, c *gin.Context) {
		l := ctxzap.Extract(ctx)
		l.Info("Processing webhook", zap.String("path", c.Request.URL.Path))
		// Extract channel ID from query parameter or header
		channelID := c.Query("channel_id")
		if channelID == "" {
			channelID = c.GetHeader("X-Discord-Channel-ID")
		}

		if channelID == "" {
			c.JSON(400, gin.H{"error": "channel_id is required"})
			return
		}

		// Handle different webhook actions based on path
		action := strings.TrimPrefix(c.Request.URL.Path, "/webhook/idleclans")

		switch action {
		case "", "/":
			// Base path - return available actions
			c.JSON(200, gin.H{
				"available_actions": []string{"price", "pvm"},
				"usage":             "Use /webhook/idleclans/price or /webhook/idleclans/pvm",
			})
		case "/price":
			p.handlePriceWebhook(ctx, s, c, channelID)
		case "/pvm":
			p.handlePvmWebhook(ctx, s, c, channelID)
		default:
			c.JSON(404, gin.H{"error": "unknown action"})
		}
	}
}

func (p *plugin) handlePriceWebhook(ctx context.Context, s *discordgo.Session, c *gin.Context, channelID string) {
	l := ctxzap.Extract(ctx)

	var req struct {
		ItemID string `json:"item_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": fmt.Sprintf("invalid request: %s", err.Error())})
		return
	}

	l.Info(
		"Processing price webhook",
		zap.String("item_id", req.ItemID),
		zap.String("channel_id", channelID),
	)

	price, err := p.client.GetLatestPrice(ctx, req.ItemID)
	if err != nil {
		msg := fmt.Sprintf("Error getting price for %s: %s", req.ItemID, err.Error())
		s.ChannelMessageSend(channelID, msg)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	printer := message.NewPrinter(language.English)
	msg := printer.Sprintf(
		"**Price Update for %s**\nLowest Sell: %dg (%d)\nHighest Buy: %dg (%d)",
		req.ItemID,
		price.LowestSellPrice,
		price.LowestPriceVolume,
		price.HighestBuyPrice,
		price.HighestPriceVolume,
	)

	s.ChannelMessageSend(channelID, msg)
	c.JSON(200, gin.H{"status": "message sent", "price": price})
}

func (p *plugin) handlePvmWebhook(ctx context.Context, s *discordgo.Session, c *gin.Context, channelID string) {
	l := ctxzap.Extract(ctx)

	var req struct {
		PlayerName string `json:"player_name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": fmt.Sprintf("invalid request: %s", err.Error())})
		return
	}

	l.Info(
		"Processing PVM webhook",
		zap.String("player_name", req.PlayerName),
		zap.String("channel_id", channelID),
	)

	// This is a placeholder - you would implement the actual PVM logic here
	msg := fmt.Sprintf("**PVM Update for %s**\nThis is a webhook-triggered PVM update!", req.PlayerName)

	s.ChannelMessageSend(channelID, msg)
	c.JSON(200, gin.H{"status": "message sent", "player": req.PlayerName})
}
