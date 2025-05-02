import { CircularProgress, List, ListItem } from "@mui/joy";
import { Button, Input } from "@usememos/mui";
import { SearchIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "react-hot-toast";
import { memoServiceClient } from "@/grpcweb";
import useCurrentUser from "@/hooks/useCurrentUser";
import { Memo } from "@/types/proto/api/v1/memo_service";
import { generateDialog } from "./Dialog";
import MemoContent from "./MemoContent";

interface Props {
  destroy: () => void;
}

interface SearchResult {
  memo: Memo;
  score: number;
}

const SemanticSearchDialog: React.FC<Props> = (props: Props) => {
  const { destroy } = props;
  const currentUser = useCurrentUser();
  const [isSearching, setIsSearching] = useState(false);
  const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
  const [query, setQuery] = useState("");

  const handleSearch = async () => {
    if (!query.trim()) {
      return;
    }

    try {
      setIsSearching(true);
      const { results } = await memoServiceClient.semanticSearchMemos({
        query: query,
        limit: 10,
        parent: currentUser?.name || "users/-",
      });

      setSearchResults(
        results.map((result) => ({
          memo: result.memo!,
          score: result.score,
        })),
      );
    } catch (error: any) {
      console.error("Semantic search failed:", error);
      toast.error("Semantic search failed");
    } finally {
      setIsSearching(false);
    }
  };

  const handleMemoClick = (memo: Memo) => {
    // Extract the memo ID from the name (format: memos/{id})
    const memoId = memo.name.split("/").pop();
    window.location.href = `/memos/${memoId}`;
    destroy();
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      handleSearch();
    }
  };

  return (
    <div className="dialog-container w-full max-w-3xl bg-white dark:bg-zinc-800 rounded-lg shadow-xl p-4">
      <div className="dialog-header flex justify-between items-center mb-4 pb-2 border-b border-gray-100 dark:border-zinc-700">
        <div className="dialog-title text-xl font-semibold">Semantic Search</div>
      </div>
      <div className="dialog-content">
        <div className="w-full flex items-center mb-4">
          <div className="w-full relative">
            <div className="absolute left-3 top-1/2 -translate-y-1/2">
              <SearchIcon className="w-4 h-auto text-gray-400" />
            </div>
            <Input
              className="w-full pl-10"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Search for semantically similar memos..."
              autoFocus
            />
          </div>
        </div>

        {isSearching ? (
          <div className="w-full flex justify-center my-6">
            <CircularProgress size="sm" />
          </div>
        ) : searchResults.length > 0 ? (
          <div className="w-full">
            <List>
              {searchResults.map((result) => (
                <ListItem
                  key={result.memo.name}
                  className="flex flex-col items-start cursor-pointer border rounded-lg p-3 mb-2 hover:bg-gray-50 dark:hover:bg-zinc-700"
                  onClick={() => handleMemoClick(result.memo)}
                >
                  <div className="w-full">
                    <MemoContent nodes={result.memo.nodes} memoName={result.memo.name} />
                  </div>
                  <div className="text-xs text-gray-400 mt-2">Relevance: {(result.score * 100).toFixed(1)}%</div>
                </ListItem>
              ))}
            </List>
          </div>
        ) : query && !isSearching ? (
          <div className="w-full text-center py-6 text-gray-400">No results found</div>
        ) : null}
      </div>
      <div className="dialog-footer flex justify-end gap-2 mt-4 pt-2 border-t border-gray-100 dark:border-zinc-700">
        <Button variant="plain" onClick={destroy}>
          Close
        </Button>
        <Button onClick={handleSearch} disabled={isSearching || !query.trim()}>
          Search
        </Button>
      </div>
    </div>
  );
};

function showSemanticSearchDialog() {
  generateDialog(
    {
      dialogName: "semantic-search-dialog",
      className: "semantic-search-dialog",
    },
    SemanticSearchDialog,
  );
}

export default showSemanticSearchDialog;
