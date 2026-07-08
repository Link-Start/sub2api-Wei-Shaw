//go:build unit

// Phase-1 回归防线（TASK-004）：粘性会话不变量锁定。
//   - I-4.4 BindStickySession 以 TTL=1h 写入绑定；bind 后二次查询命中同账号；
//   - I-4.5 粘性逃逸硬语义：shouldClearStickySession 纯函数的触发条件判定表。
//
// 期望值手工推导自 gateway_service.go 当前实现并硬编码（stickySessionTTL、
// derefGroupID、shouldClearStickySession），不从被测函数输出反填。
package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingStickyCache 是 GatewayCache 的记录型内存实现：
// 记录 SetSessionAccountID 收到的全部参数（尤其是 TTL），并以 groupID|hash 为键提供命中查询。
type recordingStickyCache struct {
	bindings map[string]int64

	setCalls         int
	getCalls         int
	lastSetGroupID   int64
	lastSetHash      string
	lastSetAccountID int64
	lastSetTTL       time.Duration

	getErr error
}

var _ GatewayCache = (*recordingStickyCache)(nil)

func stickyBindingKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d|%s", groupID, sessionHash)
}

func (c *recordingStickyCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	c.getCalls++
	if c.getErr != nil {
		return 0, c.getErr
	}
	return c.bindings[stickyBindingKey(groupID, sessionHash)], nil
}

func (c *recordingStickyCache) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	c.setCalls++
	c.lastSetGroupID = groupID
	c.lastSetHash = sessionHash
	c.lastSetAccountID = accountID
	c.lastSetTTL = ttl
	if c.bindings == nil {
		c.bindings = make(map[string]int64)
	}
	c.bindings[stickyBindingKey(groupID, sessionHash)] = accountID
	return nil
}

func (c *recordingStickyCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (c *recordingStickyCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.bindings, stickyBindingKey(groupID, sessionHash))
	return nil
}

// TestInvariant_I44_BindStickySessionUsesOneHourTTL 锁定粘性会话 TTL 语义（I-4.4）：
//  1. 常量 stickySessionTTL 当前值必须为 1 小时（硬编码断言，常量被改动时变红）；
//  2. BindStickySession 必须把该 TTL 原样传给缓存端口 SetSessionAccountID。
func TestInvariant_I44_BindStickySessionUsesOneHourTTL(t *testing.T) {
	// 双断言其一：常量值锁定（time.Hour 为手工推导的期望值，非引用反填）
	require.Equal(t, time.Hour, stickySessionTTL, "I-4.4: stickySessionTTL 常量必须为 1h")

	rec := &recordingStickyCache{}
	svc := &GatewayService{cache: rec}
	groupID := int64(7)

	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "sess-abc", 42))

	// 双断言其二：缓存端口收到的实参锁定
	require.Equal(t, 1, rec.setCalls, "I-4.4: bind 必须恰好触发一次 SetSessionAccountID")
	require.Equal(t, time.Hour, rec.lastSetTTL, "I-4.4: 写入绑定的 TTL 必须为 1h")
	require.Equal(t, int64(7), rec.lastSetGroupID)
	require.Equal(t, "sess-abc", rec.lastSetHash)
	require.Equal(t, int64(42), rec.lastSetAccountID)
}

// TestInvariant_I44_BindThenHitSameAccount 锁定 bind→hit 硬语义（I-4.4）：
// 绑定后以相同 (groupID, sessionHash) 查询必须命中同一账号；
// 不同 groupID 不得串组命中（groupID 参与绑定键）。
func TestInvariant_I44_BindThenHitSameAccount(t *testing.T) {
	rec := &recordingStickyCache{}
	svc := &GatewayService{cache: rec}
	groupID := int64(7)
	otherGroupID := int64(8)

	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "sess-abc", 42))

	got, err := svc.GetCachedSessionAccountID(context.Background(), &groupID, "sess-abc")
	require.NoError(t, err)
	require.Equal(t, int64(42), got, "I-4.4: bind 后二次请求必须命中同账号")

	miss, err := svc.GetCachedSessionAccountID(context.Background(), &otherGroupID, "sess-abc")
	require.NoError(t, err)
	require.Equal(t, int64(0), miss, "I-4.4: 其他分组不得命中本分组的粘性绑定")
}

// TestInvariant_I44_NilGroupIDNormalizedToZero 锁定 nil groupID 归一化 quirk（I-4.4）：
// BindStickySession / GetCachedSessionAccountID 对 nil groupID 均按 derefGroupID=0 处理，
// 因此 nil 与显式 0 指向同一绑定键。
func TestInvariant_I44_NilGroupIDNormalizedToZero(t *testing.T) {
	rec := &recordingStickyCache{}
	svc := &GatewayService{cache: rec}

	require.NoError(t, svc.BindStickySession(context.Background(), nil, "sess-nil", 99))
	require.Equal(t, int64(0), rec.lastSetGroupID, "I-4.4: nil groupID 必须归一化为 0 传给缓存端口")

	zeroGroupID := int64(0)
	got, err := svc.GetCachedSessionAccountID(context.Background(), &zeroGroupID, "sess-nil")
	require.NoError(t, err)
	require.Equal(t, int64(99), got, "I-4.4: nil groupID 绑定与显式 0 分组共享同一绑定键")
}

