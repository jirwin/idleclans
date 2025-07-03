package idleclans

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"go.uber.org/zap"
)

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

	if req.Params == nil {
		c.JSON(400, gin.H{"error": "params are required"})
		return
	}

	action := req.Params["action"]
	if action == "" {
		c.JSON(400, gin.H{"error": "action is required"})
		return
	}

	switch action {
	case "member-logged-in":
		p.handleMemberLoggedIn(ctx, s, c, channelID, req.Params)
	case "member-logged-out":
		p.handleMemberLoggedOut(ctx, s, c, channelID, req.Params)
	default:
		s.ChannelMessageSend(channelID, fmt.Sprintf("Receieved webhook with unknown action: %s", action))
	}

	c.JSON(200, gin.H{"status": "success"})
}

func (p *plugin) handleMemberLoggedIn(ctx context.Context, s *discordgo.Session, c *gin.Context, channelID string, params map[string]string) {
	l := ctxzap.Extract(ctx)

	username, ok := params["username"]
	if !ok {
		l.Error("username is required")
		return
	}

	s.ChannelMessageSend(channelID, fmt.Sprintf("%s logged in", username))
}

func (p *plugin) handleMemberLoggedOut(ctx context.Context, s *discordgo.Session, c *gin.Context, channelID string, params map[string]string) {
	l := ctxzap.Extract(ctx)

	username, ok := params["username"]
	if !ok {
		l.Error("username is required")
		return
	}

	s.ChannelMessageSend(channelID, fmt.Sprintf("%s logged out", username))
}
