import { useMemo } from "react";
import { workspaceStore } from "@/store/v2";

/**
 * Hook to determine whether semantic search is available for the current user
 * @returns boolean indicating whether semantic search features should be displayed
 */
const useSemanticSearchAvailability = (): boolean => {
  const isAvailable = useMemo(() => {
    // Get semantic setting from the store
    const semanticSetting = workspaceStore.state.semanticSetting;

    // Semantic search is available if it's enabled in the settings
    return semanticSetting?.enabled || false;
  }, [workspaceStore.state.semanticSetting]);

  return isAvailable;
};

export default useSemanticSearchAvailability;
