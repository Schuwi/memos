import { Divider, List, ListItem, Switch } from "@mui/joy";
import { Button, Input } from "@usememos/mui";
import { isEqual } from "lodash-es";
import React, { useState } from "react";
import { toast } from "react-hot-toast";
import { Link } from "react-router-dom";
import { workspaceSettingNamePrefix } from "@/store/v1";
import { workspaceStore } from "@/store/v2";
import { WorkspaceSettingKey } from "@/store/v2/workspace";
import { WorkspaceSemanticSetting } from "@/types/proto/api/v1/workspace_setting_service";
import { useTranslate } from "@/utils/i18n";

const DEFAULT_QUERY_INSTRUCTION = "Instruct: Given a query, look for notes that could be related in any way\nQuery: ";

const SemanticSearchSection = () => {
  const t = useTranslate();
  const initialSetting = workspaceStore.getWorkspaceSettingByKey(WorkspaceSettingKey.SEMANTIC).semanticSetting;

  const [workspaceSemanticSetting, setWorkspaceSemanticSetting] = useState<WorkspaceSemanticSetting>(
    initialSetting || {
      enabled: false,
      apiKey: "",
      baseUrl: "https://api.openai.com/v1",
      embeddingModel: "text-embedding-ada-002",
      queryInstruction: DEFAULT_QUERY_INSTRUCTION,
    },
  );

  const [initialWorkspaceSemanticSetting, setInitialWorkspaceSemanticSetting] = useState<WorkspaceSemanticSetting>(
    initialSetting || {
      enabled: false,
      apiKey: "",
      baseUrl: "https://api.openai.com/v1",
      embeddingModel: "text-embedding-ada-002",
      queryInstruction: DEFAULT_QUERY_INSTRUCTION,
    },
  );

  const handleEnabledChanged = (event: React.ChangeEvent<HTMLInputElement>) => {
    setWorkspaceSemanticSetting({
      ...workspaceSemanticSetting,
      enabled: event.target.checked,
    });
  };

  const handleApiKeyChanged = (value: string) => {
    setWorkspaceSemanticSetting({
      ...workspaceSemanticSetting,
      apiKey: value,
    });
  };

  const handleBaseUrlChanged = (value: string) => {
    setWorkspaceSemanticSetting({
      ...workspaceSemanticSetting,
      baseUrl: value,
    });
  };

  const handleEmbeddingModelChanged = (value: string) => {
    setWorkspaceSemanticSetting({
      ...workspaceSemanticSetting,
      embeddingModel: value,
    });
  };

  const handleQueryInstructionChanged = (value: string) => {
    setWorkspaceSemanticSetting({
      ...workspaceSemanticSetting,
      queryInstruction: value,
    });
  };

  const handleSaveBtnClick = async () => {
    try {
      // Show confirmation dialog if embedding model has changed
      if (workspaceSemanticSetting.embeddingModel !== initialWorkspaceSemanticSetting.embeddingModel) {
        const confirmed = window.confirm(t("setting.semantic-search-section.model-change-confirmation"));
        if (!confirmed) {
          return;
        }
      }

      await workspaceStore.upsertWorkspaceSetting({
        name: `${workspaceSettingNamePrefix}${WorkspaceSettingKey.SEMANTIC}`,
        semanticSetting: workspaceSemanticSetting,
      });
      setInitialWorkspaceSemanticSetting(workspaceSemanticSetting);
      toast.success(t("message.update-succeed"));
    } catch (error: any) {
      toast.error(error.details);
    }
  };

  const isChanged = !isEqual(initialWorkspaceSemanticSetting, workspaceSemanticSetting);

  return (
    <div className="section-container">
      <p className="title-text">{t("setting.semantic-search")}</p>
      <div className="form-label">
        <div className="flex flex-row justify-between items-center gap-1">
          <span className="text-sm font-medium">{t("setting.semantic-search-section.enable")}</span>
          <Switch checked={workspaceSemanticSetting.enabled} onChange={handleEnabledChanged} />
        </div>
      </div>

      {workspaceSemanticSetting.enabled && (
        <>
          <div className="form-label">
            <div className="flex flex-row justify-between items-center">
              <span className="text-sm font-medium">{t("setting.semantic-search-section.api-key")}</span>
              <Input
                className="w-60"
                type="password"
                value={workspaceSemanticSetting.apiKey}
                placeholder={t("setting.semantic-search-section.api-key-placeholder")}
                onChange={(e) => handleApiKeyChanged(e.target.value)}
              />
            </div>
          </div>

          <div className="form-label">
            <div className="flex flex-row justify-between items-center">
              <span className="text-sm font-medium">{t("setting.semantic-search-section.base-url")}</span>
              <Input
                className="w-60"
                value={workspaceSemanticSetting.baseUrl}
                placeholder="https://api.openai.com/v1"
                onChange={(e) => handleBaseUrlChanged(e.target.value)}
              />
            </div>
          </div>

          <div className="form-label">
            <div className="flex flex-row justify-between items-center">
              <span className="text-sm font-medium">{t("setting.semantic-search-section.embedding-model")}</span>
              <Input
                className="w-60"
                value={workspaceSemanticSetting.embeddingModel}
                placeholder="text-embedding-ada-002"
                onChange={(e) => handleEmbeddingModelChanged(e.target.value)}
              />
            </div>
          </div>

          <div className="form-label mt-4">
            <div className="flex flex-col gap-2">
              <span className="text-sm font-medium">{t("setting.semantic-search-section.query-instruction") || "Query Instruction"}</span>
              <p className="text-xs text-gray-500">{t("setting.semantic-search-section.query-instruction-description")}</p>
              <textarea
                className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 transition"
                rows={4}
                value={workspaceSemanticSetting.queryInstruction}
                placeholder={DEFAULT_QUERY_INSTRUCTION}
                onChange={(e) => handleQueryInstructionChanged(e.target.value)}
                style={{ minHeight: "100px", resize: "vertical" }}
              />
            </div>
          </div>
        </>
      )}

      <div className="mt-4">
        <Button disabled={!isChanged} onClick={handleSaveBtnClick}>
          {t("common.save")}
        </Button>
      </div>

      <Divider className="!my-4" />

      <div className="w-full mt-2">
        <p className="text-sm">{t("common.learn-more")}:</p>
        <List component="ul" marker="disc" size="sm">
          <ListItem>
            <Link className="text-sm text-blue-600 hover:underline" to="https://platform.openai.com/docs/guides/embeddings" target="_blank">
              {t("setting.semantic-search-section.embeddings")}
            </Link>
          </ListItem>
        </List>
      </div>
    </div>
  );
};

export default SemanticSearchSection;
