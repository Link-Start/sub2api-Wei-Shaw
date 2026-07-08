package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/plugininstallation"
	"github.com/Wei-Shaw/sub2api/internal/pluginhost"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
)

// PluginInstallationRepository 是 pluginhost.InstallationStore 的持久化实现：
// plugin_installations 表承载外部插件的安装登记（唯一事实源，无行 = 未安装）。
type PluginInstallationRepository struct {
	client *ent.Client
}

var _ pluginhost.InstallationStore = (*PluginInstallationRepository)(nil)

// NewPluginInstallationRepository 创建外部插件安装登记仓储。
func NewPluginInstallationRepository(client *ent.Client) *PluginInstallationRepository {
	return &PluginInstallationRepository{client: client}
}

// Upsert 按 ID 插入或整行替换一条登记（config 未设置时保留 DB 现值）。
func (r *PluginInstallationRepository) Upsert(ctx context.Context, inst *pluginhost.Installation) error {
	manifestRaw, err := json.Marshal(inst.Manifest)
	if err != nil {
		return fmt.Errorf("plugin installation repo: marshal manifest: %w", err)
	}
	create := r.client.PluginInstallation.Create().
		SetID(string(inst.ID)).
		SetVersion(inst.Version).
		SetManifest(manifestRaw).
		SetInstallPath(inst.InstallPath).
		SetChecksum(inst.Checksum).
		SetInstalledBy(inst.InstalledBy).
		SetInstalledAt(inst.InstalledAt).
		SetUpdatedAt(inst.UpdatedAt)
	if inst.Config != nil {
		create.SetConfig(inst.Config)
	}
	if err := create.
		OnConflictColumns(plugininstallation.FieldID).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return fmt.Errorf("plugin installation repo: upsert %s: %w", inst.ID, err)
	}
	return nil
}

// Get 读取一条登记；未登记返回 pluginhost.ErrNotInstalled。
func (r *PluginInstallationRepository) Get(ctx context.Context, id pluginkit.ID) (*pluginhost.Installation, error) {
	row, err := r.client.PluginInstallation.Get(ctx, string(id))
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %q", pluginhost.ErrNotInstalled, id)
		}
		return nil, fmt.Errorf("plugin installation repo: get %s: %w", id, err)
	}
	return toInstallation(row)
}

// List 返回全部登记，按 ID 稳定排序。
func (r *PluginInstallationRepository) List(ctx context.Context) ([]*pluginhost.Installation, error) {
	rows, err := r.client.PluginInstallation.Query().
		Order(ent.Asc(plugininstallation.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin installation repo: list: %w", err)
	}
	out := make([]*pluginhost.Installation, 0, len(rows))
	for _, row := range rows {
		inst, err := toInstallation(row)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, nil
}

// Delete 删除一条登记（不存在时为 no-op）。
func (r *PluginInstallationRepository) Delete(ctx context.Context, id pluginkit.ID) error {
	if _, err := r.client.PluginInstallation.Delete().
		Where(plugininstallation.IDEQ(string(id))).
		Exec(ctx); err != nil {
		return fmt.Errorf("plugin installation repo: delete %s: %w", id, err)
	}
	return nil
}

// SetConfig 更新插件私有配置并刷新 updated_at；未登记返回 pluginhost.ErrNotInstalled。
func (r *PluginInstallationRepository) SetConfig(ctx context.Context, id pluginkit.ID, config json.RawMessage) error {
	update := r.client.PluginInstallation.Update().
		Where(plugininstallation.IDEQ(string(id)))
	if config == nil {
		update.ClearConfig()
	} else {
		update.SetConfig(config)
	}
	affected, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("plugin installation repo: set config %s: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", pluginhost.ErrNotInstalled, id)
	}
	return nil
}

// toInstallation 把 ent 行转换为领域对象（manifest 原文反序列化为结构）。
func toInstallation(row *ent.PluginInstallation) (*pluginhost.Installation, error) {
	manifest, err := pluginhost.ParseManifest(row.Manifest)
	if err != nil {
		return nil, fmt.Errorf("plugin installation repo: decode manifest of %s: %w", row.ID, err)
	}
	return &pluginhost.Installation{
		ID:          pluginkit.ID(row.ID),
		Version:     row.Version,
		Manifest:    manifest,
		InstallPath: row.InstallPath,
		Checksum:    row.Checksum,
		Config:      row.Config,
		InstalledBy: row.InstalledBy,
		InstalledAt: row.InstalledAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}
