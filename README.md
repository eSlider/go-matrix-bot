# go-matrix-bot

Go library for building [Matrix](https://matrix.org/) bots with end-to-end encryption support, built on top of [mautrix-go](https://github.com/mautrix/go).

Pairs with [go-ollama](https://github.com/eSlider/go-ollama) to build AI-powered chat assistants.

## Features

- Simple, handler-based bot API — register functions, not interfaces
- End-to-end encryption out of the box (via mautrix crypto helper)
- Auto-join rooms on invite
- Send plain text, HTML, and markdown-formatted messages
- Mention users in replies
- Markdown-to-HTML conversion built in
- Graceful shutdown with context cancellation
- Works with [go-ollama](https://github.com/eSlider/go-ollama) for AI responses

## Installation

```bash
go get github.com/eslider/go-matrix-bot
```

For AI features (optional):

```bash
go get github.com/eslider/go-ollama
```

**System dependency** (required for encryption):

```bash
# Debian/Ubuntu
sudo apt-get install libolm-dev
```

---

## Quick Start

### 1. Echo Bot

The simplest possible bot — replies with whatever you send it:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"

    matrix "github.com/eslider/go-matrix-bot"
    "maunium.net/go/mautrix/event"
    "maunium.net/go/mautrix/id"
)

func main() {
    bot, _ := matrix.NewBot(matrix.GetEnvironmentConfig())

    bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
        bot.SendText(ctx, roomID, "Echo: "+msg.Body)
    })

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    go bot.Run(ctx)
    <-ctx.Done()
    bot.Stop()
}
```

### 2. AI Assistant (with Ollama)

A bot that forwards user messages to an Ollama LLM and returns formatted responses:

```go
package main

import (
    "context"
    "os"
    "os/signal"
    "strings"

    matrix "github.com/eslider/go-matrix-bot"
    ollama "github.com/eslider/go-ollama"
    "maunium.net/go/mautrix/event"
    "maunium.net/go/mautrix/id"
)

