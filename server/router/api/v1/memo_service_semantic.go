package v1

import (
	"context"
	"log/slog"
	"strconv"
	"sync"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/plugin/semantic"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"github.com/usememos/memos/store"
)

// semanticSearcher is the singleton instance of the semantic search engine.
var (
	semanticSearcher    *semantic.VectorIndex
	semanticSearchMutex sync.Mutex
	isIndexed           map[int32]bool
)

// InitSemanticSearch initializes the semantic search engine with the provided configuration.
func InitSemanticSearch(config semantic.Config) error {
	semanticSearchMutex.Lock()
	defer semanticSearchMutex.Unlock()

	embeddingClient := semantic.NewEmbeddingClient(config.APIKey, config.BaseURL, config.EmbeddingModel, config.QueryInstruction)
	semanticSearcher = semantic.NewVectorIndex(embeddingClient)

	isIndexed = make(map[int32]bool)
	return nil
}

// GetSemanticSearcher returns the semantic search engine instance.
func (s *APIV1Service) GetSemanticSearcher() *semantic.VectorIndex {
	semanticSearchMutex.Lock()
	defer semanticSearchMutex.Unlock()
	return semanticSearcher
}

// IsSemanticSearchEnabled returns true if the semantic search engine is initialized.
func (s *APIV1Service) IsSemanticSearchEnabled() bool {
	semanticSearchMutex.Lock()
	defer semanticSearchMutex.Unlock()
	return semanticSearcher != nil
}

// ensureIndex ensures the semantic search index is built for all accessible memos
func (s *APIV1Service) ensureIndex(ctx context.Context, userID int32) error {
	semanticSearchMutex.Lock()
	defer semanticSearchMutex.Unlock()

	if isIndexed[userID] {
		return nil
	}

	// Get all accessible memos
	memoFind := &store.FindMemo{}
	if userID > 0 {
		internalFilter := `creator_id == ` + strconv.Itoa(int(userID)) + ` || visibility in ["PUBLIC", "PROTECTED"]`
		memoFind.Filter = &internalFilter
	} else {
		memoFind.VisibilityList = []store.Visibility{store.Public}
	}

	memos, err := s.Store.ListMemos(ctx, memoFind)
	if err != nil {
		return errors.Wrap(err, "failed to list memos")
	}

	// Convert all memos to documents
	docs := make([]*semantic.Document, 0, len(memos))
	for _, memo := range memos {
		docu := semantic.CreateDocumentFromMemo(memo);
		docs = append(docs, docu)
	}

	// Index all memos using AddDocuments
	if err := semanticSearcher.AddDocuments(ctx, docs); err != nil {
		slog.Error("Failed to index memos", "error", err)
	}

	isIndexed[userID] = true
	return nil
}

// SemanticSearchMemos implements the semantic search RPC.
func (s *APIV1Service) SemanticSearchMemos(ctx context.Context, request *v1pb.SemanticSearchMemosRequest) (*v1pb.SemanticSearchMemosResponse, error) {
	if !s.IsSemanticSearchEnabled() {
		return nil, status.Errorf(codes.FailedPrecondition, "semantic search is not enabled")
	}

	searcher := s.GetSemanticSearcher()

	currentUser, err := s.GetCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user")
	}

	// Make sure memos are indexed
	var userID int32
	if currentUser != nil {
		userID = currentUser.ID
	}
	if err := s.ensureIndex(ctx, userID); err != nil {
		slog.Error("Failed to index memos for semantic search", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to index memos for semantic search")
	}

	// Set a reasonable default limit if not specified
	limit := 10
	if request.Limit > 0 {
		limit = int(request.Limit)
	}

	// Perform the semantic search
	results, err := searcher.Search(ctx, request.Query, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "semantic search failed: %v", err)
	}

	// Convert search results to response
	response := &v1pb.SemanticSearchMemosResponse{
		Results: make([]*v1pb.SemanticSearchMemosResponse_SemanticSearchResult, 0, len(results)),
	}

	for _, result := range results {
		// Extract memo ID from metadata
		memoID := result.ID

		// Get the full memo
		memo, err := s.Store.GetMemo(ctx, &store.FindMemo{
			ID: &memoID,
		})
		if err != nil {
			slog.Warn("Failed to get memo for semantic search result", "id", memoID, "error", err)
			continue
		}

		// Skip if memo not found
		if memo == nil {
			continue
		}

		// Convert to API memo
		memoMessage, err := s.convertMemoFromStore(ctx, memo)
		if err != nil {
			slog.Warn("Failed to convert memo", "id", memoID, "error", err)
			continue
		}

		// Add to response
		response.Results = append(response.Results, &v1pb.SemanticSearchMemosResponse_SemanticSearchResult{
			Memo:  memoMessage,
			Score: result.Score,
		})
	}

	return response, nil
}
