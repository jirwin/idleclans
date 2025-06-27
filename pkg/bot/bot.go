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
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type Bot struct {
	session         *discordgo.Session
	plugins         []Plugin
	webhookHandlers map[string]WebhookHandler
	webhookServer   *http.Server
	mu              sync.RWMutex
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

func (b *Bot) RegisterWebhookHandler(pathPrefix string, handler WebhookHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.webhookHandlers == nil {
		b.webhookHandlers = make(map[string]WebhookHandler)
	}
	b.webhookHandlers[pathPrefix] = handler
}

func (b *Bot) startWebhookServer(port string) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Add webhook routes
	b.mu.RLock()
	for pathPrefix, handler := range b.webhookHandlers {
		// Register both with and without trailing slash to avoid 301 redirects
		router.Any(pathPrefix+"/*path", func(c *gin.Context) {
			b.handleWebhook(c, handler)
		})
		router.Any(pathPrefix, func(c *gin.Context) {
			b.handleWebhook(c, handler)
		})
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

func (b *Bot) handleWebhook(c *gin.Context, handler WebhookHandler) {
	ctx := c.Request.Context()
	l := ctxzap.Extract(ctx)

	l.Info(
		"Processing webhook request",
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("remote_addr", c.ClientIP()),
	)

	// Run handler in a goroutine to handle concurrently
	go func() {
		defer func() {
			if r := recover(); r != nil {
				l.Error("Webhook handler panicked", zap.Any("panic", r))
			}
		}()

		handler(b.session, c)
	}()

	// Return immediately to avoid blocking
	c.JSON(http.StatusOK, gin.H{"status": "processing"})
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
		session:         dg,
		webhookHandlers: make(map[string]WebhookHandler),
	}

	for _, opt := range opts {
		opt(b)
	}

	return b, nil
}
