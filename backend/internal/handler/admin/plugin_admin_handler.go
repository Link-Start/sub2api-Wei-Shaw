package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginhost"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

const (
	// pluginUploadOverheadBytes 是 multipart 编码开销的宽限量（上限 = 包上限 + 此值）。
	pluginUploadOverheadBytes = 1 << 20
	// pluginConfigMaxBytes 是 config PUT 请求体的上限。
	pluginConfigMaxBytes = 1 << 20
)

// 插件层级标识（admin 快照的 tier 字段）。
const (
	pluginTierBuiltin  = "builtin"
	pluginTierExternal = "external"
)

// PluginHandler 提供插件运行时的管理端点：状态快照查询、免重启启停，
// 以及外部插件层的上传安装/卸载/配置读写。
// 启停的唯一事实源是 DB（经 StateStore 写入），Manager/Supervisor 经订阅回调
// 驱动生命周期，本 handler 只做参数校验与状态透出，不直接操作插件实例。
type PluginHandler struct {
	manager *pluginkit.Manager
	states  pluginkit.StateStore
	// installer/installs/supervisor 是外部插件层依赖，未接入装配时为 nil，
	// 管理端点返回 503、外部 ID 视为未注册（路由面保持稳定，便于路由 diff 守护）。
	installer  *pluginhost.Installer
	installs   pluginhost.InstallationStore
	supervisor *pluginhost.Supervisor
}

// NewPluginHandler 创建插件管理处理器。installer/installs/supervisor 允许为 nil
// （外部插件层未装配时，外部层端点统一返回 503）。
func NewPluginHandler(
	manager *pluginkit.Manager,
	states pluginkit.StateStore,
	installer *pluginhost.Installer,
	installs pluginhost.InstallationStore,
	supervisor *pluginhost.Supervisor,
) *PluginHandler {
	return &PluginHandler{
		manager:    manager,
		states:     states,
		installer:  installer,
		installs:   installs,
		supervisor: supervisor,
	}
}

