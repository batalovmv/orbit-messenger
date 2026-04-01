package search

import (
	"encoding/json"
	"fmt"

	"github.com/meilisearch/meilisearch-go"
)

// SearchOptions configures a Meilisearch query.
type SearchOptions struct {
	Filter string
	Limit  int
	Offset int
	Sort   []string
}

// SearchResponse is the result returned by SearchClient.Search.
type SearchResponse struct {
	Hits               []map[string]interface{} `json:"hits"`
	EstimatedTotalHits int                      `json:"estimatedTotalHits"`
}

// SearchClient is the interface satisfied by a Meilisearch client wrapper.
type SearchClient interface {
	Search(index string, query string, opts *SearchOptions) (*SearchResponse, error)
	IndexDocuments(index string, docs []interface{}) error
	DeleteDocument(index string, id string) error
}

// MeilisearchClient wraps the official Meilisearch Go client.
type MeilisearchClient struct {
	client meilisearch.ServiceManager
}

// NewMeilisearchClient creates a new MeilisearchClient and configures index settings.
func NewMeilisearchClient(url, apiKey string) (*MeilisearchClient, error) {
	client := meilisearch.New(url, meilisearch.WithAPIKey(apiKey))

	if err := configureIndexes(client); err != nil {
		return nil, fmt.Errorf("configure meilisearch indexes: %w", err)
	}

	return &MeilisearchClient{client: client}, nil
}

// configureIndexes sets up filterable, sortable, and searchable attributes for each index.
func configureIndexes(client meilisearch.ServiceManager) error {
	type indexConfig struct {
		name       string
		searchable []string
		filterable []interface{} // UpdateFilterableAttributes requires *[]interface{}
		sortable   []string
	}

	configs := []indexConfig{
		{
			name:       "messages",
			searchable: []string{"content"},
			filterable: []interface{}{"chat_id", "sender_id", "type", "has_media", "created_at_ts"},
			sortable:   []string{"created_at_ts", "sequence_number"},
		},
		{
			name:       "users",
			searchable: []string{"display_name", "email"},
			filterable: []interface{}{"role"},
		},
		{
			name:       "chats",
			searchable: []string{"name", "description"},
			filterable: []interface{}{"type"},
		},
	}

	for _, cfg := range configs {
		idx := client.Index(cfg.name)

		if len(cfg.searchable) > 0 {
			task, err := idx.UpdateSearchableAttributes(&cfg.searchable)
			if err != nil {
				return fmt.Errorf("update searchable attributes for %s: %w", cfg.name, err)
			}
			if _, err := client.WaitForTask(task.TaskUID, 0); err != nil {
				return fmt.Errorf("wait for searchable attributes task for %s: %w", cfg.name, err)
			}
		}

		if len(cfg.filterable) > 0 {
			task, err := idx.UpdateFilterableAttributes(&cfg.filterable)
			if err != nil {
				return fmt.Errorf("update filterable attributes for %s: %w", cfg.name, err)
			}
			if _, err := client.WaitForTask(task.TaskUID, 0); err != nil {
				return fmt.Errorf("wait for filterable attributes task for %s: %w", cfg.name, err)
			}
		}

		if len(cfg.sortable) > 0 {
			task, err := idx.UpdateSortableAttributes(&cfg.sortable)
			if err != nil {
				return fmt.Errorf("update sortable attributes for %s: %w", cfg.name, err)
			}
			if _, err := client.WaitForTask(task.TaskUID, 0); err != nil {
				return fmt.Errorf("wait for sortable attributes task for %s: %w", cfg.name, err)
			}
		}
	}

	return nil
}

// Search executes a full-text search query against the given index.
func (c *MeilisearchClient) Search(index string, query string, opts *SearchOptions) (*SearchResponse, error) {
	req := &meilisearch.SearchRequest{}
	if opts != nil {
		req.Filter = opts.Filter
		req.Limit = int64(opts.Limit)
		req.Offset = int64(opts.Offset)
		req.Sort = opts.Sort
	}

	result, err := c.client.Index(index).Search(query, req)
	if err != nil {
		return nil, fmt.Errorf("meilisearch search on index %s: %w", index, err)
	}

	// meilisearch.Hit is map[string]json.RawMessage — convert to map[string]interface{}.
	hits := make([]map[string]interface{}, 0, len(result.Hits))
	for _, h := range result.Hits {
		m := make(map[string]interface{}, len(h))
		for k, raw := range h {
			var v interface{}
			if err := json.Unmarshal(raw, &v); err != nil {
				m[k] = string(raw)
			} else {
				m[k] = v
			}
		}
		hits = append(hits, m)
	}

	return &SearchResponse{
		Hits:               hits,
		EstimatedTotalHits: int(result.EstimatedTotalHits),
	}, nil
}

// IndexDocuments adds or updates documents in the given index, using "id" as the primary key.
func (c *MeilisearchClient) IndexDocuments(index string, docs []interface{}) error {
	pk := "id"
	opts := &meilisearch.DocumentOptions{PrimaryKey: &pk}
	_, err := c.client.Index(index).AddDocuments(docs, opts)
	if err != nil {
		return fmt.Errorf("meilisearch index documents on index %s: %w", index, err)
	}
	return nil
}

// DeleteDocument removes a single document by ID from the given index.
func (c *MeilisearchClient) DeleteDocument(index string, id string) error {
	_, err := c.client.Index(index).DeleteDocument(id, nil)
	if err != nil {
		return fmt.Errorf("meilisearch delete document %s from index %s: %w", id, index, err)
	}
	return nil
}

// noopSearchClient is a no-op SearchClient used in tests.
type noopSearchClient struct{}

// NewNoopSearchClient returns a SearchClient that silently discards all operations.
func NewNoopSearchClient() SearchClient {
	return &noopSearchClient{}
}

func (n *noopSearchClient) Search(_ string, _ string, _ *SearchOptions) (*SearchResponse, error) {
	return &SearchResponse{Hits: []map[string]interface{}{}, EstimatedTotalHits: 0}, nil
}

func (n *noopSearchClient) IndexDocuments(_ string, _ []interface{}) error {
	return nil
}

func (n *noopSearchClient) DeleteDocument(_ string, _ string) error {
	return nil
}
