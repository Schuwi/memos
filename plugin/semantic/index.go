package semantic

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/philippgille/chromem-go"
	"github.com/usememos/memos/store"
)

const (
	// DefaultEmbeddingModel is the default model for generating embeddings
	DefaultEmbeddingModel = "text-embedding-ada-002"
	// DefaultQueryInstruction is the default instruction for semantic search queries
	DefaultQueryInstruction = "Instruct: Given a query, look for notes that could be related in any way\nQuery: "
)

// Config holds the configuration for the semantic search service
type Config struct {
	// APIKey is the API key for the embedding service (e.g., OpenAI)
	APIKey string
	// BaseURL is the base URL for the embedding API
	BaseURL string
	// EmbeddingModel is the model to use for generating embeddings
	EmbeddingModel string
	// QueryInstruction is the instruction to prepend to the query for better semantic search results
	QueryInstruction string
}

// VectorIndex manages embeddings and provides semantic search capabilities.
type VectorIndex struct {
	db         *chromem.DB
	collection *chromem.Collection
	mu         sync.RWMutex
	client    *EmbeddingClient
}

// SearchResult represents a result from a semantic search
type SearchResult struct {
	// ID is the unique identifier of the memo
	ID int32
	// Score is the similarity score (higher is more similar)
	Score float32
	// Metadata is additional metadata about the document
	Metadata map[string]string
}

// NewVectorIndex creates a new semantic vector index.
func NewVectorIndex(client *EmbeddingClient) *VectorIndex {
	// Create a new chromem DB
	db := chromem.NewDB()

	// Create an embedding function using the client's configuration
	embeddingFunc := func(ctx context.Context, text string) ([]float32, error) {
		return client.GetEmbedding(ctx, text)
	}

	// Create a collection in the DB
	collection, err := db.CreateCollection(
		"memos",
		nil,
		embeddingFunc,
	)

	if err != nil {
		panic(fmt.Sprintf("failed to create collection: %v", err))
	}

	return &VectorIndex{
		db:         db,
		collection: collection,
		client:     client,
	}
}

// AddDocument adds a document to the index.
func (vi *VectorIndex) AddDocument(ctx context.Context, memo *store.Memo) error {
	if memo.Content == "" {
		return errors.New("document content cannot be empty")
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	metadata := make(map[string]string)
	metadata["creator_id"] = fmt.Sprintf("%d", memo.CreatorID)
	metadata["visibility"] = string(memo.Visibility)

	chromemDoc := chromem.Document{
		ID:       fmt.Sprintf("%d", memo.ID),
		Content:  memo.Content,
		Metadata: metadata,
	}

	// Add to chromem collection
	err := vi.collection.AddDocument(ctx, chromemDoc)

	return err
}

// AddDocuments adds multiple documents to the index.
func (vi *VectorIndex) AddDocuments(ctx context.Context, memos []*store.Memo) error {
	if len(memos) == 0 {
		return errors.New("no documents to add")
	}

	docs := make([]chromem.Document, 0, len(memos))
	for _, memo := range memos {
		if memo.Content == "" {
			return errors.New("document content cannot be empty")
		}

		metadata := make(map[string]string)
		metadata["creator_id"] = fmt.Sprintf("%d", memo.CreatorID)
		metadata["visibility"] = string(memo.Visibility)

		docs = append(docs, chromem.Document{
			ID:      fmt.Sprintf("%d", memo.ID),
			Content: memo.Content,
			Metadata: metadata,
		})
	}

	// Pre-calculate embeddings for all documents to avoid multiple API calls
	// This is a workaround for chromem's current limitation of not supporting batch embedding
	contents := make([]string, len(memos))
	for i, memo := range memos {
		contents[i] = memo.Content
	}
	embeddings, err := vi.client.GetBatchEmbeddings(ctx, contents)
	if err != nil {
		return fmt.Errorf("failed to get batch embeddings: %w", err)
	}
	for i, _ := range docs {
		docs[i].Embedding = embeddings[i]
	}

	err = vi.collection.AddDocuments(ctx, docs, 8)

	return err
}

// RemoveDocument removes a document from the index.
func (vi *VectorIndex) RemoveDocument(ctx context.Context, memoID int32) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	err := vi.collection.Delete(ctx, nil, nil, fmt.Sprintf("%d", memoID))

	return err
}

// Search performs a semantic search with the given query text.
func (vi *VectorIndex) Search(ctx context.Context, queryText string, limit int, userID int32) ([]SearchResult, error) {
	if queryText == "" {
		return nil, errors.New("empty query text")
	}

	// Use the configured query instruction
	queryInstruction := vi.client.config.QueryInstruction
	fullQuery := queryInstruction + queryText

	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if limit <= 0 {
		limit = 10 // Default limit
	}

	// For now can only search for own documents because
	// chromem doesn't (yet) support OR filters (e.g., creator_id == userID || visibility == PUBLIC)
	metadataFilter := make(map[string]string)
	metadataFilter["creator_id"] = fmt.Sprintf("%d", userID)

	// Use chromem Query method which will generate the embedding and perform the search
	results, err := vi.collection.Query(ctx, fullQuery, limit, metadataFilter, nil)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert to SearchResults
	searchResults := make([]SearchResult, 0, len(results))
	for _, result := range results {
		// Convert ID back to int32
		var docID int32
		fmt.Sscanf(result.ID, "%d", &docID)

		searchResults = append(searchResults, SearchResult{
			ID:       docID,
			Score:    result.Similarity,
			Metadata: result.Metadata,
		})
	}

	// Sort results by score (descending) - this should already be done by chromem
	// sort.Slice(searchResults, func(i, j int) bool {
	// 	return searchResults[i].Score > searchResults[j].Score
	// })

	return searchResults, nil
}

// SearchByVector performs a semantic search with the given embedding vector.
func (vi *VectorIndex) SearchByVector(ctx context.Context, queryVector []float32, limit int) ([]SearchResult, error) {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Use chromem QueryEmbedding method to search by vector
	results, err := vi.collection.QueryEmbedding(ctx, queryVector, limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Convert to SearchResults
	searchResults := make([]SearchResult, 0, len(results))
	for _, result := range results {
		// Convert ID back to int32
		var docID int32
		fmt.Sscanf(result.ID, "%d", &docID)

		searchResults = append(searchResults, SearchResult{
			ID:       docID,
			Score:    result.Similarity,
			Metadata: result.Metadata,
		})
	}

	// Sort results by score (descending)
	// sort.Slice(searchResults, func(i, j int) bool {
	// 	return searchResults[i].Score > searchResults[j].Score
	// })

	return searchResults, nil
}
