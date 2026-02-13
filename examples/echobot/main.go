// Package main demonstrates a simple Matrix echo bot using the matrix library.
//
// Set environment variables before running:
//
//	export MATRIX_API_URL="https://matrix.example.com"
//	export MATRIX_API_USER="botuser"
//	export MATRIX_API_PASS="botpassword"
//	go run ./examples/echobot/
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	matrix "github.com/eslider/go-matrix-bot"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func main() {
	config := matrix.GetEnvironmentConfig()
	if config.Homeserver == "" {
		fmt.Fprintln(os.Stderr, "MATRIX_API_URL is not set")
		os.Exit(1)
	}

	bot, err := matrix.NewBot(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create bot: %v\n", err)
		os.Exit(1)
	}

	// Register message handler: respond to messages starting with "!echo"
	bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
		fmt.Printf("[%s] %s: %s\n", roomID, sender, msg.Body)

		if !strings.HasPrefix(msg.Body, "!echo ") {
			return
		}

		reply := strings.TrimPrefix(msg.Body, "!echo ")
		html := matrix.MarkdownToHTML(reply)

		if err := bot.SendReply(ctx, roomID, reply, html, sender); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to send reply: %v\n", err)
		}
	})

	// Start bot with graceful shutdown on SIGINT
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("Starting echo bot... Press Ctrl+C to stop.")

	go func() {
		if runErr := bot.Run(ctx); runErr != nil {
			fmt.Fprintf(os.Stderr, "Bot error: %v\n", runErr)
			cancel()
		}
	}()

	<-ctx.Done()
	fmt.Println("\nShutting down...")
	if stopErr := bot.Stop(); stopErr != nil {
		fmt.Fprintf(os.Stderr, "Error stopping bot: %v\n", stopErr)
	}
}
