package pluginkit

import (
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// PluginsConfig 是 plugins: 配置子树的解析结果：点分插件 ID → 该插件的私有配置。
// enabled 状态不在此处（唯一事实源是 DB），配置文件只承载插件私有配置。
type PluginsConfig map[ID]map[string]any

// ParsePluginsConfig 从 viper.Get("plugins") 的原始值还原点分插件 ID。
//
// viper 对含点 key 会按 . 拆层存储（"plugins.job.backup.greeting" 变成
// job→backup→greeting 的嵌套 map），因此无法直接以顶层 key 作为插件 ID。
// 本函数按以下规则还原：
//
//  1. key 本身含点（配置文件中的字面 key，如 "job.backup":）→ 视为完整插件 ID，
//     其值即私有配置，不再下钻；
//  2. key 不含点且值是"纯命名空间层"（非空 map 且所有子值均为 map）→ 下钻一层，
//     路径段以 . 拼接；
//  3. 其余情况（值含标量叶子、或为空 map / nil）→ 当前路径即插件 ID，值即私有配置。
//
// 歧义说明：规则 2 无法区分"命名空间层"与"私有配置顶层恰好全是嵌套 map"的插件
// （如插件 "job" 的配置只有 limits: {max: 5}，会被误还原为插件 "job.limits"）。
// 兜底策略：Manager.Bootstrap 会对未匹配任何注册插件的配置 ID 打 warn 日志；
// 插件作者可通过字面点分 key 书写配置（规则 1）或保证私有配置顶层含标量字段规避。
//
// 同一 ID 经字面 key 与拆层两种形态同时出现视为歧义，返回错误而非静默合并。
func ParsePluginsConfig(raw any) (PluginsConfig, error) {
	out := PluginsConfig{}
	if raw == nil {
		return out, nil
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("pluginkit: plugins config must be a map, got %T", raw)
	}
	if err := flattenPlugins("", root, out); err != nil {
		return nil, err
	}
	return out, nil
}

func flattenPlugins(prefix string, node map[string]any, out PluginsConfig) error {
	for key, val := range node {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		var sub map[string]any
		switch v := val.(type) {
		case nil:
			// yaml 中 "demo:" 空段落 → 视为空配置（仅声明存在）。
			sub = map[string]any{}
		case map[string]any:
			sub = v
		default:
			return fmt.Errorf("pluginkit: config for plugin %q must be a map, got %T", full, val)
		}
		if !strings.Contains(key, ".") && isNamespaceLevel(sub) {
			if err := flattenPlugins(full, sub, out); err != nil {
				return err
			}
			continue
		}
		id := ID(full)
		if err := id.Validate(); err != nil {
			return err
		}
		if _, dup := out[id]; dup {
			return fmt.Errorf("pluginkit: ambiguous plugins config: id %q appears both as literal key and nested path", id)
		}
		out[id] = sub
	}
	return nil
}

// isNamespaceLevel 判断一个 map 是否是纯命名空间层（非空且所有子值均为 map）。
func isNamespaceLevel(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for _, v := range m {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// DecoderFor 返回解码 plugins.<id> 私有配置子树的函数（即 Host.Config）。
// 未配置该 ID 时对 out 不做任何写入并返回 nil（零值成功）。
// 解码参数对齐 viper 默认行为（弱类型转换 + duration/slice 字符串 hook），
// 保证插件私有配置与主配置的解码语义一致。
func (c PluginsConfig) DecoderFor(id ID) func(out any) error {
	sub := c[id]
	return func(out any) error {
		if len(sub) == 0 {
			return nil
		}
		dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				mapstructure.StringToSliceHookFunc(","),
			),
			WeaklyTypedInput: true,
			Result:           out,
			TagName:          "mapstructure",
		})
		if err != nil {
			return fmt.Errorf("pluginkit: build decoder for plugins.%s: %w", id, err)
		}
		if err := dec.Decode(sub); err != nil {
			return fmt.Errorf("pluginkit: decode plugins.%s: %w", id, err)
		}
		return nil
	}
}
