# Webhook Support

The Discord bot now supports HTTP webhooks that allow external services to trigger Discord messages through HTTP requests.

## Configuration

Set the `WEBHOOK_PORT` environment variable to enable webhook support:

```bash
export WEBHOOK_PORT=8080
```

## Plugin Webhook Handlers

Plugins can register webhook handlers in their `Load()` method:

```go
func (p *plugin) Load(ctx context.Context) []bot.Option {
    opts := []bot.Option{
        bot.WithMessageHandler(p.someMessageHandler(ctx)),
        bot.WithWebhookHandler("/webhook/myplugin", p.webhookHandler(ctx)),
    }
    return opts
}
```

## Webhook Handler Implementation

Webhook handlers receive a Discord session and Gin context:

```go
func (p *plugin) webhookHandler(ctx context.Context) bot.WebhookHandler {
    return func(s *discordgo.Session, c *gin.Context) {
        // Extract channel ID from query parameter or header
        channelID := c.Query("channel_id")
        if channelID == "" {
            channelID = c.GetHeader("X-Discord-Channel-ID")
        }

        if channelID == "" {
            c.JSON(400, gin.H{"error": "channel_id is required"})
            return
        }

        // Handle the webhook request
        // Send message to Discord
        s.ChannelMessageSend(channelID, "Hello from webhook!")
        
        // Return response
        c.JSON(200, gin.H{"status": "success"})
    }
}
```

## Example Usage

### Price Webhook

Send a POST request to get item prices:

```bash
curl -X POST http://localhost:8080/webhook/idleclans/price \
  -H "Content-Type: application/json" \
  -H "X-Discord-Channel-ID: YOUR_CHANNEL_ID" \
  -d '{"item_id": "dragon_scimitar"}'
```

### PVM Webhook

Send a POST request for PVM updates:

```bash
curl -X POST http://localhost:8080/webhook/idleclans/pvm \
  -H "Content-Type: application/json" \
  -H "X-Discord-Channel-ID: YOUR_CHANNEL_ID" \
  -d '{"player_name": "PlayerName"}'
```

## Features

- **Concurrent Processing**: Webhook handlers run concurrently in goroutines
- **Path Prefix Matching**: Routes are matched by path prefix
- **Discord Integration**: Full access to Discord session for sending messages
- **Logging**: All webhook requests are logged with structured logging
- **Error Handling**: Panic recovery and proper error responses
- **404 Handling**: Unmatched routes return 404
- **Graceful Shutdown**: Webhook server shuts down properly with the bot

## Security Considerations

- The webhook server is currently unauthenticated
- Consider adding authentication (API keys, tokens) for production use
- Validate channel IDs to ensure messages are sent to intended channels
- Rate limiting may be needed for high-traffic scenarios

## Adding New Webhook Handlers

To add a new webhook handler to a plugin:

1. Create a handler function that implements `bot.WebhookHandler`
2. Register it in the plugin's `Load()` method using `bot.WithWebhookHandler()`
3. Handle the request and send appropriate Discord messages
4. Return proper HTTP responses

Example:

```go
func (p *plugin) myWebhookHandler(ctx context.Context) bot.WebhookHandler {
    return func(s *discordgo.Session, c *gin.Context) {
        channelID := c.GetHeader("X-Discord-Channel-ID")
        if channelID == "" {
            c.JSON(400, gin.H{"error": "channel_id required"})
            return
        }

        // Your webhook logic here
        s.ChannelMessageSend(channelID, "Webhook triggered!")
        c.JSON(200, gin.H{"status": "success"})
    }
}
``` 