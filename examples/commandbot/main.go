// Command bot that demonstrates a multi-command handler pattern.
//
// Supported commands:
//
//	!help       - Show available commands
//	!ping       - Respond with "pong" (latency check)
//	!time       - Show current server time
//	!ai <prompt> - Generate AI response using Ollama (if configured)
//	!code <prompt> - Generate code and extract code blocks
//
// Set environment variables before running:
//
//	export MATRIX_API_URL="https://matrix.example.com"
//	export MATRIX_API_USER="botuser"
//	export MATRIX_API_PASS="botpassword"
//	export OPEN_WEB_API_GENERATE_URL="http://localhost:11434/api/generate"  # optional
//	export OPEN_WEB_API_TOKEN="your-ollama-token"                           # optional
//	go run ./examples/commandbot/
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	matrix "github.com/eslider/go-matrix-bot"
	ollama "github.com/eslider/go-ollama"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// command represents a bot command with its handler.
type command struct {
	Name        string
	Description string
	Usage       string
	Handler     func(ctx context.Context, bot *matrix.Bot, roomID id.RoomID, sender id.UserID, args string)
}

func main() {
	// --- Matrix bot ---
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

	// --- Ollama AI client (optional) ---
	var ai *ollama.Client
	if aiURL := os.Getenv("OPEN_WEB_API_GENERATE_URL"); aiURL != "" {
		ai = ollama.NewOpenWebUiClient(&ollama.DSN{
			URL:   aiURL,
			Token: os.Getenv("OPEN_WEB_API_TOKEN"),
		})
		fmt.Println("Ollama AI enabled")
	} else {
		fmt.Println("Ollama AI disabled (OPEN_WEB_API_GENERATE_URL not set)")
	}

	// --- Define commands ---
	commands := buildCommands(bot, ai)

	// --- Register message handler ---
	bot.OnMessage(func(ctx context.Context, roomID id.RoomID, sender id.UserID, msg *event.MessageEventContent) {
		body := strings.TrimSpace(msg.Body)
		if !strings.HasPrefix(body, "!") {
			return
		}

		// Parse command name and arguments
		parts := strings.SplitN(body, " ", 2)
		cmdName := strings.ToLower(parts[0])
		args := ""
		if len(parts) > 1 {
			args = strings.TrimSpace(parts[1])
		}

		fmt.Printf("[%s] %s: %s\n", roomID, sender, body)

		// Find and execute matching command
		for _, cmd := range commands {
			if cmd.Name == cmdName {
				cmd.Handler(ctx, bot, roomID, sender, args)
				return
			}
		}

		// Unknown command
		_ = bot.SendText(ctx, roomID, fmt.Sprintf("Unknown command: %s. Type !help for available commands.", cmdName))
	})

	// --- Start ---
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("Command bot starting... Type !help in a room to see commands.")
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

// buildCommands returns all available bot commands.
func buildCommands(bot *matrix.Bot, ai *ollama.Client) []command {
	commands := []command{
		{
			Name:        "!ping",
			Description: "Check if the bot is alive",
			Usage:       "!ping",
			Handler: func(ctx context.Context, bot *matrix.Bot, roomID id.RoomID, _ id.UserID, _ string) {
				_ = bot.SendText(ctx, roomID, "pong!")
			},
		},
		{
			Name:        "!time",
			Description: "Show current server time",
			Usage:       "!time",
			Handler: func(ctx context.Context, bot *matrix.Bot, roomID id.RoomID, _ id.UserID, _ string) {
				now := time.Now().Format("2006-01-02 15:04:05 MST")
				md := fmt.Sprintf("Server time: **%s**", now)
				_ = bot.SendHTML(ctx, roomID, now, matrix.MarkdownToHTML(md))
			},
		},
	}

	// AI-powered commands (only available when Ollama is configured)
	if ai != nil {
		commands = append(commands,
			command{
				Name:        "!ai",
				Description: "Ask the AI a question",
				Usage:       "!ai <your question>",
				Handler:     makeAIHandler(ai, "llama3.2:3b", false),
			},
			command{
				Name:        "!code",
				Description: "Generate code with the AI and extract code blocks",
				Usage:       "!code <describe what you need>",
				Handler:     makeAIHandler(ai, "llama3.2:3b", true),
			},
		)
	}

	// Help command (needs access to the full commands list)
	helpCmd := command{
		Name:        "!help",
		Description: "Show available commands",
		Usage:       "!help",
	}
	// We'll set the handler after appending, so it captures the final list
	allCommands := append([]command{helpCmd}, commands...)

	allCommands[0].Handler = func(ctx context.Context, bot *matrix.Bot, roomID id.RoomID, sender id.UserID, _ string) {
		var sb strings.Builder
		sb.WriteString("**Available commands:**\n\n")
		for _, cmd := range allCommands {
			sb.WriteString(fmt.Sprintf("- `%s` — %s\n", cmd.Usage, cmd.Description))
		}
		md := sb.String()
		_ = bot.SendReply(ctx, roomID, md, matrix.MarkdownToHTML(md), sender)
	}

	return allCommands
}

// makeAIHandler creates a message handler that queries Ollama.
// If extractCode is true, it also extracts and displays code blocks.
func makeAIHandler(ai *ollama.Client, model string, extractCode bool) func(ctx context.Context, bot *matrix.Bot, roomID id.RoomID, sender id.UserID, args string) {
	return func(ctx context.Context, bot *matrix.Bot, roomID id.RoomID, sender id.UserID, args string) {
		if args == "" {
			_ = bot.SendText(ctx, roomID, "Please provide a prompt. Example: !ai What is Go?")
			return
		}

		// Collect streaming tokens
		var chunks []string
		var codeBlocks []*ollama.CodeBlock

		req := ollama.Request{
			Model:  model,
			Prompt: args,
			Options: &ollama.RequestOptions{
				Temperature: ollama.Float(0.7),
			},
			OnJson: func(res ollama.Response) error {
				if res.Response != nil {
					chunks = append(chunks, *res.Response)
				}
				return nil
			},
		}

		if extractCode {
			req.Options.Temperature = ollama.Float(0) // deterministic for code
			req.OnCodeBlock = func(blocks []*ollama.CodeBlock) error {
				codeBlocks = append(codeBlocks, blocks...)
				return nil
			}
		}

		if queryErr := ai.Query(req); queryErr != nil {
			fmt.Fprintf(os.Stderr, "Ollama error: %v\n", queryErr)
			_ = bot.SendText(ctx, roomID, "Sorry, AI query failed: "+queryErr.Error())
			return
		}

		response := strings.Join(chunks, "")

		// Append extracted code block summary
		if extractCode && len(codeBlocks) > 0 {
			response += fmt.Sprintf("\n\n---\n*Extracted %d code block(s)*", len(codeBlocks))
		}

		html := matrix.MarkdownToHTML(response)
		_ = bot.SendReply(ctx, roomID, response, html, sender)
	}
}
