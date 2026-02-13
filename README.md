# go-matrix-bot

Go library for building [Matrix](https://matrix.org/) bots with end-to-end encryption support, built on top of [mautrix-go](https://github.com/mautrix/go).

## Features

- Simple bot API with message handlers
- End-to-end encryption via mautrix crypto helper
- Auto-join rooms on invite
- Send plain text, HTML, and markdown-formatted messages
- Mention users in replies
- Environment variable configuration

## Installation

```bash
go get github.com/eslider/go-matrix-bot
```

**System dependency:** requires `libolm-dev` for encryption support:

```bash
# Debian/Ubuntu
sudo apt-get install libolm-dev
```

## Usage

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
    bot, err := matrix.NewBot(matrix.GetEnvironmentConfig())
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    // Register a message handler
    bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
        fmt.Printf("[%s] %s: %s\n", roomID, sender, msg.Body)

        // Echo bot: reply with the same message
        if err := bot.SendText(ctx, roomID, "Echo: "+msg.Body); err != nil {
            fmt.Fprintf(os.Stderr, "Failed to send: %v\n", err)
        }
    })

    // Run with graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    go func() {
        if err := bot.Run(ctx); err != nil {
            fmt.Fprintf(os.Stderr, "Bot error: %v\n", err)
        }
    }()

    <-ctx.Done()
    bot.Stop()
}
```

### Markdown Responses

```go
bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
    md := "**Hello!** Here is a list:\n- Item 1\n- Item 2"
    html := matrix.MarkdownToHTML(md)
    bot.SendReply(ctx, roomID, md, html, sender)
})
```

## Environment Variables

| Variable | Description |
|---|---|
| `MATRIX_API_URL` | Matrix homeserver URL (e.g. `https://matrix.org`) |
| `MATRIX_API_USER` | Bot username (localpart) |
| `MATRIX_API_PASS` | Bot password |
| `MATRIX_DEBUG` | Set to `true` for debug logging |

## License

[MIT](LICENSE)
