-- 插件运行时启停状态表：enabled 的唯一事实源（无行 = disabled）。
-- 各实例启动时全量加载到内存快照，变更经 Redis Pub/Sub 广播 + 定时对账收敛；
-- 配置文件只承载插件私有配置，不参与启停判定。
CREATE TABLE IF NOT EXISTS plugin_states (
    id VARCHAR(64) PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by VARCHAR NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE plugin_states IS '插件运行时启停状态；无行等价于 disabled';
COMMENT ON COLUMN plugin_states.id IS '插件 ID（点分命名空间风格，如 demo、job.backup）';
COMMENT ON COLUMN plugin_states.enabled IS '是否启用该插件（唯一事实源）';
COMMENT ON COLUMN plugin_states.updated_by IS '最后操作者（admin 用户名或系统标识）';
COMMENT ON COLUMN plugin_states.updated_at IS '最后变更时间';
