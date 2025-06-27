# Webhook Support

The Discord bot now supports HTTP webhooks that allow external services to trigger Discord messages through HTTP requests.

## Configuration

Set the `WEBHOOK_PORT` environment variable to enable webhook support:

```bash
export WEBHOOK_PORT=8080
```

## Plugin Webhook Routers

Plugins can register webhook routers in their `Load()` method:

```go
func (p *plugin) Load(ctx context.Context) []bot.Option {
    opts := []bot.Option{
        bot.WithMessageHandler(p.someMessageHandler(ctx)),
        bot.WithWebhookRouter("/webhook/myplugin", p.setupWebhookRoutes(ctx)),
    }
    return opts
}
```

## Webhook Router Setup

Plugins define a router setup function that receives a Gin router group and Discord session:

```go
func (p *plugin) setupWebhookRoutes(ctx context.Context) bot.WebhookRouterSetup {
    return func(router *gin.RouterGroup, s *discordgo.Session) {
        // Define your routes here
        router.GET("", func(c *gin.Context) {
            c.JSON(200, gin.H{"message": "Hello from webhook!"})
        })
        
        router.POST("/action", func(c *gin.Context) {
            // Extract channel ID
            channelID := c.GetHeader("X-Discord-Channel-ID")
            if channelID == "" {
                c.JSON(400, gin.H{"error": "channel_id required"})
                return
            }
            
            // Send message to Discord
            s.ChannelMessageSend(channelID, "Webhook triggered!")
            c.JSON(200, gin.H{"status": "success"})
        })
    }
}
```

## Example Usage

### Get Available Actions

```bash
curl http://localhost:8080/webhook/idleclans
```

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

- **Router-Based**: Plugins have full control over their HTTP routing using Gin
- **Discord Integration**: Full access to Discord session for sending messages
- **Flexible Routing**: Use any HTTP method, middleware, or Gin features
- **Logging**: All webhook requests are logged with structured logging
- **Error Handling**: Proper error responses and panic recovery
- **404 Handling**: Unmatched routes return 404
- **Graceful Shutdown**: Webhook server shuts down properly with the bot

## Adding New Webhook Routes

To add webhook routes to a plugin:

1. Create a `setupWebhookRoutes` function that returns a `bot.WebhookRouterSetup`
2. Register it in the plugin's `Load()` method using `bot.WithWebhookRouter()`
3. Define your routes on the provided router group
4. Use the Discord session to send messages

Example:

```go
func (p *plugin) setupWebhookRoutes(ctx context.Context) bot.WebhookRouterSetup {
    return func(router *gin.RouterGroup, s *discordgo.Session) {
        // Base route
        router.GET("", func(c *gin.Context) {
            c.JSON(200, gin.H{"status": "plugin ready"})
        })
        
        // Custom action route
        router.POST("/custom", func(c *gin.Context) {
            channelID := c.GetHeader("X-Discord-Channel-ID")
            if channelID == "" {
                c.JSON(400, gin.H{"error": "channel_id required"})
                return
            }
            
            // Your custom logic here
            s.ChannelMessageSend(channelID, "Custom webhook action!")
            c.JSON(200, gin.H{"status": "success"})
        })
        
        // You can add middleware, sub-routes, etc.
        admin := router.Group("/admin")
        admin.Use(func(c *gin.Context) {
            // Admin authentication middleware
            c.Next()
        })
        admin.POST("/action", func(c *gin.Context) {
            // Admin-only action
        })
    }
}
```

## Security Considerations

- The webhook server is currently unauthenticated
- Consider adding authentication (API keys, tokens) for production use
- Validate channel IDs to ensure messages are sent to intended channels
- Rate limiting may be needed for high-traffic scenarios
- Use HTTPS in production environments 