// adminPluginStatus 是 admin 快照里的一项：内建/外部统一形状，
// tier 区分层级，外部插件另带安装版本与清单元数据（name/description）。
// 内建插件的名称/描述由前端按 ID 经 i18n 提供，后端不下发。
type adminPluginStatus struct {
	pluginkit.PluginStatus
	Tier        string `json:"tier"`
	Version     string `json:"version,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// List 返回全部插件的状态快照：内建（Manager）与外部（Supervisor）合并，
// 按 ID 稳定排序（两层 ID 不重叠，安装器拒绝占用内建清单）。
func (h *PluginHandler) List(c *gin.Context) {
	builtin := h.manager.Snapshot()
	merged := make([]adminPluginStatus, 0, len(builtin))
	for _, st := range builtin {
		merged = append(merged, adminPluginStatus{PluginStatus: st, Tier: pluginTierBuiltin})
	}
	if h.supervisor != nil {
		for _, st := range h.supervisor.Snapshot() {
			merged = append(merged, adminPluginStatus{
				PluginStatus: externalToPluginStatus(st),
				Tier:         pluginTierExternal,
				Version:      st.Version,
				Name:         st.Name,
				Description:  st.Description,
			})
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	response.Success(c, merged)
}

// externalToPluginStatus 把外部插件状态映射到统一的快照形状。
func externalToPluginStatus(st pluginhost.ExternalPluginStatus) pluginkit.PluginStatus {
	return pluginkit.PluginStatus{
		ID:        st.ID,
		Enabled:   st.Enabled,
		State:     st.State,
		Err:       st.Err,
		StartedAt: st.StartedAt,
	}
}

// Enable 启用插件（免重启）。未注册的 ID 返回 404；重复启用幂等返回 200。
func (h *PluginHandler) Enable(c *gin.Context) {
	h.setEnabled(c, true)
}

// Disable 停用插件（免重启）。未注册的 ID 返回 404；重复停用幂等返回 200。
func (h *PluginHandler) Disable(c *gin.Context) {
	h.setEnabled(c, false)
}

// setEnabled 校验插件已注册（内建）或已安装（外部）后写入启停状态，
// 并回传该插件的最新状态快照。
// SetEnabled 本身幂等（DB upsert + Manager/Supervisor 幂等 start/stop），
// 重复设置同值返回 200。
func (h *PluginHandler) setEnabled(c *gin.Context, enabled bool) {
	id := pluginkit.ID(c.Param("id"))
	_, isBuiltin := h.statusOf(id)
	isExternal := !isBuiltin && h.supervisor != nil && h.supervisor.Handles(id)
	if !isBuiltin && !isExternal {
		response.NotFound(c, "plugin not found")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.states.SetEnabled(c.Request.Context(), id, enabled, fmt.Sprintf("admin:%d", subject.UserID)); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 本实例的生效不依赖广播链路，SetEnabled 返回后快照即反映最新生命周期状态。
	if isExternal {
		st, _ := h.supervisor.StatusOf(id)
		response.Success(c, adminPluginStatus{
			PluginStatus: externalToPluginStatus(st),
			Tier:         pluginTierExternal,
			Version:      st.Version,
			Name:         st.Name,
			Description:  st.Description,
		})
		return
	}
	status, _ := h.statusOf(id)
	response.Success(c, adminPluginStatus{PluginStatus: status, Tier: pluginTierBuiltin})
}

// statusOf 从 Manager 快照中取指定插件的状态；不存在（未注册）返回 false。
func (h *PluginHandler) statusOf(id pluginkit.ID) (pluginkit.PluginStatus, bool) {
	for _, st := range h.manager.Snapshot() {
		if st.ID == id {
			return st, true
		}
	}
	return pluginkit.PluginStatus{}, false
}

// ============================================================
// 外部插件层端点（phase-4）
// ============================================================

// externalPluginInstalled 是上传安装/升级成功的响应载荷。
type externalPluginInstalled struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Checksum    string    `json:"checksum"`
	InstalledBy string    `json:"installed_by"`
	InstalledAt time.Time `json:"installed_at"`
}

// pluginConfigPayload 是 config GET 的响应载荷：当前配置 + 清单声明的 schema。
type pluginConfigPayload struct {
	Config       json.RawMessage `json:"config"`
	ConfigSchema json.RawMessage `json:"config_schema,omitempty"`
}

// Upload 上传 zip 插件包并安装（同 ID 已安装时走升级路径）。
// POST /api/v1/admin/plugins/upload（multipart，单文件字段 file）
//
// 安装后默认 disabled（不写 plugin_states），启用需另调 enable 端点。
func (h *PluginHandler) Upload(c *gin.Context) {
	if h.installer == nil {
		response.Error(c, http.StatusServiceUnavailable, "external plugin layer not available")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	maxBytes := h.installer.MaxPackageBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+pluginUploadOverheadBytes)
	file, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.BadRequest(c, fmt.Sprintf("plugin package exceeds size limit %d bytes", maxBytes))
			return
		}
		response.BadRequest(c, `multipart file field "file" is required`)
		return
	}
	if file.Size > maxBytes {
		response.BadRequest(c, fmt.Sprintf("plugin package exceeds size limit %d bytes", maxBytes))
		return
	}

	tmp, err := os.CreateTemp("", "plugin-upload-*.zip")
	if err != nil {
		response.InternalError(c, "failed to buffer uploaded package")
		return
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		response.InternalError(c, "failed to buffer uploaded package")
		return
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := c.SaveUploadedFile(file, tmpPath); err != nil {
		response.InternalError(c, "failed to save uploaded package")
		return
	}

	inst, err := h.installer.InstallOrUpgrade(c.Request.Context(), tmpPath, fmt.Sprintf("admin:%d", subject.UserID))
	if err != nil {
		writeInstallerError(c, err)
		return
	}
	response.Success(c, externalPluginInstalled{
		ID:          string(inst.ID),
		Version:     inst.Version,
		Checksum:    inst.Checksum,
		InstalledBy: inst.InstalledBy,
		InstalledAt: inst.InstalledAt,
	})
}

// Uninstall 卸载外部插件（若启用先停 → 删文件 → 删登记）。
// DELETE /api/v1/admin/plugins/:id
//
// 仅允许外部层：内建插件 ID 返回 400；未安装返回 404。
func (h *PluginHandler) Uninstall(c *gin.Context) {
	if h.installer == nil {
		response.Error(c, http.StatusServiceUnavailable, "external plugin layer not available")
		return
	}
	id := pluginkit.ID(c.Param("id"))
	if _, isBuiltin := h.statusOf(id); isBuiltin {
		response.BadRequest(c, "builtin plugin cannot be uninstalled")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.installer.Uninstall(c.Request.Context(), id, fmt.Sprintf("admin:%d", subject.UserID)); err != nil {
		writeInstallerError(c, err)
		return
	}
	response.Success(c, nil)
}

// GetConfig 读取外部插件的私有配置与清单声明的 config_schema。
// GET /api/v1/admin/plugins/:id/config
func (h *PluginHandler) GetConfig(c *gin.Context) {
	if h.installs == nil {
		response.Error(c, http.StatusServiceUnavailable, "external plugin layer not available")
		return
	}
	inst, err := h.installs.Get(c.Request.Context(), pluginkit.ID(c.Param("id")))
	if err != nil {
		writeInstallerError(c, err)
		return
	}
	response.Success(c, pluginConfigPayload{
		Config:       inst.Config,
		ConfigSchema: inst.Manifest.ConfigSchema,
	})
}

// PutConfig 写入外部插件的私有配置（请求体即配置 JSON 本体）。
// PUT /api/v1/admin/plugins/:id/config
//
// 校验：JSON 合法性 + 清单 config_schema 的基础校验（type/properties/required 子集）。
// 配置经 env 注入插件进程：写入成功后对已启用且在跑的插件触发进程重启
// （phase-4 决策 7）；重启失败不回滚配置——失败现场经 admin 快照可观测。
func (h *PluginHandler) PutConfig(c *gin.Context) {
	if h.installs == nil {
		response.Error(c, http.StatusServiceUnavailable, "external plugin layer not available")
		return
	}
	inst, err := h.installs.Get(c.Request.Context(), pluginkit.ID(c.Param("id")))
	if err != nil {
		writeInstallerError(c, err)
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, pluginConfigMaxBytes+1))
	if err != nil {
		response.BadRequest(c, "failed to read config body")
		return
	}
	if len(body) == 0 {
		response.BadRequest(c, "config body is required")
		return
	}
	if len(body) > pluginConfigMaxBytes {
		response.BadRequest(c, fmt.Sprintf("config exceeds size limit %d bytes", pluginConfigMaxBytes))
		return
	}
	if !json.Valid(body) {
		response.BadRequest(c, "config must be valid JSON")
		return
	}
	if err := inst.Manifest.ValidateConfig(body); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.installs.SetConfig(c.Request.Context(), inst.ID, body); err != nil {
		writeInstallerError(c, err)
		return
	}
	if h.supervisor != nil {
		// 配置已落库：重启失败只记日志，插件的 failed 态在 admin 快照中可见。
		if err := h.supervisor.ReloadConfig(c.Request.Context(), inst.ID); err != nil {
			slog.Warn("plugin_config_restart_failed", "plugin", string(inst.ID), "error", err)
		}
	}
	response.Success(c, json.RawMessage(body))
}

// writeInstallerError 把安装器错误映射为项目惯例的错误响应：
// 包不合法/撞内建清单 → 400，未安装 → 404，其余走通用映射。
func writeInstallerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pluginhost.ErrInvalidPackage), errors.Is(err, pluginhost.ErrBuiltinConflict):
		response.BadRequest(c, err.Error())
	case errors.Is(err, pluginhost.ErrNotInstalled):
		response.NotFound(c, "plugin not installed")
	default:
		response.ErrorFrom(c, err)
	}
}
