-- 外部插件 KV 能力存储表（phase-4 决策 2 能力面 v1，TASK-002）。
-- 独立新表而非复用通用设置表：(plugin_id, key) 唯一键实现命名空间硬隔离，
-- 卸载插件时按 plugin_id 一条 DELETE 清空整个命名空间。
CREATE TABLE IF NOT EXISTS plugin_kvs (
    id BIGSERIAL PRIMARY KEY,
    plugin_id VARCHAR(64) NOT NULL,
    key VARCHAR(256) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS plugin_kvs_plugin_id_key ON plugin_kvs (plugin_id, key);

COMMENT ON TABLE plugin_kvs IS '外部插件 KV 能力存储；(plugin_id, key) 唯一，卸载时按 plugin_id 整体清空';
COMMENT ON COLUMN plugin_kvs.plugin_id IS '所属插件 ID（命名空间），与 plugin_installations.id 同域';
COMMENT ON COLUMN plugin_kvs.key IS '命名空间内的键';
COMMENT ON COLUMN plugin_kvs.value IS '值本体（大小上限由宿主能力面施加）';
COMMENT ON COLUMN plugin_kvs.created_at IS '首次写入时间';
COMMENT ON COLUMN plugin_kvs.updated_at IS '最后写入时间';
