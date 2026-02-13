// Package matrix provides a reusable Matrix bot client built on top of mautrix.
// It supports encrypted rooms, message handling, and formatted (HTML) responses.
//
// Environment variables:
//   - MATRIX_API_URL: Matrix homeserver URL
//   - MATRIX_API_USER: Matrix username (localpart)
//   - MATRIX_API_PASS: Matrix password
package matrix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/exzerolog"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Config holds the configuration for the Matrix bot.
// All fields can be populated from environment variables using GetEnvironmentConfig().
type Config struct {
	Homeserver string // Matrix homeserver URL (e.g. https://matrix.org)
	Username   string // Username localpart (e.g. "mybot")
	Password   string // Password for authentication
	Database   string // SQLite database path for crypto state (default: "matrix-bot.db")
	Debug      bool   // Enable debug logging
}

// GetEnvironmentConfig creates a Config from environment variables.
func GetEnvironmentConfig() Config {
	return Config{
		Homeserver: os.Getenv("MATRIX_API_URL"),
		Username:   os.Getenv("MATRIX_API_USER"),
		Password:   os.Getenv("MATRIX_API_PASS"),
		Database:   "matrix-bot.db",
		Debug:      os.Getenv("MATRIX_DEBUG") == "true",
	}
}

// Validate checks that required fields are set.
func (c Config) Validate() error {
	if c.Homeserver == "" {
		return fmt.Errorf("matrix: homeserver URL is required")
	}
	if c.Username == "" {
		return fmt.Errorf("matrix: username is required")
	}
	if c.Password == "" {
		return fmt.Errorf("matrix: password is required")
	}
	return nil
}

// MessageHandler is called when the bot receives a message.
// The handler receives the context, the room ID, the sender, and the message event.
type MessageHandler func(ctx context.Context, roomID id.RoomID, sender id.UserID, message *event.MessageEventContent)

// Bot is a Matrix bot that can join rooms, receive messages, and send responses.
type Bot struct {
	config   Config
	client   *mautrix.Client
	crypto   *cryptohelper.CryptoHelper
	log      zerolog.Logger
	handlers []MessageHandler

	cancelSync func()
	syncWait   sync.WaitGroup
}

// NewBot creates a new Matrix bot with the given configuration.
// Call Run() to start the bot.
func NewBot(config Config) (*Bot, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	if config.Database == "" {
		config.Database = "matrix-bot.db"
	}

	return &Bot{
		config: config,
	}, nil
}

// OnMessage registers a handler for incoming messages.
// Multiple handlers can be registered and all will be called.
func (b *Bot) OnMessage(handler MessageHandler) {
	b.handlers = append(b.handlers, handler)
}

// SendText sends a plain text message to the given room.
func (b *Bot) SendText(ctx context.Context, roomID id.RoomID, text string) error {
	_, err := b.client.SendText(ctx, roomID, text)
	return err
}

// SendHTML sends a formatted message with both plain text and HTML body.
func (b *Bot) SendHTML(ctx context.Context, roomID id.RoomID, text string, html string) error {
	_, err := b.client.SendMessageEvent(ctx, roomID, event.EventMessage, &event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          text,
		Format:        event.FormatHTML,
		FormattedBody: html,
	})
	return err
}

// SendReply sends a formatted message that mentions specific users.
func (b *Bot) SendReply(ctx context.Context, roomID id.RoomID, text string, html string, mentionUserIDs ...id.UserID) error {
	content := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          text,
		Format:        event.FormatHTML,
		FormattedBody: html,
	}

	if len(mentionUserIDs) > 0 {
		content.Mentions = &event.Mentions{
			UserIDs: mentionUserIDs,
			Room:    true,
		}
	}

	_, err := b.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	return err
}

// Client returns the underlying mautrix client for advanced usage.
func (b *Bot) Client() *mautrix.Client {
	return b.client
}

// Run starts the bot: connects to the homeserver, sets up encryption,
// and begins syncing. This blocks until Stop() is called or an error occurs.
func (b *Bot) Run(ctx context.Context) error {
	client, err := mautrix.NewClient(b.config.Homeserver, "", "")
	if err != nil {
		return fmt.Errorf("matrix: failed to create client: %w", err)
	}
	b.client = client

	// Set up logging
	log := zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stderr
		w.TimeFormat = time.Stamp
	})).With().Timestamp().Logger()

	if !b.config.Debug {
		log = log.Level(zerolog.InfoLevel)
	}
	exzerolog.SetupDefaults(&log)
	b.log = log
	b.client.Log = log

	// Register event handlers
	syncer := b.client.Syncer.(*mautrix.DefaultSyncer)

	// Handle incoming messages
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		msg := evt.Content.AsMessage()
		for _, handler := range b.handlers {
			handler(ctx, evt.RoomID, evt.Sender, msg)
		}
	})

	// Auto-join rooms on invite
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		if evt.GetStateKey() == b.client.UserID.String() && evt.Content.AsMember().Membership == event.MembershipInvite {
			_, joinErr := b.client.JoinRoomByID(ctx, evt.RoomID)
			if joinErr != nil {
				b.log.Error().Err(joinErr).
					Str("room_id", evt.RoomID.String()).
					Str("inviter", evt.Sender.String()).
					Msg("Failed to join room after invite")
			} else {
				b.log.Info().
					Str("room_id", evt.RoomID.String()).
					Str("inviter", evt.Sender.String()).
					Msg("Joined room after invite")
			}
		}
	})

	// Set up encryption
	cryptoHelper, err := cryptohelper.NewCryptoHelper(b.client, []byte("meow"), b.config.Database)
	if err != nil {
		return fmt.Errorf("matrix: failed to create crypto helper: %w", err)
	}

	cryptoHelper.LoginAs = &mautrix.ReqLogin{
		Type:             mautrix.AuthTypePassword,
		Identifier:       mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: b.config.Username},
		Password:         b.config.Password,
		StoreCredentials: true,
	}

	if err = cryptoHelper.Init(ctx); err != nil {
		return fmt.Errorf("matrix: failed to init crypto: %w", err)
	}
	b.crypto = cryptoHelper
	b.client.Crypto = cryptoHelper

	b.log.Info().Str("user", b.config.Username).Msg("Matrix bot is running")

	// Start syncing
	syncCtx, cancelSync := context.WithCancel(ctx)
	b.cancelSync = cancelSync
	b.syncWait.Add(1)

	go func() {
		defer b.syncWait.Done()
		if syncErr := b.client.SyncWithContext(syncCtx); syncErr != nil && !errors.Is(syncErr, context.Canceled) {
			b.log.Error().Err(syncErr).Msg("Sync error")
		}
	}()

	// Wait for context cancellation
	<-syncCtx.Done()
	return nil
}

// Stop gracefully stops the bot.
func (b *Bot) Stop() error {
	if b.cancelSync != nil {
		b.cancelSync()
	}
	b.syncWait.Wait()

	if b.crypto != nil {
		return b.crypto.Close()
	}
	return nil
}
