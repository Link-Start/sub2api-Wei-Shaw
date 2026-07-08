package pluginkit

import "context"

// StateChange 描述一次启停状态变更（本实例或经 Redis 广播的跨实例变更）。
type StateChange struct {
	ID      ID
	Enabled bool
}

// StateStore 是启停状态机端口：唯一事实源为 DB，内存快照供热路径 O(1) 读，
// Redis Pub/Sub 跨实例广播 + 定时对账兜底。实现位于 repository 层。
//
// 语义契约：
//   - Enabled 只读内存快照，无 IO；未知 ID 返回 false（无记录 = disabled）；
//   - SetEnabled 顺序：DB upsert → 本地内存 → Redis 广播 → 本地订阅者回调；
//     本实例的生效不依赖广播链路；
//   - Subscribe 的回调必须在独立 goroutine 或非持锁上下文中调用，允许回调内再调 Enabled；
//     同一 StateChange 至多触发一次本地回调（本实例变更不因收到自己的广播而重复触发）；
//   - Close 停止订阅与对账循环，幂等。
type StateStore interface {
	Enabled(id ID) bool
	SetEnabled(ctx context.Context, id ID, enabled bool, updatedBy string) error
	Subscribe(fn func(StateChange))
	Close() error
}
