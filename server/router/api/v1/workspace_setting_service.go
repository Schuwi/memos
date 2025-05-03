package v1

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/plugin/semantic"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

func (s *APIV1Service) GetWorkspaceSetting(ctx context.Context, request *v1pb.GetWorkspaceSettingRequest) (*v1pb.WorkspaceSetting, error) {
	workspaceSettingKeyString, err := ExtractWorkspaceSettingKeyFromName(request.Name)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid workspace setting name: %v", err)
	}

	workspaceSettingKey := storepb.WorkspaceSettingKey(storepb.WorkspaceSettingKey_value[workspaceSettingKeyString])
	// Get workspace setting from store with default value.
	switch workspaceSettingKey {
	case storepb.WorkspaceSettingKey_BASIC:
		_, err = s.Store.GetWorkspaceBasicSetting(ctx)
	case storepb.WorkspaceSettingKey_GENERAL:
		_, err = s.Store.GetWorkspaceGeneralSetting(ctx)
	case storepb.WorkspaceSettingKey_MEMO_RELATED:
		_, err = s.Store.GetWorkspaceMemoRelatedSetting(ctx)
	case storepb.WorkspaceSettingKey_STORAGE:
		_, err = s.Store.GetWorkspaceStorageSetting(ctx)
	case storepb.WorkspaceSettingKey_SEMANTIC:
		_, err = s.Store.GetWorkspaceSemanticSetting(ctx)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported workspace setting key: %v", workspaceSettingKey)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get workspace setting: %v", err)
	}

	workspaceSetting, err := s.Store.GetWorkspaceSetting(ctx, &store.FindWorkspaceSetting{
		Name: workspaceSettingKey.String(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get workspace setting: %v", err)
	}
	if workspaceSetting == nil {
		return nil, status.Errorf(codes.NotFound, "workspace setting not found")
	}

	// For storage settings, only host can get them.
	if workspaceSetting.Key == storepb.WorkspaceSettingKey_STORAGE {
		user, err := s.GetCurrentUser(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
		}
		if user == nil || user.Role != store.RoleHost {
			return nil, status.Errorf(codes.PermissionDenied, "permission denied")
		}
	}

	// For semantic settings, regular users can only see 'enabled' status
	if workspaceSetting.Key == storepb.WorkspaceSettingKey_SEMANTIC {
		user, err := s.GetCurrentUser(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
		}
		if user == nil || user.Role != store.RoleHost {
			// Return only the enabled flag, without sensitive information
			semanticSetting := workspaceSetting.GetSemanticSetting()
			if semanticSetting != nil {
				return &v1pb.WorkspaceSetting{
					Name: fmt.Sprintf("%s%s", WorkspaceSettingNamePrefix, workspaceSetting.Key.String()),
					Value: &v1pb.WorkspaceSetting_SemanticSetting{
						SemanticSetting: &v1pb.WorkspaceSemanticSetting{
							Enabled: semanticSetting.Enabled,
							// Hide sensitive fields for non-host users
							ApiKey:         "",
							BaseUrl:        "",
							EmbeddingModel: "",
							QueryInstruction: "",
						},
					},
				}, nil
			}
		}
	}

	return convertWorkspaceSettingFromStore(workspaceSetting), nil
}

func (s *APIV1Service) SetWorkspaceSetting(ctx context.Context, request *v1pb.SetWorkspaceSettingRequest) (*v1pb.WorkspaceSetting, error) {
	user, err := s.GetCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user.Role != store.RoleHost {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	updateSetting := convertWorkspaceSettingToStore(request.Setting)

	// Handle semantic search setting changes
	if updateSetting.Key == storepb.WorkspaceSettingKey_SEMANTIC {
		// Get the existing semantic setting to compare
		existingSettings, err := s.Store.GetWorkspaceSemanticSetting(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get existing workspace semantic setting: %v", err)
		}

		// Get the new semantic setting
		newSettings := updateSetting.GetSemanticSetting()

		// Check if the enabled state has changed or if settings were modified while enabled
		if existingSettings != nil && (existingSettings.Enabled != newSettings.Enabled ||
			(newSettings.Enabled && (existingSettings.ApiKey != newSettings.ApiKey ||
				existingSettings.BaseUrl != newSettings.BaseUrl ||
				existingSettings.EmbeddingModel != newSettings.EmbeddingModel))) {

			slog.Info("Semantic search settings changed",
				"previousEnabled", existingSettings.Enabled,
				"newEnabled", newSettings.Enabled)

			// Initialize semantic search if it's being enabled or settings were changed while enabled
			if newSettings.Enabled {
				slog.Info("Initializing semantic search with updated settings")
				config := semantic.Config{
					APIKey:         newSettings.ApiKey,
					BaseURL:        newSettings.BaseUrl,
					EmbeddingModel: newSettings.EmbeddingModel,
				}

				// Use defaults if not specified
				if config.BaseURL == "" {
					config.BaseURL = "https://api.openai.com/v1"
				}
				if config.EmbeddingModel == "" {
					config.EmbeddingModel = semantic.DefaultEmbeddingModel
				}

				if err := InitSemanticSearch(config); err != nil {
					slog.Error("Failed to initialize semantic search with updated settings", "error", err)
					return nil, status.Errorf(codes.Internal, "failed to initialize semantic search: %v", err)
				}
				slog.Info("Semantic search initialized successfully")
			} else {
				// Uninitialize semantic search if it's being disabled
				slog.Info("Disabling semantic search")
				semanticSearchMutex.Lock()
				semanticSearcher = nil
				isIndexed = make(map[int32]bool)
				semanticSearchMutex.Unlock()
			}
		}
	}

	workspaceSetting, err := s.Store.UpsertWorkspaceSetting(ctx, updateSetting)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to upsert workspace setting: %v", err)
	}

	return convertWorkspaceSettingFromStore(workspaceSetting), nil
}

func convertWorkspaceSettingFromStore(storeWorkspaceSetting *storepb.WorkspaceSetting) *v1pb.WorkspaceSetting {
	if storeWorkspaceSetting == nil {
		return nil
	}

	workspaceSetting := &v1pb.WorkspaceSetting{
		Name: fmt.Sprintf("%s%s", WorkspaceSettingNamePrefix, storeWorkspaceSetting.Key.String()),
	}

	switch storeWorkspaceSetting.Key {
	case storepb.WorkspaceSettingKey_GENERAL:
		workspaceSetting.Value = &v1pb.WorkspaceSetting_GeneralSetting{
			GeneralSetting: convertWorkspaceGeneralSettingFromStore(storeWorkspaceSetting.GetGeneralSetting()),
		}
	case storepb.WorkspaceSettingKey_STORAGE:
		workspaceSetting.Value = &v1pb.WorkspaceSetting_StorageSetting{
			StorageSetting: convertWorkspaceStorageSettingFromStore(storeWorkspaceSetting.GetStorageSetting()),
		}
	case storepb.WorkspaceSettingKey_MEMO_RELATED:
		workspaceSetting.Value = &v1pb.WorkspaceSetting_MemoRelatedSetting{
			MemoRelatedSetting: convertWorkspaceMemoRelatedSettingFromStore(storeWorkspaceSetting.GetMemoRelatedSetting()),
		}
	case storepb.WorkspaceSettingKey_SEMANTIC:
		workspaceSetting.Value = &v1pb.WorkspaceSetting_SemanticSetting{
			SemanticSetting: convertWorkspaceSemanticSettingFromStore(storeWorkspaceSetting.GetSemanticSetting()),
		}
	}

	return workspaceSetting
}

func convertWorkspaceSettingToStore(setting *v1pb.WorkspaceSetting) *storepb.WorkspaceSetting {
	settingKeyString, _ := ExtractWorkspaceSettingKeyFromName(setting.Name)
	workspaceSetting := &storepb.WorkspaceSetting{
		Key: storepb.WorkspaceSettingKey(storepb.WorkspaceSettingKey_value[settingKeyString]),
	}
	switch workspaceSetting.Key {
	case storepb.WorkspaceSettingKey_GENERAL:
		workspaceSetting.Value = &storepb.WorkspaceSetting_GeneralSetting{
			GeneralSetting: convertWorkspaceGeneralSettingToStore(setting.GetGeneralSetting()),
		}
	case storepb.WorkspaceSettingKey_STORAGE:
		workspaceSetting.Value = &storepb.WorkspaceSetting_StorageSetting{
			StorageSetting: convertWorkspaceStorageSettingToStore(setting.GetStorageSetting()),
		}
	case storepb.WorkspaceSettingKey_MEMO_RELATED:
		workspaceSetting.Value = &storepb.WorkspaceSetting_MemoRelatedSetting{
			MemoRelatedSetting: convertWorkspaceMemoRelatedSettingToStore(setting.GetMemoRelatedSetting()),
		}
	case storepb.WorkspaceSettingKey_SEMANTIC:
		workspaceSetting.Value = &storepb.WorkspaceSetting_SemanticSetting{
			SemanticSetting: convertWorkspaceSemanticSettingToStore(setting.GetSemanticSetting()),
		}
	}
	return workspaceSetting
}

// Add conversion functions for semantic settings
func convertWorkspaceSemanticSettingFromStore(setting *storepb.WorkspaceSemanticSetting) *v1pb.WorkspaceSemanticSetting {
	if setting == nil {
		return nil
	}
	return &v1pb.WorkspaceSemanticSetting{
		Enabled:        setting.Enabled,
		ApiKey:         setting.ApiKey,
		BaseUrl:        setting.BaseUrl,
		EmbeddingModel: setting.EmbeddingModel,
		QueryInstruction: setting.QueryInstruction,
	}
}

func convertWorkspaceSemanticSettingToStore(setting *v1pb.WorkspaceSemanticSetting) *storepb.WorkspaceSemanticSetting {
	if setting == nil {
		return nil
	}
	return &storepb.WorkspaceSemanticSetting{
		Enabled:        setting.Enabled,
		ApiKey:         setting.ApiKey,
		BaseUrl:        setting.BaseUrl,
		EmbeddingModel: setting.EmbeddingModel,
		QueryInstruction: setting.QueryInstruction,
	}
}

func convertWorkspaceGeneralSettingFromStore(setting *storepb.WorkspaceGeneralSetting) *v1pb.WorkspaceGeneralSetting {
	if setting == nil {
		return nil
	}
	generalSetting := &v1pb.WorkspaceGeneralSetting{
		DisallowUserRegistration: setting.DisallowUserRegistration,
		DisallowPasswordAuth:     setting.DisallowPasswordAuth,
		AdditionalScript:         setting.AdditionalScript,
		AdditionalStyle:          setting.AdditionalStyle,
		WeekStartDayOffset:       setting.WeekStartDayOffset,
		DisallowChangeUsername:   setting.DisallowChangeUsername,
		DisallowChangeNickname:   setting.DisallowChangeNickname,
	}
	if setting.CustomProfile != nil {
		generalSetting.CustomProfile = &v1pb.WorkspaceCustomProfile{
			Title:       setting.CustomProfile.Title,
			Description: setting.CustomProfile.Description,
			LogoUrl:     setting.CustomProfile.LogoUrl,
			Locale:      setting.CustomProfile.Locale,
			Appearance:  setting.CustomProfile.Appearance,
		}
	}
	return generalSetting
}

func convertWorkspaceGeneralSettingToStore(setting *v1pb.WorkspaceGeneralSetting) *storepb.WorkspaceGeneralSetting {
	if setting == nil {
		return nil
	}
	generalSetting := &storepb.WorkspaceGeneralSetting{
		DisallowUserRegistration: setting.DisallowUserRegistration,
		DisallowPasswordAuth:     setting.DisallowPasswordAuth,
		AdditionalScript:         setting.AdditionalScript,
		AdditionalStyle:          setting.AdditionalStyle,
		WeekStartDayOffset:       setting.WeekStartDayOffset,
		DisallowChangeUsername:   setting.DisallowChangeUsername,
		DisallowChangeNickname:   setting.DisallowChangeNickname,
	}
	if setting.CustomProfile != nil {
		generalSetting.CustomProfile = &storepb.WorkspaceCustomProfile{
			Title:       setting.CustomProfile.Title,
			Description: setting.CustomProfile.Description,
			LogoUrl:     setting.CustomProfile.LogoUrl,
			Locale:      setting.CustomProfile.Locale,
			Appearance:  setting.CustomProfile.Appearance,
		}
	}
	return generalSetting
}

func convertWorkspaceStorageSettingFromStore(settingpb *storepb.WorkspaceStorageSetting) *v1pb.WorkspaceStorageSetting {
	if settingpb == nil {
		return nil
	}
	setting := &v1pb.WorkspaceStorageSetting{
		StorageType:       v1pb.WorkspaceStorageSetting_StorageType(settingpb.StorageType),
		FilepathTemplate:  settingpb.FilepathTemplate,
		UploadSizeLimitMb: settingpb.UploadSizeLimitMb,
	}
	if settingpb.S3Config != nil {
		setting.S3Config = &v1pb.WorkspaceStorageSetting_S3Config{
			AccessKeyId:     settingpb.S3Config.AccessKeyId,
			AccessKeySecret: settingpb.S3Config.AccessKeySecret,
			Endpoint:        settingpb.S3Config.Endpoint,
			Region:          settingpb.S3Config.Region,
			Bucket:          settingpb.S3Config.Bucket,
			UsePathStyle:    settingpb.S3Config.UsePathStyle,
		}
	}
	return setting
}

func convertWorkspaceStorageSettingToStore(setting *v1pb.WorkspaceStorageSetting) *storepb.WorkspaceStorageSetting {
	if setting == nil {
		return nil
	}
	settingpb := &storepb.WorkspaceStorageSetting{
		StorageType:       storepb.WorkspaceStorageSetting_StorageType(setting.StorageType),
		FilepathTemplate:  setting.FilepathTemplate,
		UploadSizeLimitMb: setting.UploadSizeLimitMb,
	}
	if setting.S3Config != nil {
		settingpb.S3Config = &storepb.StorageS3Config{
			AccessKeyId:     setting.S3Config.AccessKeyId,
			AccessKeySecret: setting.S3Config.AccessKeySecret,
			Endpoint:        setting.S3Config.Endpoint,
			Region:          setting.S3Config.Region,
			Bucket:          setting.S3Config.Bucket,
			UsePathStyle:    setting.S3Config.UsePathStyle,
		}
	}
	return settingpb
}

func convertWorkspaceMemoRelatedSettingFromStore(setting *storepb.WorkspaceMemoRelatedSetting) *v1pb.WorkspaceMemoRelatedSetting {
	if setting == nil {
		return nil
	}
	return &v1pb.WorkspaceMemoRelatedSetting{
		DisallowPublicVisibility: setting.DisallowPublicVisibility,
		DisplayWithUpdateTime:    setting.DisplayWithUpdateTime,
		ContentLengthLimit:       setting.ContentLengthLimit,
		EnableDoubleClickEdit:    setting.EnableDoubleClickEdit,
		EnableLinkPreview:        setting.EnableLinkPreview,
		EnableComment:            setting.EnableComment,
		EnableLocation:           setting.EnableLocation,
		Reactions:                setting.Reactions,
		DisableMarkdownShortcuts: setting.DisableMarkdownShortcuts,
		EnableBlurNsfwContent:    setting.EnableBlurNsfwContent,
		NsfwTags:                 setting.NsfwTags,
	}
}

func convertWorkspaceMemoRelatedSettingToStore(setting *v1pb.WorkspaceMemoRelatedSetting) *storepb.WorkspaceMemoRelatedSetting {
	if setting == nil {
		return nil
	}
	return &storepb.WorkspaceMemoRelatedSetting{
		DisallowPublicVisibility: setting.DisallowPublicVisibility,
		DisplayWithUpdateTime:    setting.DisplayWithUpdateTime,
		ContentLengthLimit:       setting.ContentLengthLimit,
		EnableDoubleClickEdit:    setting.EnableDoubleClickEdit,
		EnableLinkPreview:        setting.EnableLinkPreview,
		EnableComment:            setting.EnableComment,
		EnableLocation:           setting.EnableLocation,
		Reactions:                setting.Reactions,
		DisableMarkdownShortcuts: setting.DisableMarkdownShortcuts,
		EnableBlurNsfwContent:    setting.EnableBlurNsfwContent,
		NsfwTags:                 setting.NsfwTags,
	}
}
