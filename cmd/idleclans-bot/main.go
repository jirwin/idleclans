package main

import (
	"context"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/jirwin/idleclans/pkg/bot"
	"github.com/jirwin/idleclans/pkg/quests"
	"github.com/jirwin/idleclans/pkg/web"
	icPlugin "github.com/jirwin/idleclans/plugins/idleclans"
	"go.uber.org/zap"
)

// botAdapter adapts the Bot to the DiscordMessageSender interface
type botAdapter struct {
	bot *bot.Bot
}

func (a *botAdapter) SendMessage(channelID, message string) error {
	return a.bot.SendMessage(channelID, message)
}

func (a *botAdapter) SendMessageWithEmbed(channelID, content string, embed *web.DiscordEmbed) error {
	// Convert web.DiscordEmbed to discordgo.MessageEmbed
	dgEmbed := &discordgo.MessageEmbed{
		Title:       embed.Title,
		Description: embed.Description,
		Color:       embed.Color,
	}

	for _, field := range embed.Fields {
		dgEmbed.Fields = append(dgEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   field.Name,
			Value:  field.Value,
			Inline: field.Inline,
		})
	}

	return a.bot.SendMessageWithEmbed(channelID, content, dgEmbed)
}

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

// getCredential retrieves a credential from systemd credentials first,
// then falls back to environment variable
func getCredential(credName, envName string) string {
	if credsDir := os.Getenv("CREDENTIALS_DIRECTORY"); credsDir != "" {
		if data, err := os.ReadFile(path.Join(credsDir, credName)); err == nil {
			value := string(data)
			if len(value) > 0 && value[len(value)-1] == '\n' {
				value = value[:len(value)-1]
			}
			return value
		}
	}
	return os.Getenv(envName)
}

func getEnvInt(name string, defaultVal int) int {
	val := os.Getenv(name)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

func getEnvString(name, defaultVal string) string {
	val := os.Getenv(name)
	if val == "" {
		return defaultVal
	}
	return val
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

	// Initialize web server if configured
	var webServer *web.Server
	discordClientID := getCredential("discord_client_id", "DISCORD_CLIENT_ID")
	discordClientSecret := getCredential("discord_client_secret", "DISCORD_CLIENT_SECRET")

	if discordClientID != "" && discordClientSecret != "" {
		// Get database connection string from environment
		dbURL := os.Getenv("DATABASE_URL")
		if dbURL == "" {
			// Fallback to SQLite for backward compatibility
			sqlitePath := getEnvString("SQLITE_DB_PATH", "quests.db")
			dbURL = "sqlite://" + sqlitePath
		}
		
		// Create database connection for web server
		webDB, err := quests.NewDB(dbURL)
		if err != nil {
			l.Error("Failed to open database for web server", zap.Error(err), zap.String("db_url", dbURL))
			os.Exit(1)
		}
		defer webDB.Close()

		webConfig := &web.Config{
			PublicPort:          getEnvInt("WEB_PUBLIC_PORT", 8080),
			AdminPort:           getEnvInt("WEB_ADMIN_PORT", 8081),
			BaseURL:             os.Getenv("WEB_BASE_URL"),
			DiscordClientID:     discordClientID,
			DiscordClientSecret: discordClientSecret,
			SessionSecret:       getCredential("session_secret", "SESSION_SECRET"),
			RequiredGuild:       os.Getenv("REQUIRED_GUILD"),
			DiscordChannelID:    os.Getenv("DISCORD_CHANNEL_ID"),
			OpenAIAPIKey:        getCredential("openai_api_key", "OPENAI_API_KEY"),
			OpenAIModel:         getEnvString("OPENAI_MODEL", "gpt-4o"),
		}

		if webConfig.BaseURL == "" {
			webConfig.BaseURL = "http://localhost:" + strconv.Itoa(webConfig.PublicPort)
		}

		webServer, err = web.NewServer(webConfig, webDB, l)
		if err != nil {
			l.Error("Failed to create web server", zap.Error(err))
			os.Exit(1)
		}

		if err := webServer.Start(ctx); err != nil {
			l.Error("Failed to start web server", zap.Error(err))
			os.Exit(1)
		}

		l.Info("Web server started",
			zap.Int("public_port", webConfig.PublicPort),
			zap.Int("admin_port", webConfig.AdminPort),
			zap.String("required_guild", webConfig.RequiredGuild),
		)
	} else {
		l.Info("Web server disabled (DISCORD_CLIENT_ID and DISCORD_CLIENT_SECRET not set)")
	}

	b, err := bot.New(discordToken)
	if err != nil {
		l.Error("Error creating bot,", zap.Error(err))
		os.Exit(1)
	}

	// Create the plugin
	plugin := icPlugin.New()

	// If web server is running, connect notifications
	if webServer != nil {
		if p, ok := plugin.(interface {
			SetNotifyFunc(icPlugin.DataChangeNotifier)
		}); ok {
			p.SetNotifyFunc(webServer.NotifyDataChange)
			l.Info("Connected web server notifications to bot plugin")
		} else {
			l.Warn("Failed to connect web server notifications - type assertion failed")
		}
	}

	b.LoadPlugins(ctx, []bot.Plugin{
		plugin,
	})

	err = b.Start()
	if err != nil {
		l.Error("Error starting bot,", zap.Error(err))
		os.Exit(1)
	}

	// Connect Discord sender to web server after bot starts
	if webServer != nil {
		webServer.SetDiscordSender(&botAdapter{bot: b})
		l.Info("Connected Discord message sender to web server")
	}

	l.Info("Bot is now running. Press CTRL+C to exit.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	l.Info("Shutting down...")

	if webServer != nil {
		if err := webServer.Stop(ctx); err != nil {
			l.Error("Error stopping web server", zap.Error(err))
		}
	}

	b.Close(ctx)
}
