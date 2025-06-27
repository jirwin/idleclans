package main

import (
	"context"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	icPlugin "github.com/jirwin/idleclans/plugins/idleclans"
	"go.uber.org/zap"
)

func initLogging(ctx context.Context) context.Context {
	l := zap.Must(zap.NewProduction())
	l.Sync()
	zap.ReplaceGlobals(l)

	return ctxzap.ToContext(ctx, l)
}

// getDiscordToken retrieves the Discord token from systemd credentials first,
// then falls back to environment variable
func getDiscordToken() string {
	// Check for systemd credential first
	if credsDir := os.Getenv("CREDENTIALS_DIRECTORY"); credsDir != "" {
		if tokenBytes, err := os.ReadFile(path.Join(credsDir, "discord_token")); err == nil {
			// Remove trailing newline if present
			token := string(tokenBytes)
			if len(token) > 0 && token[len(token)-1] == '\n' {
				token = token[:len(token)-1]
			}
			return token
		}
	}

	// Fall back to environment variable
	return os.Getenv("DISCORD_TOKEN")
}

func main() {
	ctx := context.Background()

	ctx = initLogging(ctx)
	l := ctxzap.Extract(ctx)

	discordToken := getDiscordToken()
	if discordToken == "" {
		l.Error("No Discord token found. Please set either systemd credential 'discord_token' or DISCORD_TOKEN environment variable")
		os.Exit(1)
	}

	b, err := bot.New(discordToken)
	if err != nil {
		l.Error("Error creating bot,", zap.Error(err))
		os.Exit(1)
	}

	b.LoadPlugins(ctx, []bot.Plugin{
		icPlugin.New(),
	})

	err = b.Start()
	if err != nil {
		l.Error("Error starting bot,", zap.Error(err))
		os.Exit(1)
	}

	l.Info("Bot is now running. Press CTRL+C to exit.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	b.Close(ctx)
}
