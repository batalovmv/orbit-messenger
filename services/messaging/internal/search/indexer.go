package search

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// DocumentIndexer is a minimal interface for indexing and deleting documents.
// Defined here to avoid a circular import with the service package.
type DocumentIndexer interface {
	IndexDocuments(index string, docs []interface{}) error
	DeleteDocument(index string, id string) error
}

// Indexer subscribes to NATS message events and keeps Meilisearch in sync.
type Indexer struct {
	client DocumentIndexer
	nc     *nats.Conn
	subs   []*nats.Subscription
	logger *slog.Logger
}

// NewIndexer creates a new Indexer. client is typically a *MeilisearchClient.
func NewIndexer(client *MeilisearchClient, nc *nats.Conn, logger *slog.Logger) *Indexer {
	return &Indexer{
		client: client,
		nc:     nc,
		logger: logger,
	}
}

// Start subscribes to NATS subjects for message lifecycle events.
// It returns the first subscription error, if any.
func (idx *Indexer) Start() error {
	handlers := []struct {
		subject string
		handler nats.MsgHandler
	}{
		{"orbit.chat.*.message.new", idx.handleNewMessage},
		{"orbit.chat.*.message.updated", idx.handleUpdatedMessage},
		{"orbit.chat.*.message.deleted", idx.handleDeletedMessage},
	}

	for _, h := range handlers {
		sub, err := idx.nc.Subscribe(h.subject, h.handler)
		if err != nil {
			// Unsubscribe any already-registered subscriptions before returning.
			idx.Stop()
			return err
		}
		idx.subs = append(idx.subs, sub)
	}

	idx.logger.Info("search indexer started",
		"subjects", []string{
			"orbit.chat.*.message.new",
			"orbit.chat.*.message.updated",
			"orbit.chat.*.message.deleted",
		},
	)
	return nil
}

// Stop drains and unsubscribes all NATS subscriptions.
func (idx *Indexer) Stop() {
	for _, sub := range idx.subs {
		if err := sub.Unsubscribe(); err != nil {
			idx.logger.Warn("search indexer: unsubscribe error", "error", err)
		}
	}
	idx.subs = nil
	idx.logger.Info("search indexer stopped")
}

// natsEvent is the envelope used by all Orbit NATS events.
type natsEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// messageData is the subset of message fields needed for indexing.
type messageData struct {
	ID             string    `json:"id"`
	ChatID         string    `json:"chat_id"`
	SenderID       *string   `json:"sender_id"`
	Content        *string   `json:"content"`
	Type           string    `json:"type"`
	SequenceNumber int64     `json:"sequence_number"`
	CreatedAt      time.Time `json:"created_at"`
	// MediaAttachments presence is inferred from has_media flag in the event data,
	// or by checking if the slice is non-nil/non-empty after unmarshalling.
	MediaAttachments []json.RawMessage `json:"media_attachments"`
}

func (idx *Indexer) handleNewMessage(msg *nats.Msg) {
	idx.indexMessageEvent(msg)
}

func (idx *Indexer) handleUpdatedMessage(msg *nats.Msg) {
	idx.indexMessageEvent(msg)
}

// indexMessageEvent decodes the NATS event, builds a flat document, and upserts it.
func (idx *Indexer) indexMessageEvent(msg *nats.Msg) {
	var ev natsEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		idx.logger.Error("search indexer: failed to unmarshal nats event", "error", err, "subject", msg.Subject)
		return
	}

	var m messageData
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		idx.logger.Error("search indexer: failed to unmarshal message data", "error", err, "event", ev.Event)
		return
	}

	senderID := ""
	if m.SenderID != nil {
		senderID = *m.SenderID
	}
	content := ""
	if m.Content != nil {
		content = *m.Content
	}

	doc := map[string]interface{}{
		"id":              m.ID,
		"chat_id":         m.ChatID,
		"sender_id":       senderID,
		"content":         content,
		"type":            m.Type,
		"has_media":       len(m.MediaAttachments) > 0,
		"created_at_ts":   m.CreatedAt.Unix(),
		"sequence_number": m.SequenceNumber,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = ctx // DocumentIndexer does not accept context; timeout is advisory here.

	if err := idx.client.IndexDocuments("messages", []interface{}{doc}); err != nil {
		idx.logger.Error("search indexer: failed to index message",
			"error", err,
			"message_id", m.ID,
			"event", ev.Event,
		)
	}
}

// deletedPayload covers both a bare message object and a thin payload
// that carries only the message ID.
type deletedPayload struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"` // alternative field name used by some events
}

func (idx *Indexer) handleDeletedMessage(msg *nats.Msg) {
	var ev natsEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		idx.logger.Error("search indexer: failed to unmarshal delete event", "error", err, "subject", msg.Subject)
		return
	}

	var payload deletedPayload
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		idx.logger.Error("search indexer: failed to unmarshal deleted message payload", "error", err)
		return
	}

	// Prefer "id", fall back to "message_id".
	messageID := payload.ID
	if messageID == "" {
		messageID = payload.MessageID
	}
	if messageID == "" {
		idx.logger.Error("search indexer: delete event missing message id", "event", ev.Event)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = ctx // DocumentIndexer does not accept context; timeout is advisory here.

	if err := idx.client.DeleteDocument("messages", messageID); err != nil {
		idx.logger.Error("search indexer: failed to delete message from index",
			"error", err,
			"message_id", messageID,
		)
	}
}
