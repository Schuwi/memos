package store

import (
	"context"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	storepb "github.com/usememos/memos/proto/gen/store"
)

type WorkspaceSetting struct {
	Name        string
	Value       string
	Description string
}

type FindWorkspaceSetting struct {
	Name string
}

type DeleteWorkspaceSetting struct {
	Name string
}

func (s *Store) UpsertWorkspaceSetting(ctx context.Context, upsert *storepb.WorkspaceSetting) (*storepb.WorkspaceSetting, error) {
	workspaceSettingRaw := &WorkspaceSetting{
		Name: upsert.Key.String(),
	}
	var valueBytes []byte
	var err error
	if upsert.Key == storepb.WorkspaceSettingKey_BASIC {
		valueBytes, err = protojson.Marshal(upsert.GetBasicSetting())
	} else if upsert.Key == storepb.WorkspaceSettingKey_GENERAL {
		valueBytes, err = protojson.Marshal(upsert.GetGeneralSetting())
	} else if upsert.Key == storepb.WorkspaceSettingKey_STORAGE {
		valueBytes, err = protojson.Marshal(upsert.GetStorageSetting())
	} else if upsert.Key == storepb.WorkspaceSettingKey_MEMO_RELATED {
		valueBytes, err = protojson.Marshal(upsert.GetMemoRelatedSetting())
	} else if upsert.Key == storepb.WorkspaceSettingKey_SEMANTIC {
		valueBytes, err = protojson.Marshal(upsert.GetSemanticSetting())
	} else {
		return nil, errors.Errorf("unsupported workspace setting key: %v", upsert.Key)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal workspace setting value")
	}
	valueString := string(valueBytes)
	workspaceSettingRaw.Value = valueString
	workspaceSettingRaw, err = s.driver.UpsertWorkspaceSetting(ctx, workspaceSettingRaw)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to upsert workspace setting")
	}
	workspaceSetting, err := convertWorkspaceSettingFromRaw(workspaceSettingRaw)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to convert workspace setting")
	}
	s.workspaceSettingCache.Store(workspaceSetting.Key.String(), workspaceSetting)
	return workspaceSetting, nil
}

func (s *Store) ListWorkspaceSettings(ctx context.Context, find *FindWorkspaceSetting) ([]*storepb.WorkspaceSetting, error) {
	list, err := s.driver.ListWorkspaceSettings(ctx, find)
	if err != nil {
		return nil, err
	}

	workspaceSettings := []*storepb.WorkspaceSetting{}
	for _, workspaceSettingRaw := range list {
		workspaceSetting, err := convertWorkspaceSettingFromRaw(workspaceSettingRaw)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to convert workspace setting")
		}
		if workspaceSetting == nil {
			continue
		}
		s.workspaceSettingCache.Store(workspaceSetting.Key.String(), workspaceSetting)
		workspaceSettings = append(workspaceSettings, workspaceSetting)
	}
	return workspaceSettings, nil
}

func (s *Store) GetWorkspaceSetting(ctx context.Context, find *FindWorkspaceSetting) (*storepb.WorkspaceSetting, error) {
	if cache, ok := s.workspaceSettingCache.Load(find.Name); ok {
		workspaceSetting, ok := cache.(*storepb.WorkspaceSetting)
		if ok {
			return workspaceSetting, nil
		}
	}

	list, err := s.ListWorkspaceSettings(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found multiple workspace settings with key %s", find.Name)
	}
	return list[0], nil
}