func main() {
    bot, _ := matrix.NewBot(matrix.GetEnvironmentConfig())

    ai := ollama.NewOpenWebUiClient(&ollama.DSN{
        URL:   os.Getenv("OPEN_WEB_API_GENERATE_URL"),
        Token: os.Getenv("OPEN_WEB_API_TOKEN"),
    })

    bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
        if !strings.HasPrefix(msg.Body, "::") {
            return
        }

        prompt := msg.Body[2:]
        var chunks []string

        ai.Query(ollama.Request{
            Model:  "llama3.2:3b",
            Prompt: prompt,
            Options: &ollama.RequestOptions{
                Temperature: ollama.Float(0.7),
            },
            OnJson: func(res ollama.Response) error {
                if res.Response != nil {
                    chunks = append(chunks, *res.Response)
                }
                return nil
            },
        })

        response := strings.Join(chunks, "")
        html := matrix.MarkdownToHTML(response)
        bot.SendReply(ctx, roomID, response, html, sender)
    })

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()
    go bot.Run(ctx)
    <-ctx.Done()
    bot.Stop()
}
```

### 3. Command Bot (Multi-Command)

A bot with multiple commands like `!help`, `!ping`, `!time`, `!ai`:

```go
bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
    switch {
    case msg.Body == "!ping":
        bot.SendText(ctx, roomID, "pong!")

    case msg.Body == "!time":
        bot.SendText(ctx, roomID, time.Now().Format(time.RFC3339))

    case strings.HasPrefix(msg.Body, "!ai "):
        prompt := strings.TrimPrefix(msg.Body, "!ai ")
        // ... query Ollama and send response
    }
})
```

See the full [commandbot example](examples/commandbot/main.go) for a complete implementation with help text and code extraction.

---

## Use Cases

### Chat-Ops / DevOps Notifications

```go
// Send deployment notifications to a Matrix room
bot.SendHTML(ctx, opsRoom,
    "Deploy complete: v1.2.3",
    matrix.MarkdownToHTML("**Deploy complete:** `v1.2.3` to production"),
)
```

### AI Code Review Assistant

```go
bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
    if !strings.HasPrefix(msg.Body, "!review ") {
        return
    }

    code := strings.TrimPrefix(msg.Body, "!review ")
    var chunks []string

    ai.Query(ollama.Request{
        Model:  "llama3.2:3b",
        Prompt: "Review this code for bugs, security issues, and improvements:\n\n" + code,
        Options: &ollama.RequestOptions{Temperature: ollama.Float(0.3)},
        OnJson: func(res ollama.Response) error {
            if res.Response != nil {
                chunks = append(chunks, *res.Response)
            }
            return nil
        },
    })

    review := strings.Join(chunks, "")
    bot.SendReply(ctx, roomID, review, matrix.MarkdownToHTML(review), sender)
})
```

### Monitoring Alert Bot

```go
// Run a periodic health check and report to Matrix
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        resp, err := http.Get("https://api.example.com/health")
        if err != nil || resp.StatusCode != 200 {
            bot.SendHTML(ctx, alertRoom,
                "ALERT: API is down!",
                matrix.MarkdownToHTML("**ALERT:** API health check failed"),
            )
        }
    }
}()
```

### AI Translation Bot

```go
bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
    if !strings.HasPrefix(msg.Body, "!translate ") {
        return
    }

    text := strings.TrimPrefix(msg.Body, "!translate ")
    var chunks []string

    ai.Query(ollama.Request{
        Model:  "llama3.2:3b",
        Prompt: "Translate the following text to English. Only output the translation:\n\n" + text,
        Options: &ollama.RequestOptions{Temperature: ollama.Float(0.1)},
        OnJson: func(res ollama.Response) error {
            if res.Response != nil {
                chunks = append(chunks, *res.Response)
            }
            return nil
        },
    })

    translation := strings.Join(chunks, "")
    bot.SendReply(ctx, roomID, translation, matrix.MarkdownToHTML(translation), sender)
})
```

---

## API Reference

### Types

#### `Config`

```go
type Config struct {
    Homeserver string // Matrix homeserver URL (e.g. "https://matrix.org")
    Username   string // Bot username (localpart, e.g. "mybot")
    Password   string // Bot password
    Database   string // SQLite database path for crypto state (default: "matrix-bot.db")
    Debug      bool   // Enable debug logging
}
```

#### `MessageHandler`

```go
type MessageHandler func(ctx context.Context, roomID id.RoomID, sender id.UserID, message *event.MessageEventContent)
```

### Functions

| Function | Description |
|---|---|
| `NewBot(config Config)` | Create a new bot instance |
| `GetEnvironmentConfig()` | Load config from `MATRIX_API_*` env vars |
| `MarkdownToHTML(md string)` | Convert markdown to HTML for rich messages |

### Bot Methods

| Method | Description |
|---|---|
| `OnMessage(handler)` | Register a message handler (can register multiple) |
| `SendText(ctx, roomID, text)` | Send a plain text message |
| `SendHTML(ctx, roomID, text, html)` | Send a message with HTML formatting |
| `SendReply(ctx, roomID, text, html, ...userIDs)` | Send a formatted reply with user mentions |
| `Client()` | Access the underlying mautrix client for advanced usage |
| `Run(ctx)` | Start the bot (blocks until context is cancelled) |
| `Stop()` | Gracefully stop the bot and close the database |

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `MATRIX_API_URL` | Yes | Matrix homeserver URL |
| `MATRIX_API_USER` | Yes | Bot username (localpart) |
| `MATRIX_API_PASS` | Yes | Bot password |
| `MATRIX_DEBUG` | No | Set `true` for verbose logging |
| `OPEN_WEB_API_GENERATE_URL` | No | Ollama API URL (for AI examples) |
| `OPEN_WEB_API_TOKEN` | No | Ollama API token (for AI examples) |

## Examples

| Example | Description | Run |
|---|---|---|
| [echobot](examples/echobot/) | Simple echo bot | `go run ./examples/echobot/` |
| [ai-assistant](examples/ai-assistant/) | AI chat with Ollama | `go run ./examples/ai-assistant/` |
| [commandbot](examples/commandbot/) | Multi-command bot with `!help`, `!ai`, `!code` | `go run ./examples/commandbot/` |

## Related

- [go-ollama](https://github.com/eSlider/go-ollama) — Ollama/Open WebUI API client with streaming
- [go-onlyoffice](https://github.com/eSlider/go-onlyoffice) — OnlyOffice Project Management API client
- [go-gitea-helpers](https://github.com/eSlider/go-gitea-helpers) — Gitea API pagination helpers

## License

[MIT](LICENSE)
