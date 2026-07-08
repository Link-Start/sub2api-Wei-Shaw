package pluginkit

import (
	"log/slog"

	"github.com/Wei-Shaw/sub2api/ent"

	"github.com/redis/go-redis/v9"
)

// Host 是插件能拿到的全部宿主能力。每个插件在 Init 时收到自己的 Host 实例，
// Logger 自动携带 plugin=<id> 字段。
//
// 能力面扩展纪律：每新增一个字段必须在 PROPOSAL 的 Host 能力登记表补一行并说明理由；
// 永不暴露具体 Service——插件需要业务能力时先抽 ports 接口再挂载。
type Host struct {
	Logger *slog.Logger
	DB     *ent.Client
	Redis  *redis.Client
	// Config 解码本插件在 plugins.<id> 下的私有配置子树；未配置时对 out 零值填充并返回 nil。
	Config func(out any) error
}

// HostDeps 是构造各插件 Host 所需的宿主侧依赖（由 Wire 装配）。
type HostDeps struct {
	Logger *slog.Logger
	DB     *ent.Client
	Redis  *redis.Client
}
