-- 外部插件安装登记表：外部插件"已安装"的唯一事实源（phase-4 决策 5）。
-- 启停状态不在本表：与内建层共用 plugin_states（无行 = disabled），
-- 安装后默认不写 plugin_states，即默认 disabled；卸载时硬删除本行。
CREATE TABLE IF NOT EXISTS plugin_installations (
    id VARCHAR(64) PRIMARY KEY,
    version VARCHAR(64) NOT NULL,
    manifest JSONB NOT NULL,
    install_path VARCHAR NOT NULL,
    checksum VARCHAR(64) NOT NULL,
    config JSONB,
    installed_by VARCHAR NOT NULL DEFAULT '',
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE plugin_installations IS '外部插件安装登记；无行等价于未安装，启停状态另存 plugin_states';
COMMENT ON COLUMN plugin_installations.id IS '插件 ID（点分命名空间风格，与内建层共享命名空间）';
COMMENT ON COLUMN plugin_installations.version IS '当前安装的版本号（同时是磁盘版本目录名）';
COMMENT ON COLUMN plugin_installations.manifest IS '包内 manifest.json 原文';
COMMENT ON COLUMN plugin_installations.install_path IS '解包后的安装目录（DATA_DIR/plugins/<id>/<version>）';
COMMENT ON COLUMN plugin_installations.checksum IS '上传 zip 包的 SHA256（hex）';
COMMENT ON COLUMN plugin_installations.config IS '管理员写入的插件私有配置（JSON）；NULL = 未配置';
COMMENT ON COLUMN plugin_installations.installed_by IS '最后安装/升级操作者（admin 用户标识）';
COMMENT ON COLUMN plugin_installations.installed_at IS '最后安装/升级时间';
COMMENT ON COLUMN plugin_installations.updated_at IS '最后变更时间（含 config 更新）';
