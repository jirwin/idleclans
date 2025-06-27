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

func (p *plugin) setupWebhookRoutes(ctx context.Context) bot.WebhookRouterSetup {
	return func(router *gin.RouterGroup, s *discordgo.Session) {
		// Base route - show available actions
		router.GET("", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"available_actions": []string{"price", "pvm"},
				"usage":             "Use POST /webhook/idleclans/price or POST /webhook/idleclans/pvm",
			})
		})

		// Price webhook route
		router.POST("/price", func(c *gin.Context) {
			p.handlePriceWebhook(ctx, s, c)
		})

		// PVM webhook route
		router.POST("/pvm", func(c *gin.Context) {
			p.handlePvmWebhook(ctx, s, c)
		})
	}
}

func (p *plugin) handlePriceWebhook(ctx context.Context, s *discordgo.Session, c *gin.Context) {
	l := ctxzap.Extract(ctx)

	// Extract channel ID from query parameter or header
	channelID := c.Query("channel_id")
	if channelID == "" {
		channelID = c.GetHeader("X-Discord-Channel-ID")
	}

	if channelID == "" {
		c.JSON(400, gin.H{"error": "channel_id is required"})
		return
	}

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

func (p *plugin) handlePvmWebhook(ctx context.Context, s *discordgo.Session, c *gin.Context) {
	l := ctxzap.Extract(ctx)

	// Extract channel ID from query parameter or header
	channelID := c.Query("channel_id")
	if channelID == "" {
		channelID = c.GetHeader("X-Discord-Channel-ID")
	}

	if channelID == "" {
		c.JSON(400, gin.H{"error": "channel_id is required"})
		return
	}

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
