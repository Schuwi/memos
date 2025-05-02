package semantic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"
)

// EmbeddingClient handles interactions with the OpenAI API for embeddings.
type EmbeddingClient struct {
	client    *openai.Client
	modelName string
	timeout   time.Duration
	config    Config // Store the configuration
}

// NewEmbeddingClient creates a new client for generating embeddings.
func NewEmbeddingClient(apiKey, baseURL, modelName, queryInstruction string) *EmbeddingClient {
	config := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)

	return &EmbeddingClient{
		client:    client,
		modelName: modelName,
		timeout:   30 * time.Second,
		config: Config{
			APIKey:           apiKey,
			BaseURL:          baseURL,
			EmbeddingModel:   modelName,
			QueryInstruction: queryInstruction,
		},
	}
}

// GetEmbedding generates an embedding vector for the given text.
func (c *EmbeddingClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, errors.New("empty text provided for embedding")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.EmbeddingModel(c.modelName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding: %w", err)
	}

	if len(resp.Data) == 0 || len(resp.Data[0].Embedding) == 0 {
		return nil, errors.New("no embedding returned from API")
	}

	return resp.Data[0].Embedding, nil
}

// GetBatchEmbeddings generates embeddings for multiple texts in a single API call.
func (c *EmbeddingClient) GetBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, errors.New("empty texts provided for embedding")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: texts,
		Model: openai.EmbeddingModel(c.modelName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create batch embeddings: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, errors.New("no embeddings returned from API")
	}

	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}