func (s *Store) GetWorkspaceBasicSetting(ctx context.Context) (*storepb.WorkspaceBasicSetting, error) {
	workspaceSetting, err := s.GetWorkspaceSetting(ctx, &FindWorkspaceSetting{
		Name: storepb.WorkspaceSettingKey_BASIC.String(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get workspace basic setting")
	}

	workspaceBasicSetting := &storepb.WorkspaceBasicSetting{}
	if workspaceSetting != nil {
		workspaceBasicSetting = workspaceSetting.GetBasicSetting()
	}
	s.workspaceSettingCache.Store(storepb.WorkspaceSettingKey_BASIC.String(), &storepb.WorkspaceSetting{
		Key:   storepb.WorkspaceSettingKey_BASIC,
		Value: &storepb.WorkspaceSetting_BasicSetting{BasicSetting: workspaceBasicSetting},
	})
	return workspaceBasicSetting, nil
}

func (s *Store) GetWorkspaceGeneralSetting(ctx context.Context) (*storepb.WorkspaceGeneralSetting, error) {
	workspaceSetting, err := s.GetWorkspaceSetting(ctx, &FindWorkspaceSetting{
		Name: storepb.WorkspaceSettingKey_GENERAL.String(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get workspace general setting")
	}

	workspaceGeneralSetting := &storepb.WorkspaceGeneralSetting{}
	if workspaceSetting != nil {
		workspaceGeneralSetting = workspaceSetting.GetGeneralSetting()
	}
	s.workspaceSettingCache.Store(storepb.WorkspaceSettingKey_GENERAL.String(), &storepb.WorkspaceSetting{
		Key:   storepb.WorkspaceSettingKey_GENERAL,
		Value: &storepb.WorkspaceSetting_GeneralSetting{GeneralSetting: workspaceGeneralSetting},
	})
	return workspaceGeneralSetting, nil
}

// DefaultContentLengthLimit is the default limit of content length in bytes. 8KB.
const DefaultContentLengthLimit = 8 * 1024

// DefaultReactions is the default reactions for memo related setting.
var DefaultReactions = []string{"👍", "👎", "❤️", "🎉", "😄", "😕", "😢", "😡"}

// DefaultNsfwTags is the default tags that mark content as NSFW for blurring.
var DefaultNsfwTags = []string{"nsfw"}

func (s *Store) GetWorkspaceMemoRelatedSetting(ctx context.Context) (*storepb.WorkspaceMemoRelatedSetting, error) {
	workspaceSetting, err := s.GetWorkspaceSetting(ctx, &FindWorkspaceSetting{
		Name: storepb.WorkspaceSettingKey_MEMO_RELATED.String(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get workspace general setting")
	}

	workspaceMemoRelatedSetting := &storepb.WorkspaceMemoRelatedSetting{}
	if workspaceSetting != nil {
		workspaceMemoRelatedSetting = workspaceSetting.GetMemoRelatedSetting()
	}
	if workspaceMemoRelatedSetting.ContentLengthLimit < DefaultContentLengthLimit {
		workspaceMemoRelatedSetting.ContentLengthLimit = DefaultContentLengthLimit
	}
	if len(workspaceMemoRelatedSetting.Reactions) == 0 {
		workspaceMemoRelatedSetting.Reactions = append(workspaceMemoRelatedSetting.Reactions, DefaultReactions...)
	}
	if len(workspaceMemoRelatedSetting.NsfwTags) == 0 {
		workspaceMemoRelatedSetting.NsfwTags = append(workspaceMemoRelatedSetting.NsfwTags, DefaultNsfwTags...)
	}
	s.workspaceSettingCache.Store(storepb.WorkspaceSettingKey_MEMO_RELATED.String(), &storepb.WorkspaceSetting{
		Key:   storepb.WorkspaceSettingKey_MEMO_RELATED,
		Value: &storepb.WorkspaceSetting_MemoRelatedSetting{MemoRelatedSetting: workspaceMemoRelatedSetting},
	})
	return workspaceMemoRelatedSetting, nil
}

const (
	defaultWorkspaceStorageType       = storepb.WorkspaceStorageSetting_DATABASE
	defaultWorkspaceUploadSizeLimitMb = 30
	defaultWorkspaceFilepathTemplate  = "assets/{timestamp}_{filename}"
)

func (s *Store) GetWorkspaceStorageSetting(ctx context.Context) (*storepb.WorkspaceStorageSetting, error) {
	workspaceSetting, err := s.GetWorkspaceSetting(ctx, &FindWorkspaceSetting{
		Name: storepb.WorkspaceSettingKey_STORAGE.String(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get workspace storage setting")
	}

	workspaceStorageSetting := &storepb.WorkspaceStorageSetting{}
	if workspaceSetting != nil {
		workspaceStorageSetting = workspaceSetting.GetStorageSetting()
	}
	if workspaceStorageSetting.StorageType == storepb.WorkspaceStorageSetting_STORAGE_TYPE_UNSPECIFIED {
		workspaceStorageSetting.StorageType = defaultWorkspaceStorageType
	}
	if workspaceStorageSetting.UploadSizeLimitMb == 0 {
		workspaceStorageSetting.UploadSizeLimitMb = defaultWorkspaceUploadSizeLimitMb
	}
	if workspaceStorageSetting.FilepathTemplate == "" {
		workspaceStorageSetting.FilepathTemplate = defaultWorkspaceFilepathTemplate
	}
	s.workspaceSettingCache.Store(storepb.WorkspaceSettingKey_STORAGE.String(), &storepb.WorkspaceSetting{
		Key:   storepb.WorkspaceSettingKey_STORAGE,
		Value: &storepb.WorkspaceSetting_StorageSetting{StorageSetting: workspaceStorageSetting},
	})
	return workspaceStorageSetting, nil
}

func (s *Store) GetWorkspaceSemanticSetting(ctx context.Context) (*storepb.WorkspaceSemanticSetting, error) {
	workspaceSetting, err := s.GetWorkspaceSetting(ctx, &FindWorkspaceSetting{
		Name: storepb.WorkspaceSettingKey_SEMANTIC.String(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get workspace semantic setting")
	}

	workspaceSemanticSetting := &storepb.WorkspaceSemanticSetting{}
	if workspaceSetting != nil {
		workspaceSemanticSetting = workspaceSetting.GetSemanticSetting()
	}
	s.workspaceSettingCache.Store(storepb.WorkspaceSettingKey_SEMANTIC.String(), &storepb.WorkspaceSetting{
		Key:   storepb.WorkspaceSettingKey_SEMANTIC,
		Value: &storepb.WorkspaceSetting_SemanticSetting{SemanticSetting: workspaceSemanticSetting},
	})
	return workspaceSemanticSetting, nil
}

func convertWorkspaceSettingFromRaw(workspaceSettingRaw *WorkspaceSetting) (*storepb.WorkspaceSetting, error) {
	workspaceSetting := &storepb.WorkspaceSetting{
		Key: storepb.WorkspaceSettingKey(storepb.WorkspaceSettingKey_value[workspaceSettingRaw.Name]),
	}
	switch workspaceSettingRaw.Name {
	case storepb.WorkspaceSettingKey_BASIC.String():
		basicSetting := &storepb.WorkspaceBasicSetting{}
		if err := protojsonUnmarshaler.Unmarshal([]byte(workspaceSettingRaw.Value), basicSetting); err != nil {
			return nil, err
		}
		workspaceSetting.Value = &storepb.WorkspaceSetting_BasicSetting{BasicSetting: basicSetting}
	case storepb.WorkspaceSettingKey_GENERAL.String():
		generalSetting := &storepb.WorkspaceGeneralSetting{}
		if err := protojsonUnmarshaler.Unmarshal([]byte(workspaceSettingRaw.Value), generalSetting); err != nil {
			return nil, err
		}
		workspaceSetting.Value = &storepb.WorkspaceSetting_GeneralSetting{GeneralSetting: generalSetting}
	case storepb.WorkspaceSettingKey_STORAGE.String():
		storageSetting := &storepb.WorkspaceStorageSetting{}
		if err := protojsonUnmarshaler.Unmarshal([]byte(workspaceSettingRaw.Value), storageSetting); err != nil {
			return nil, err
		}
		workspaceSetting.Value = &storepb.WorkspaceSetting_StorageSetting{StorageSetting: storageSetting}
	case storepb.WorkspaceSettingKey_MEMO_RELATED.String():
		memoRelatedSetting := &storepb.WorkspaceMemoRelatedSetting{}
		if err := protojsonUnmarshaler.Unmarshal([]byte(workspaceSettingRaw.Value), memoRelatedSetting); err != nil {
			return nil, err
		}
		workspaceSetting.Value = &storepb.WorkspaceSetting_MemoRelatedSetting{MemoRelatedSetting: memoRelatedSetting}
	case storepb.WorkspaceSettingKey_SEMANTIC.String():
		semanticSetting := &storepb.WorkspaceSemanticSetting{}
		if err := protojsonUnmarshaler.Unmarshal([]byte(workspaceSettingRaw.Value), semanticSetting); err != nil {
			return nil, err
		}
		workspaceSetting.Value = &storepb.WorkspaceSetting_SemanticSetting{SemanticSetting: semanticSetting}
	default:
		// Skip unsupported workspace setting key.
		return nil, nil
	}
	return workspaceSetting, nil
}
