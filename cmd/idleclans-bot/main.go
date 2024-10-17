package main

import (
	"context"
	"os"
	"os/signal"
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

func main() {
	ctx := context.Background()

	ctx = initLogging(ctx)
	l := ctxzap.Extract(ctx)

	discordToken := os.Getenv("DISCORD_TOKEN")
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
