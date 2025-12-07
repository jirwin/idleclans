package bot

import (
	"context"
	"errors"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	session *discordgo.Session
	plugins []Plugin
}

func (b *Bot) Start() error {
	return b.session.Open()
}

func (b *Bot) Close(ctx context.Context) error {
	var finalErr error

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

func New(token string, opts ...Option) (*Bot, error) {
	if token == "" {
		return nil, errors.New("token is required")
	}
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		session: dg,
	}

	for _, opt := range opts {
		opt(b)
	}

	return b, nil
}

// SendMessage sends a message to a Discord channel
func (b *Bot) SendMessage(channelID, message string) error {
	_, err := b.session.ChannelMessageSend(channelID, message)
	return err
}

// SendMessageWithEmbed sends a message with an embed to a Discord channel
func (b *Bot) SendMessageWithEmbed(channelID, content string, embed *discordgo.MessageEmbed) error {
	_, err := b.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		Embeds:  []*discordgo.MessageEmbed{embed},
	})
	return err
}
