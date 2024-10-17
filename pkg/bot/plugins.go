package bot

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

type Plugin interface {
	Name() string
	Load(ctx context.Context) []Option
	Close(ctx context.Context) error
}

func (b *Bot) LoadPlugins(ctx context.Context, plugins []Plugin) {
	var opts []Option
	for _, p := range plugins {
		b.plugins = append(b.plugins, p)
		opts = append(opts, p.Load(ctx)...)
	}

	for _, opt := range opts {
		opt(b)
	}
}

type Option func(*Bot)

type MessageHandler func(*discordgo.Session, *discordgo.MessageCreate)

func WithMessageHandler(handler func(*discordgo.Session, *discordgo.MessageCreate)) Option {
	return func(b *Bot) {
		b.session.AddHandler(handler)
	}
}
