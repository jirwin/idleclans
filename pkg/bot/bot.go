package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
)

type Bot struct {
	session       *discordgo.Session
	plugins       []Plugin
	webhookSetups map[string]WebhookRouterSetup
	webhookServer *http.Server
	mu            sync.RWMutex
}

func (b *Bot) Start() error {
	// Start webhook server if port is configured
	if port := os.Getenv("WEBHOOK_PORT"); port != "" {
		go b.startWebhookServer(port)
	}

	return b.session.Open()
}

func (b *Bot) Close(ctx context.Context) error {
	var finalErr error

	// Close webhook server if running
	if b.webhookServer != nil {
		if err := b.webhookServer.Shutdown(ctx); err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("failed to shutdown webhook server: %w", err))
		}
	}

	for _, p := range b.plugins {
		err := p.Close(ctx)
		if err != nil {
			finalErr = errors.Join(finalErr, err)
		}
	}

	err := b.session.Close()
	if err != nil {
		err = errors.Join(finalErr, err)
	}

	return finalErr
}

func (b *Bot) RegisterWebhookRouter(pathPrefix string, setup WebhookRouterSetup) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.webhookSetups == nil {
		b.webhookSetups = make(map[string]WebhookRouterSetup)
	}
	b.webhookSetups[pathPrefix] = setup
}

func (b *Bot) startWebhookServer(port string) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Add webhook routes from plugins
	b.mu.RLock()
	for pathPrefix, setup := range b.webhookSetups {
		// Create a router group for this plugin
		group := router.Group(pathPrefix)

		// Let the plugin set up its routes
		setup(group, b.session)
	}
	b.mu.RUnlock()

	// 404 handler for unmatched routes
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
	})

	b.webhookServer = &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	if err := b.webhookServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		// Log error but don't panic since this is running in a goroutine
		fmt.Printf("Webhook server error: %v\n", err)
	}
}

func New(token string, opts ...Option) (*Bot, error) {
	if token == "" {
		return nil, errors.New("token is required")
	}
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		session:       dg,
		webhookSetups: make(map[string]WebhookRouterSetup),
	}

	for _, opt := range opts {
		opt(b)
	}

	return b, nil
}
