package idleclans

import (
	"context"

	"github.com/jirwin/idleclans/pkg/bot"
	"github.com/jirwin/idleclans/pkg/idleclans"
)

type plugin struct {
	client *idleclans.Client
}

func (p *plugin) Name() string {
	return "idleclans"
}

func (p *plugin) Load(ctx context.Context) []bot.Option {
	p.client.Run(ctx)

	opts := []bot.Option{
		bot.WithMessageHandler(p.priceCmd(ctx)),
		bot.WithMessageHandler(p.pvmCmd(ctx)),
		bot.WithMessageHandler(p.playerCmd(ctx)),
		bot.WithWebhookRouter("/webhook/idleclans", p.setupWebhookRoutes(ctx)),
	}

	return opts
}

func (p *plugin) Close(ctx context.Context) error {
	return p.client.Close(ctx)
}

func New() bot.Plugin {
	return &plugin{
		client: idleclans.New(),
	}
}
