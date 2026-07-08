//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/plugininstallation"
	"github.com/Wei-Shaw/sub2api/internal/pluginhost"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"

	"github.com/stretchr/testify/require"
)

// newTestInstallation 构造一条可入库的安装登记（manifest 含当前平台二进制声明）。
func newTestInstallation(id pluginkit.ID, version string) *pluginhost.Installation {
	now := time.Now().Truncate(time.Second)
	return &pluginhost.Installation{
		ID:      id,
		Version: version,
		Manifest: &pluginhost.Manifest{
			ID:       id,
			Name:     "Test Plugin",
			Version:  version,
			Protocol: pluginhost.ProtocolHTTP1,
			Backend: &pluginhost.BackendSpec{
				Executables: map[string]string{pluginhost.CurrentPlatform(): "bin/plugin"},
			},
		},
		InstallPath: "/data/plugins/" + string(id) + "/" + version,
		Checksum:    "deadbeef",
		InstalledBy: "admin:1",
		InstalledAt: now,
		UpdatedAt:   now,
	}
}

func TestPluginInstallationRepo_UpsertGetRoundtrip(t *testing.T) {
	client := newPluginStateEntClient(t)
	repo := NewPluginInstallationRepository(client)
	ctx := context.Background()

	_, err := repo.Get(ctx, "acme.hello")
	require.ErrorIs(t, err, pluginhost.ErrNotInstalled)

	inst := newTestInstallation("acme.hello", "1.0.0")
	require.NoError(t, repo.Upsert(ctx, inst))

	got, err := repo.Get(ctx, "acme.hello")
	require.NoError(t, err)
	require.Equal(t, inst.ID, got.ID)
	require.Equal(t, "1.0.0", got.Version)
	require.Equal(t, inst.InstallPath, got.InstallPath)
	require.Equal(t, "deadbeef", got.Checksum)
	require.Equal(t, "admin:1", got.InstalledBy)
	require.Nil(t, got.Config, "未配置时 config 必须为 nil")
	require.WithinDuration(t, inst.InstalledAt, got.InstalledAt, time.Second)

	// manifest 结构化往返
	require.Equal(t, "Test Plugin", got.Manifest.Name)
	exe, ok := got.Manifest.ExecutableFor(pluginhost.CurrentPlatform())
	require.True(t, ok)
	require.Equal(t, "bin/plugin", exe)
}

// TestPluginInstallationRepo_UpsertReplacesRow 同 ID 再次 Upsert 走 conflict-update：
// 仍是单行，字段整体替换（升级路径的登记语义）。
func TestPluginInstallationRepo_UpsertReplacesRow(t *testing.T) {
	client := newPluginStateEntClient(t)
	repo := NewPluginInstallationRepository(client)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, newTestInstallation("acme.hello", "1.0.0")))

	upgraded := newTestInstallation("acme.hello", "2.0.0")
	upgraded.Checksum = "cafebabe"
	upgraded.InstalledBy = "admin:2"
	upgraded.Config = json.RawMessage(`{"port":8080}`)
	require.NoError(t, repo.Upsert(ctx, upgraded))

	count, err := client.PluginInstallation.Query().
		Where(plugininstallation.IDEQ("acme.hello")).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "upsert 不得产生重复行")

	got, err := repo.Get(ctx, "acme.hello")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", got.Version)
	require.Equal(t, "cafebabe", got.Checksum)
	require.Equal(t, "admin:2", got.InstalledBy)
	require.JSONEq(t, `{"port":8080}`, string(got.Config))
}

func TestPluginInstallationRepo_SetConfig(t *testing.T) {
	client := newPluginStateEntClient(t)
	repo := NewPluginInstallationRepository(client)
	ctx := context.Background()

	// 未登记 → ErrNotInstalled
	err := repo.SetConfig(ctx, "acme.hello", json.RawMessage(`{}`))
	require.ErrorIs(t, err, pluginhost.ErrNotInstalled)

	require.NoError(t, repo.Upsert(ctx, newTestInstallation("acme.hello", "1.0.0")))
	require.NoError(t, repo.SetConfig(ctx, "acme.hello", json.RawMessage(`{"debug":true}`)))

	got, err := repo.Get(ctx, "acme.hello")
	require.NoError(t, err)
	require.JSONEq(t, `{"debug":true}`, string(got.Config))

	// nil 清空配置（回到未配置态）
	require.NoError(t, repo.SetConfig(ctx, "acme.hello", nil))
	got, err = repo.Get(ctx, "acme.hello")
	require.NoError(t, err)
	require.Nil(t, got.Config)
}

func TestPluginInstallationRepo_ListStableOrder(t *testing.T) {
	client := newPluginStateEntClient(t)
	repo := NewPluginInstallationRepository(client)
	ctx := context.Background()

	for _, id := range []pluginkit.ID{"zeta", "alpha", "midway"} {
		require.NoError(t, repo.Upsert(ctx, newTestInstallation(id, "1.0.0")))
	}
	rows, err := repo.List(ctx)
	require.NoError(t, err)
	ids := make([]pluginkit.ID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	require.Equal(t, []pluginkit.ID{"alpha", "midway", "zeta"}, ids)
}

func TestPluginInstallationRepo_Delete(t *testing.T) {
	client := newPluginStateEntClient(t)
	repo := NewPluginInstallationRepository(client)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, newTestInstallation("acme.hello", "1.0.0")))
	require.NoError(t, repo.Delete(ctx, "acme.hello"))

	_, err := repo.Get(ctx, "acme.hello")
	require.ErrorIs(t, err, pluginhost.ErrNotInstalled)

	// 不存在时为 no-op
	require.NoError(t, repo.Delete(ctx, "acme.hello"))
}
