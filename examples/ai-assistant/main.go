// AI Assistant bot that uses Ollama to generate responses in Matrix rooms.
//
// The bot listens for messages starting with "::" and forwards the prompt
// to an Ollama/Open WebUI instance. The AI response is rendered as markdown
// and sent back to the room with user mentions.
//
// Set environment variables before running:
//
//	export MATRIX_API_URL="https://matrix.example.com"
//	export MATRIX_API_USER="botuser"
//	export MATRIX_API_PASS="botpassword"
//	export OPEN_WEB_API_GENERATE_URL="http://localhost:11434/api/generate"
//	export OPEN_WEB_API_TOKEN="your-ollama-token"
//	go run ./examples/ai-assistant/
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	matrix "github.com/eslider/go-matrix-bot"
	ollama "github.com/eslider/go-ollama"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const (
	// commandPrefix is the trigger prefix for AI queries.
	// Users type "::what is Go?" to get an AI response.
	commandPrefix = "::"

	// model is the Ollama model to use for generation.
	model = "llama3.2:3b"
)

func main() {
	// --- Matrix bot setup ---
	botConfig := matrix.GetEnvironmentConfig()
	botConfig.Debug = true

	if botConfig.Homeserver == "" {
		fmt.Fprintln(os.Stderr, "MATRIX_API_URL is not set")
		os.Exit(1)
	}

	bot, err := matrix.NewBot(botConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create bot: %v\n", err)
		os.Exit(1)
	}

	// --- Ollama AI client setup ---
	aiURL := os.Getenv("OPEN_WEB_API_GENERATE_URL")
	aiToken := os.Getenv("OPEN_WEB_API_TOKEN")

	if aiURL == "" {
		fmt.Fprintln(os.Stderr, "OPEN_WEB_API_GENERATE_URL is not set")
		os.Exit(1)
	}

	ai := ollama.NewOpenWebUiClient(&ollama.DSN{
		URL:   aiURL,
		Token: aiToken,
	})

	// --- Message handler: forward "::" messages to Ollama ---
	bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
		// Ignore messages that don't start with the command prefix
		if len(msg.Body) <= len(commandPrefix) || msg.Body[:len(commandPrefix)] != commandPrefix {
			return
		}

		prompt := strings.TrimSpace(msg.Body[len(commandPrefix):])
		if prompt == "" {
			return
		}

		fmt.Printf("[%s] %s asked: %s\n", roomID, sender, prompt)

		// Collect streaming response from Ollama
		var chunks []string
		queryErr := ai.Query(ollama.Request{
			Model:  model,
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

		if queryErr != nil {
			fmt.Fprintf(os.Stderr, "Ollama error: %v\n", queryErr)
			_ = bot.SendText(ctx, roomID, "Sorry, I encountered an error generating a response.")
			return
		}

		// Join all chunks and convert markdown to HTML
		response := strings.Join(chunks, "")
		html := matrix.MarkdownToHTML(response)

		// Send formatted reply with user mention
		if sendErr := bot.SendReply(ctx, roomID, response, html, sender); sendErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to send reply: %v\n", sendErr)
		}
	})

	// --- Start with graceful shutdown ---
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Printf("AI Assistant bot starting (model: %s)...\n", model)
	fmt.Println("Users can ask questions with: ::your question here")
	fmt.Println("Press Ctrl+C to stop.")

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
