package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PluginState 定义插件运行时启停状态的 schema。
//
// 该表是插件 enabled 状态的唯一事实源（无行 = disabled）：
//   - id 为插件 ID（点分命名空间风格，如 "demo"、"job.backup"），直接作为主键；
//   - 各实例启动时全量加载到内存快照，变更经 Redis Pub/Sub 广播 + 定时对账收敛；
//   - 配置文件只承载插件私有配置，不参与启停判定。
//
// 删除策略：硬删除。删除行等价于恢复默认（disabled），无需保留历史；
// 启停操作的审计由 updated_by/updated_at 与应用日志承担。
type PluginState struct {
	ent.Schema
}

// Annotations 返回 schema 的注解配置。
func (PluginState) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "plugin_states"},
	}
}

// Fields 定义插件状态实体的所有字段。
func (PluginState) Fields() []ent.Field {
	return []ent.Field{
		// id: 插件 ID，点分命名（小写字母/数字，段间以 . 或 - 分隔），长度 1-64
		field.String("id").
			MaxLen(64).
			NotEmpty(),

		// enabled: 是否启用该插件（唯一事实源）
		field.Bool("enabled").
			Default(false),

		// updated_by: 最后操作者（admin 用户名或系统标识）
		field.String("updated_by").
			Default(""),

		// updated_at: 最后变更时间
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{
				dialect.Postgres: "timestamptz",
			}),
	}
}
