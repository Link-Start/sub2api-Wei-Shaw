// Package plugins 是内建插件的唯一编译期装配清单。
package plugins

import (
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/plugins/demo"
)

// Builtin 返回全部内建插件的 Factory 清单，由宿主在 NewManager 时一次性实例化。
//
// 清单纪律：
//   - 新增内建插件 = 在此追加一行 Factory；禁止 init() 自注册等隐式装配；
//   - Factory 构造必须无副作用（仅分配内存），一切资源获取放在 Init；
//   - 清单顺序不承载语义（Manager 按 ID 稳定排序驱动生命周期与快照）；
//   - 列入清单不产生任何行为：插件默认 disabled，启停唯一事实源是 DB
//     （plugin_states 表），未启用的插件不 Init、不 Start。
func Builtin() []pluginkit.Factory {
	return []pluginkit.Factory{
		demo.New,
	}
}
