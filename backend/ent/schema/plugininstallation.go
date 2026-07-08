package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PluginInstallation 定义外部插件安装登记的 schema（phase-4 决策 5）。
//
// 该表是外部插件"已安装"的唯一事实源：
//   - id 为插件 ID（与内建层共享点分命名空间），直接作为主键；
//   - 启停状态不在本表：与内建层共用 plugin_states（无行 = disabled），
//     安装后默认不写 plugin_states，即默认 disabled；
//   - manifest 存包内清单原文（JSONB），供升级比对与前端展示；
//   - config 是管理员经 config API 写入的插件私有配置（phase-4 决策 7），
//     升级时保留，卸载时随行删除。
//
// 删除策略：硬删除。卸载 = 停进程 + 删文件 + 删本行，无需保留历史。
type PluginInstallation struct {
	ent.Schema
}

// Annotations 返回 schema 的注解配置。
func (PluginInstallation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "plugin_installations"},
	}
}

// Fields 定义插件安装登记实体的所有字段。
func (PluginInstallation) Fields() []ent.Field {
	return []ent.Field{
		// id: 插件 ID，点分命名（小写字母/数字，段间以 . 或 - 分隔），长度 1-64
		field.String("id").
			MaxLen(64).
			NotEmpty(),

		// version: 当前安装的版本号（同时是磁盘版本目录名）
		field.String("version").
			MaxLen(64).
			NotEmpty(),

		// manifest: 包内 manifest.json 原文
		field.JSON("manifest", json.RawMessage{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// install_path: 解包后的安装目录（DATA_DIR/plugins/<id>/<version>）
		field.String("install_path").
			NotEmpty(),

		// checksum: 上传 zip 包的 SHA256（hex）
		field.String("checksum").
			MaxLen(64).
			NotEmpty(),

		// config: 管理员写入的插件私有配置（JSON）；NULL = 未配置
		field.JSON("config", json.RawMessage{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// installed_by: 最后安装/升级操作者（admin 用户标识）
		field.String("installed_by").
			Default(""),

		// installed_at: 最后安装/升级时间
		field.Time("installed_at").
			Default(time.Now).
			SchemaType(map[string]string{
				dialect.Postgres: "timestamptz",
			}),

		// updated_at: 最后变更时间（含 config 更新）
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{
				dialect.Postgres: "timestamptz",
			}),
	}
}
