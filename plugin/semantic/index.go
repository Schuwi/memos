package semantic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/coder/hnsw"
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

// Document represents a document to be indexed for semantic search.
type Document struct {
	ID       int32                  // Unique identifier
	Content  string                 // Text content for embedding
	Metadata map[string]interface{} // Additional metadata
	Vector   []float32              // Embedding vector
}

// Helper function to create a new document from a memo
func CreateDocumentFromMemo(memo *store.Memo) *Document {
	metadata := map[string]any{
		"hasLink":            memo.Payload.Property.HasLink,
		"hasTaskList":        memo.Payload.Property.HasTaskList,
		"hasCode":            memo.Payload.Property.HasCode,
		"hasIncompleteTasks": memo.Payload.Property.HasIncompleteTasks,
		"tags":               memo.Payload.Tags,
		"memo_uid":           memo.UID,
	}

	return &Document{
		ID:       memo.ID,
		Content:  memo.Content,
		Metadata: metadata,
	}
}

// VectorIndex manages embeddings and provides semantic search capabilities.
type VectorIndex struct {
	client     *EmbeddingClient
	index      *hnsw.Graph[int]
	documents  map[int32]*Document
	idToNodeID map[int32]int
	nodeIDToID map[int]int32
	mu         sync.RWMutex
	nextNodeID int
}

// SearchResult represents a result from a semantic search
type SearchResult struct {
	// ID is the unique identifier of the document
	ID int32
	// Score is the similarity score (higher is more similar)
	Score float32
	// Metadata is additional metadata about the document
	Metadata map[string]interface{}
}

// NewVectorIndex creates a new semantic vector index.
func NewVectorIndex(client *EmbeddingClient) *VectorIndex {
	index := hnsw.NewGraph[int]()

	return &VectorIndex{
		client:     client,
		index:      index,
		documents:  make(map[int32]*Document),
		idToNodeID: make(map[int32]int),
		nodeIDToID: make(map[int]int32),
		nextNodeID: 0,
	}
}

// AddDocument adds a document to the index.
func (vi *VectorIndex) AddDocument(ctx context.Context, doc *Document) error {
	if doc.Content == "" {
		return errors.New("document content cannot be empty")
	}

	// Get embedding for the document
	embedding, err := vi.client.GetEmbedding(ctx, doc.Content)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	// Store the embedding in the document
	doc.Vector = embedding

	// Check if document already exists
	if _, exists := vi.documents[doc.ID]; exists {
		// Remove existing document first
		nodeID := vi.idToNodeID[doc.ID]
		vi.index.Delete(nodeID)
		delete(vi.nodeIDToID, nodeID)
	}

	// Add to index
	nodeID := vi.nextNodeID
	vi.nextNodeID++

	// Check if this is the first document being added after all documents were deleted
	// Workaround for https://github.com/coder/hnsw/issues/10
	if vi.index.Len() == 0 {
		// Safe initialization of a new graph when empty
		vi.index = hnsw.NewGraph[int]()
	}
	// Add to HNSW index with proper type conversion
	vi.index.Add(hnsw.MakeNode(nodeID, embedding))

	// Store mappings
	vi.documents[doc.ID] = doc
	vi.idToNodeID[doc.ID] = nodeID
	vi.nodeIDToID[nodeID] = doc.ID

	return nil
}

// AddDocuments adds multiple documents to the index.
func (vi *VectorIndex) AddDocuments(ctx context.Context, docs []*Document) error {
	if len(docs) == 0 {
		return errors.New("no documents to add")
	}

	// Extract content for batch embedding
	contents := make([]string, len(docs))
	for i, doc := range docs {
		if doc.Content == "" {
			return fmt.Errorf("document with ID %d has empty content", doc.ID)
		}
		contents[i] = doc.Content
	}

	// Get embeddings for all documents
	embeddings, err := vi.client.GetBatchEmbeddings(ctx, contents)
	if err != nil {
		return fmt.Errorf("failed to generate batch embeddings: %w", err)
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	// Check if this is the first document being added after all documents were deleted
	// Workaround for https://github.com/coder/hnsw/issues/10
	if vi.index.Len() == 0 {
		// Safe initialization of a new graph when empty
		vi.index = hnsw.NewGraph[int]()
	}

	for i, doc := range docs {
		// Store the embedding in the document
		doc.Vector = embeddings[i]

		// Check if document already exists
		if _, exists := vi.documents[doc.ID]; exists {
			// Remove existing document first
			nodeID := vi.idToNodeID[doc.ID]
			vi.index.Delete(nodeID)
			delete(vi.nodeIDToID, nodeID)
		}

		nodeID := vi.nextNodeID
		vi.nextNodeID++

		// Add to HNSW index
		vi.index.Add(hnsw.MakeNode(nodeID, embeddings[i]))

		// Store mappings
		vi.documents[doc.ID] = doc
		vi.idToNodeID[doc.ID] = nodeID
		vi.nodeIDToID[nodeID] = doc.ID
	}

	return nil
}

// RemoveDocument removes a document from the index.
func (vi *VectorIndex) RemoveDocument(docID int32) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if _, exists := vi.documents[docID]; !exists {
		return fmt.Errorf("document with ID %d not found", docID)
	}

	nodeID := vi.idToNodeID[docID]
	vi.index.Delete(nodeID)
	delete(vi.documents, docID)
	delete(vi.idToNodeID, docID)
	delete(vi.nodeIDToID, nodeID)

	return nil
}

// Search performs a semantic search with the given query text.
func (vi *VectorIndex) Search(ctx context.Context, queryText string, limit int) ([]SearchResult, error) {
	if queryText == "" {
		return nil, errors.New("empty query text")
	}

	// Use the configured query instruction
	queryInstruction := vi.client.config.QueryInstruction

	// Generate embedding for the query with the instruction
	queryEmbedding, err := vi.client.GetEmbedding(ctx, queryInstruction+queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding for query: %w", err)
	}

	return vi.SearchByVector(queryEmbedding, limit)
}

// SearchByVector performs a semantic search with the given embedding vector.
func (vi *VectorIndex) SearchByVector(queryVector []float32, limit int) ([]SearchResult, error) {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if len(vi.documents) == 0 {
		return []SearchResult{}, nil
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Perform search
	results := vi.index.Search(queryVector, limit)

	// Convert to SearchResults
	searchResults := make([]SearchResult, 0, len(results))
	for _, result := range results {
		docID, exists := vi.nodeIDToID[result.Key]
		if !exists {
			continue // Skip if mapping doesn't exist
		}

		doc, exists := vi.documents[docID]
		if !exists {
			continue // Skip if document doesn't exist
		}

		// Calculate distance (or similarity) between query and result
		distance := vi.index.Distance(queryVector, result.Value)
		// Convert distance to similarity score (cosine similarity = 1 - cosine distance)
		similarityScore := float32(1.0 - distance)

		searchResults = append(searchResults, SearchResult{
			ID:       docID,
			Score:    similarityScore,
			Metadata: doc.Metadata,
		})
	}

	// Sort results by score (descending)
	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Score > searchResults[j].Score
	})

	return searchResults, nil
}