// TestInvariant_I44_BindStickySessionNoOpGuards 锁定 bind 的静默 no-op 守卫（I-4.4 现状怪癖）：
// sessionHash 为空、accountID<=0、cache 为 nil 时均静默返回 nil，不触碰缓存端口。
func TestInvariant_I44_BindStickySessionNoOpGuards(t *testing.T) {
	rec := &recordingStickyCache{}
	svc := &GatewayService{cache: rec}
	groupID := int64(7)

	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "", 42), "空 sessionHash 应 no-op")
	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "sess", 0), "accountID=0 应 no-op")
	require.NoError(t, svc.BindStickySession(context.Background(), &groupID, "sess", -1), "accountID<0 应 no-op")
	require.Equal(t, 0, rec.setCalls, "I-4.4: 守卫命中时不得触碰缓存端口")

	nilCacheSvc := &GatewayService{cache: nil}
	require.NoError(t, nilCacheSvc.BindStickySession(context.Background(), &groupID, "sess", 42), "nil cache 应 no-op 且不报错")
}

// TestInvariant_I44_GetCachedSessionAccountIDGuardsAndErrorPropagation 锁定查询侧现状（I-4.4）：
//  1. 空 sessionHash / nil cache → (0, nil)，不触碰缓存端口；
//  2. 缓存错误按当前实现原样向上传递（返回 (0, err)）——注意这与函数注释
//     "Returns 0 if no binding exists or on error" 的字面含义不完全一致，属现状怪癖。
func TestInvariant_I44_GetCachedSessionAccountIDGuardsAndErrorPropagation(t *testing.T) {
	rec := &recordingStickyCache{}
	svc := &GatewayService{cache: rec}
	groupID := int64(7)

	got, err := svc.GetCachedSessionAccountID(context.Background(), &groupID, "")
	require.NoError(t, err)
	require.Equal(t, int64(0), got)
	require.Equal(t, 0, rec.getCalls, "空 sessionHash 不得触碰缓存端口")

	nilCacheSvc := &GatewayService{cache: nil}
	got, err = nilCacheSvc.GetCachedSessionAccountID(context.Background(), &groupID, "sess")
	require.NoError(t, err)
	require.Equal(t, int64(0), got)

	cacheErr := errors.New("redis down")
	errCacheSvc := &GatewayService{cache: &recordingStickyCache{getErr: cacheErr}}
	got, err = errCacheSvc.GetCachedSessionAccountID(context.Background(), &groupID, "sess")
	require.ErrorIs(t, err, cacheErr, "I-4.4: 缓存错误当前实现是向上传递而非吞掉")
	require.Equal(t, int64(0), got)
}

// TestInvariant_I45_StickyEscapeTriggerTable 锁定粘性逃逸硬语义（I-4.5）：
// shouldClearStickySession 是纯函数，表驱动锁定各触发条件的判定结果。
// 只锁"何种账号状态必须清绑逃逸/何种必须保持绑定"，不锁内部阈值细节。
func TestInvariant_I45_StickyEscapeTriggerTable(t *testing.T) {
	now := time.Now()
	future := now.Add(30 * time.Minute)
	past := now.Add(-30 * time.Minute)
	modelResetFuture := now.Add(20 * time.Second).Format(time.RFC3339)

	tests := []struct {
		name    string
		account *Account
		model   string
		want    bool
	}{
		// —— 必须保持绑定（不逃逸）——
		{name: "nil 账号不清绑", account: nil, model: "claude-sonnet-4", want: false},
		{
			name:    "健康账号（active+schedulable）保持绑定",
			account: &Account{Status: StatusActive, Schedulable: true},
			model:   "claude-sonnet-4",
			want:    false,
		},
		{
			name:    "临时不可调度已过期恢复绑定",
			account: &Account{Status: StatusActive, Schedulable: true, TempUnschedulableUntil: &past},
			model:   "claude-sonnet-4",
			want:    false,
		},
		{
			name: "其他模型限流不影响本模型绑定",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"model_rate_limits": map[string]any{
						"claude-opus-4": map[string]any{"rate_limit_reset_at": modelResetFuture},
					},
				},
			},
			model: "claude-sonnet-4",
			want:  false,
		},
		// —— 必须清绑逃逸 ——
		{
			name:    "状态 error 触发逃逸",
			account: &Account{Status: StatusError, Schedulable: true},
			model:   "claude-sonnet-4",
			want:    true,
		},
		{
			name:    "状态 disabled 触发逃逸",
			account: &Account{Status: StatusDisabled, Schedulable: true},
			model:   "claude-sonnet-4",
			want:    true,
		},
		{
			name:    "schedulable=false 触发逃逸",
			account: &Account{Status: StatusActive, Schedulable: false},
			model:   "claude-sonnet-4",
			want:    true,
		},
		{
			name:    "临时不可调度（未过期）触发逃逸",
			account: &Account{Status: StatusActive, Schedulable: true, TempUnschedulableUntil: &future},
			model:   "claude-sonnet-4",
			want:    true,
		},
		{
			name:    "账号过载（OverloadUntil 未过期）触发逃逸",
			account: &Account{Status: StatusActive, Schedulable: true, OverloadUntil: &future},
			model:   "claude-sonnet-4",
			want:    true,
		},
		{
			name:    "账号级限流（RateLimitResetAt 未过期）触发逃逸",
			account: &Account{Status: StatusActive, Schedulable: true, RateLimitResetAt: &future},
			model:   "claude-sonnet-4",
			want:    true,
		},
		{
			name: "请求模型处于模型级限流触发逃逸",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"model_rate_limits": map[string]any{
						"claude-sonnet-4": map[string]any{"rate_limit_reset_at": modelResetFuture},
					},
				},
			},
			model: "claude-sonnet-4",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldClearStickySession(tt.account, tt.model),
				"I-4.5: 粘性逃逸触发条件判定被改动")
		})
	}
}
