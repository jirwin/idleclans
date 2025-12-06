package idleclans

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"github.com/jirwin/idleclans/pkg/idleclans"
	"go.uber.org/zap"
)

type plugin struct {
	client       *idleclans.Client
	questsHandler *questsHandler
}

func (p *plugin) Name() string {
	return "idleclans"
}

func (p *plugin) Load(ctx context.Context) []bot.Option {
	p.client.Run(ctx)

	// Initialize quests handler
	var err error
	p.questsHandler, err = newQuestsHandler()
	if err != nil {
		// Log error but continue - quests will be unavailable
		ctxzap.Extract(ctx).Error("Failed to initialize quests handler", zap.Error(err))
	}

	opts := []bot.Option{
		bot.WithMessageHandler(p.priceCmd(ctx)),
		bot.WithMessageHandler(p.pvmCmd(ctx)),
		bot.WithMessageHandler(p.playerCmd(ctx)),
		bot.WithMessageHandler(p.questsCmd(ctx)),
		bot.WithMessageHandler(p.bossPingCmd(ctx)),
	}

	return opts
}

func (p *plugin) Close(ctx context.Context) error {
	var err error
	if p.questsHandler != nil {
		if closeErr := p.questsHandler.close(); closeErr != nil {
			err = closeErr
		}
	}
	if closeErr := p.client.Close(ctx); closeErr != nil {
		if err != nil {
			err = fmt.Errorf("multiple errors: %w; %w", err, closeErr)
		} else {
			err = closeErr
		}
	}
	return err
}

func New() bot.Plugin {
	return &plugin{
		client: idleclans.New(),
	}
}
