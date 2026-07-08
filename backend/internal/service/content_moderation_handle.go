package service

import "sync/atomic"

// ContentModerationHandle 是内容审计服务的可切换引用（原子指针）。
//
// 内容审计已整体迁移为 content-moderation 内建插件（internal/plugins/moderation）：
// 服务实例由插件在 Start 时构造并 Bind、Stop 时 Unbind 并回收。持有方
// （两个网关 handler 与 admin handler）在构造期经 Wire 注入句柄（始终非 nil），
// 每次请求 Get() 解析当前绑定：插件启用时为真实服务；停用/未启用时为 nil——
// 调用侧既有的 nil 守卫使其与迁移前"服务不存在"完全同语义（热路径直通、
// 后台端点熄灭）。
type ContentModerationHandle struct {
	ptr atomic.Pointer[ContentModerationService]
}

// NewContentModerationHandle 创建空句柄（未绑定任何服务）。
func NewContentModerationHandle() *ContentModerationHandle {
	return &ContentModerationHandle{}
}

// Bind 绑定服务实例，此后 Get 返回该实例。
func (h *ContentModerationHandle) Bind(svc *ContentModerationService) {
	if h == nil {
		return
	}
	h.ptr.Store(svc)
}

// Unbind 解除绑定，此后 Get 返回 nil。已解析到旧实例的在途请求不受影响
// （旧实例的同步检查自足、异步入队非阻塞）。
func (h *ContentModerationHandle) Unbind() {
	if h == nil {
		return
	}
	h.ptr.Store(nil)
}

// Get 返回当前绑定的服务；未绑定（或句柄为 nil）时返回 nil。
func (h *ContentModerationHandle) Get() *ContentModerationService {
	if h == nil {
		return nil
	}
	return h.ptr.Load()
}
