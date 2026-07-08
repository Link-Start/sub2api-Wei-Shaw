package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PluginKV 定义外部插件 KV 能力的存储 schema（phase-4 决策 2 能力面 v1）。
//
// 裁决说明（TASK-002）：独立新表而非复用通用设置表——
//   - 命名空间硬隔离：唯一键 (plugin_id, key)，插件永远无法读写他人条目；
//   - 卸载清理语义：按 plugin_id 一条 DELETE 即可清空整个命名空间；
//   - 与系统设置表混存会让权限边界依赖代码约定而非表结构。
//
// 删除策略：硬删除。卸载插件时随命名空间整体清空。
type PluginKV struct {
	ent.Schema
}

// Annotations 返回 schema 的注解配置。
func (PluginKV) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "plugin_kvs"},
	}
}

// Fields 定义插件 KV 条目的所有字段。
func (PluginKV) Fields() []ent.Field {
	return []ent.Field{
		// plugin_id: 所属插件 ID（命名空间），与 plugin_installations.id 同域
		field.String("plugin_id").
			MaxLen(64).
			NotEmpty(),

		// key: 命名空间内的键（能力面限制单段安全字符，见 pluginhost 校验）
		field.String("key").
			MaxLen(256).
			NotEmpty(),

		// value: 值本体（TEXT；大小上限由能力面施加）
		field.Text("value"),

		// created_at: 首次写入时间
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{
				dialect.Postgres: "timestamptz",
			}),

		// updated_at: 最后写入时间
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{
				dialect.Postgres: "timestamptz",
			}),
	}
}

// Indexes 定义索引：命名空间内键唯一（同时承担按插件清空与前缀扫描）。
func (PluginKV) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plugin_id", "key").Unique(),
	}
}
