package semantic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/coder/hnsw"
)

const (
	// DefaultEmbeddingModel is the default model for generating embeddings
	DefaultEmbeddingModel = "text-embedding-ada-002"
)

// Config holds the configuration for the semantic search service
type Config struct {
	// APIKey is the API key for the embedding service (e.g., OpenAI)
	APIKey string
	// BaseURL is the base URL for the embedding API
	BaseURL string
	// EmbeddingModel is the model to use for generating embeddings
	EmbeddingModel string
}

// Document represents a document to be indexed for semantic search.
type Document struct {
	ID       string                 // Unique identifier
	Content  string                 // Text content for embedding
	Metadata map[string]interface{} // Additional metadata
	Vector   []float32              // Embedding vector
}

// VectorIndex manages embeddings and provides semantic search capabilities.
type VectorIndex struct {
	client         *EmbeddingClient
	index          *hnsw.Graph[int]
	documents      map[string]*Document
	idToNodeID     map[string]int
	nodeIDToID     map[int]string
	mu             sync.RWMutex
	nextNodeID     int
}

// SearchResult represents a result from a semantic search
type SearchResult struct {
	// ID is the unique identifier of the document
	ID string
	// Score is the similarity score (higher is more similar)
	Score float32
	// Metadata is additional metadata about the document
	Metadata map[string]interface{}
}

// NewVectorIndex creates a new semantic vector index.
func NewVectorIndex(client *EmbeddingClient) *VectorIndex {
	// Default HNSW parameters
	// efConstruction := 200
	// efSearch := 100
	// M := 16

	// // Create HNSW index
	// config := &hnsw.Config{}
	// config.Distance = hnsw.CosineDistance
	// config.M = M
	// config.MaxElements = maxElements
	// config.EfConstruction = efConstruction

	index := hnsw.NewGraph[int]()

	return &VectorIndex{
		client:         client,
		index:          index,
		documents:      make(map[string]*Document),
		idToNodeID:     make(map[string]int),
		nodeIDToID:     make(map[int]string),
		nextNodeID:     0,
	}
}

// AddDocument adds a document to the index.
func (vi *VectorIndex) AddDocument(ctx context.Context, doc *Document) error {
	if doc.ID == "" {
		return errors.New("document ID cannot be empty")
	}

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
		if doc.ID == "" {
			return fmt.Errorf("document at index %d has empty ID", i)
		}
		if doc.Content == "" {
			return fmt.Errorf("document with ID %s has empty content", doc.ID)
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
func (vi *VectorIndex) RemoveDocument(docID string) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if _, exists := vi.documents[docID]; !exists {
		return fmt.Errorf("document with ID %s not found", docID)
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

	// Generate embedding for the query
	queryEmbedding, err := vi.client.GetEmbedding(ctx, queryText)
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