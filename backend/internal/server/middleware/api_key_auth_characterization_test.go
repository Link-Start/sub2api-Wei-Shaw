//go:build unit

// TASK-005 I-5.1 API Key 拒绝分支特征化测试（Phase-1 回归防线）。
//
// 核对结论：api_key_auth_test.go 既有测试覆盖了分组停用/删除/专属分组回收、
// IP 白/黑名单、余额耗尽、订阅限额（USAGE_LIMIT_EXCEEDED）等分支，但
// Key 自身状态的核心拒绝分支（缺失/无效/禁用/过期/额度耗尽/用户异常）
// 此前没有任何直接断言。本文件按 api_key_auth.go 当前实现逐分支锁定
// 状态码 + 错误体（code/message 精确匹配），并锁定两个既有"怪癖"：
//   - SimpleMode 在计费执行块之前 early-return → 过期/额度耗尽的 Key 放行；
//   - /v1/usage 端点 skipBilling → 过期/额度耗尽的 Key 也放行（代码注释声明的有意行为）。
package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newCharacterizationAuthService 用 stubApiKeyRepo（api_key_auth_test.go 既有夹具）
// 构造 APIKeyService：repoErr 非 nil 时 GetByKey 直接返回该错误；否则按 key 匹配返回克隆。
func newCharacterizationAuthService(apiKey *service.APIKey, repoErr error, cfg *config.Config) *service.APIKeyService {
	repo := &stubApiKeyRepo{
		getByKey: func(ctx context.Context, key string) (*service.APIKey, error) {
			if repoErr != nil {
				return nil, repoErr
			}
			if apiKey == nil || key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			return &clone, nil
		},
	}
	return service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
}

// characterizationAPIKey 构造无分组绑定（避开分组分支干扰）、挂接激活用户的
// 测试 Key，复用 testutil 工厂，按 opts 调整到目标状态。
func characterizationAPIKey(opts ...func(*service.APIKey)) *service.APIKey {
	k := testutil.NewTestAPIKey(func(k *service.APIKey) {
		k.GroupID = nil
		k.Group = nil
		k.User = testutil.NewTestUser()
	})
	for _, opt := range opts {
		opt(k)
	}
	return k
}

// TestAPIKeyAuthCharacterization_KeyRejectionBranches 锁定 I-5.1：
// api_key_auth.go 各拒绝分支的状态码与错误体（code + message 精确值，
// 均从 AbortWithError 调用处手工抄录，非测试运行反填）。
func TestAPIKeyAuthCharacterization_KeyRejectionBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pastExpiry := time.Now().Add(-time.Hour)

	cases := []struct {
		name        string
		apiKey      *service.APIKey
		repoErr     error
		target      string
		setHeader   func(req *http.Request)
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			// api_key_auth.go:63-66 所有 header 均无 Key
			name:        "missing_api_key",
			target:      "/t",
			setHeader:   func(req *http.Request) {},
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "API_KEY_REQUIRED",
			wantMessage: "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header",
		},
		{
			// api_key_auth.go:33-38 query 传参已废弃 → 400（先于一切鉴权）
			name:        "query_param_key_deprecated",
			target:      "/t?key=sk-in-query",
			setHeader:   func(req *http.Request) {},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "api_key_in_query_deprecated",
			wantMessage: "API key in query parameter is deprecated. Please use Authorization header instead.",
		},
		{
			// api_key_auth.go:72-74 Key 不存在 → 401 INVALID_API_KEY
			name:        "unknown_key",
			apiKey:      nil, // repo 返回 ErrAPIKeyNotFound
			target:      "/t",
			setHeader:   func(req *http.Request) { req.Header.Set("x-api-key", "sk-unknown") },
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "INVALID_API_KEY",
			wantMessage: "Invalid API key",
		},
		{
			// api_key_auth.go:76 仓储非 NotFound 错误 → 500 INTERNAL_ERROR
			name:        "repository_internal_error",
			repoErr:     errors.New("db unavailable"),
			target:      "/t",
			setHeader:   func(req *http.Request) { req.Header.Set("x-api-key", "sk-any") },
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "INTERNAL_ERROR",
			wantMessage: "Failed to validate API key",
		},
		{
			// api_key_auth.go:87-92 disabled 无条件拦截（expired/quota_exhausted 除外）
			name:        "disabled_key",
			apiKey:      characterizationAPIKey(func(k *service.APIKey) { k.Status = service.StatusAPIKeyDisabled }),
			target:      "/t",
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "API_KEY_DISABLED",
			wantMessage: "API key is disabled",
		},
		{
			// api_key_auth.go:178-180 状态位 expired（计费执行块）→ 403
			name:        "status_expired_key",
			apiKey:      characterizationAPIKey(func(k *service.APIKey) { k.Status = service.StatusAPIKeyExpired }),
			target:      "/t",
			wantStatus:  http.StatusForbidden,
			wantCode:    "API_KEY_EXPIRED",
			wantMessage: "API key 已过期",
		},
		{
			// api_key_auth.go:175-177 状态位 quota_exhausted → 429
			name:        "status_quota_exhausted_key",
			apiKey:      characterizationAPIKey(func(k *service.APIKey) { k.Status = service.StatusAPIKeyQuotaExhausted }),
			target:      "/t",
			wantStatus:  http.StatusTooManyRequests,
			wantCode:    "API_KEY_QUOTA_EXHAUSTED",
			wantMessage: "API key 额度已用完",
		},
		{
			// api_key_auth.go:184-187 状态仍 active 但 ExpiresAt 已过 → 运行时过期 403
			name:        "runtime_expired_active_key",
			apiKey:      characterizationAPIKey(func(k *service.APIKey) { k.ExpiresAt = &pastExpiry }),
			target:      "/t",
			wantStatus:  http.StatusForbidden,
			wantCode:    "API_KEY_EXPIRED",
			wantMessage: "API key 已过期",
		},
		{
			// api_key_auth.go:188-191 状态仍 active 但用量达到配额 → 运行时耗尽 429
			name: "runtime_quota_exhausted_active_key",
			apiKey: characterizationAPIKey(func(k *service.APIKey) {
				k.Quota = 5
				k.QuotaUsed = 5
			}),
			target:      "/t",
			wantStatus:  http.StatusTooManyRequests,
			wantCode:    "API_KEY_QUOTA_EXHAUSTED",
			wantMessage: "API key 额度已用完",
		},
		{
			// 特征化怪癖：关联用户缺失时，实际拦截点不在中间件的 USER_NOT_FOUND
			// 分支（api_key_auth.go:113-116），而在 service 层——
			// snapshotFromAPIKey（api_key_auth_cache_impl.go:204-206）对 User==nil
			// 返回 nil 快照，loadAuthCacheEntry(:180-182) 将其转换为
			// ErrAPIKeyNotFound → 对外表现为 401 INVALID_API_KEY。
			// 中间件的 USER_NOT_FOUND 分支经 APIKeyService.GetByKey 不可达。
			name:        "user_missing_surfaces_as_invalid_key",
			apiKey:      characterizationAPIKey(func(k *service.APIKey) { k.User = nil }),
			target:      "/t",
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "INVALID_API_KEY",
			wantMessage: "Invalid API key",
		},
		{
			// api_key_auth.go:119-122 用户被停用 → 401
			name:        "user_inactive",
			apiKey:      characterizationAPIKey(func(k *service.APIKey) { k.User.Status = service.StatusDisabled }),
			target:      "/t",
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "USER_INACTIVE",
			wantMessage: "User account is not active",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			router := newAuthTestRouter(newCharacterizationAuthService(tc.apiKey, tc.repoErr, cfg), nil, cfg)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.setHeader != nil {
				tc.setHeader(req)
			} else {
				req.Header.Set("x-api-key", tc.apiKey.Key)
			}
			router.ServeHTTP(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
			requireAPIKeyAuthError(t, w, tc.wantCode, tc.wantMessage)
		})
	}
}

// TestAPIKeyAuthCharacterization_SubscriptionNotFoundRejected 锁定 I-5.1：
// 订阅型分组无活跃订阅（api_key_auth.go:159-163）→ 403 SUBSCRIPTION_NOT_FOUND。
func TestAPIKeyAuthCharacterization_SubscriptionNotFoundRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	group := testutil.NewTestGroup(func(g *service.Group) {
		g.ID = 42
		g.SubscriptionType = service.SubscriptionTypeSubscription
	})
	apiKey := characterizationAPIKey(func(k *service.APIKey) {
		k.GroupID = &group.ID
		k.Group = group
	})

	cfg := &config.Config{RunMode: config.RunModeStandard}
	subscriptionRepo := &stubUserSubscriptionRepo{
		getActive: func(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
			return nil, service.ErrSubscriptionNotFound
		},
	}
	subscriptionService := service.NewSubscriptionService(nil, subscriptionRepo, nil, nil, cfg)
	router := newAuthTestRouter(newCharacterizationAuthService(apiKey, nil, cfg), subscriptionService, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	requireAPIKeyAuthError(t, w, "SUBSCRIPTION_NOT_FOUND", "No active subscription found for this group")
}

// TestAPIKeyAuthCharacterization_SimpleModeSkipsBillingEnforcement 锁定既有怪癖：
// SimpleMode 在计费执行块之前 early-return（api_key_auth.go:132-143），
// 因此过期/额度耗尽的 Key 在 SimpleMode 下放行；disabled 属于基础鉴权层，仍拦截。
func TestAPIKeyAuthCharacterization_SimpleModeSkipsBillingEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pastExpiry := time.Now().Add(-time.Hour)
	cases := []struct {
		name       string
		apiKey     *service.APIKey
		wantStatus int
	}{
		{
			name:       "status_expired_key_allowed",
			apiKey:     characterizationAPIKey(func(k *service.APIKey) { k.Status = service.StatusAPIKeyExpired }),
			wantStatus: http.StatusOK,
		},
		{
			name:       "status_quota_exhausted_key_allowed",
			apiKey:     characterizationAPIKey(func(k *service.APIKey) { k.Status = service.StatusAPIKeyQuotaExhausted }),
			wantStatus: http.StatusOK,
		},
		{
			name:       "runtime_expired_key_allowed",
			apiKey:     characterizationAPIKey(func(k *service.APIKey) { k.ExpiresAt = &pastExpiry }),
			wantStatus: http.StatusOK,
		},
		{
			// disabled 是基础鉴权（始终执行）而非计费执行 → SimpleMode 也拦截
			name:       "disabled_key_still_rejected",
			apiKey:     characterizationAPIKey(func(k *service.APIKey) { k.Status = service.StatusAPIKeyDisabled }),
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			router := newAuthTestRouter(newCharacterizationAuthService(tc.apiKey, nil, cfg), nil, cfg)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.Header.Set("x-api-key", tc.apiKey.Key)
			router.ServeHTTP(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

// TestAPIKeyAuthCharacterization_UsageEndpointSkipsBilling 锁定既有有意行为：
// /v1/usage 只鉴权不计费（api_key_auth.go:148 skipBilling），
// 过期/额度耗尽的 Key 允许查询自身用量；同一 Key 在其他端点仍被拒绝。
func TestAPIKeyAuthCharacterization_UsageEndpointSkipsBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pastExpiry := time.Now().Add(-time.Hour)
	apiKey := characterizationAPIKey(func(k *service.APIKey) { k.ExpiresAt = &pastExpiry })

	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyService := newCharacterizationAuthService(apiKey, nil, cfg)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	router.GET("/v1/usage", ok)
	router.GET("/t", ok)

	// /v1/usage：skipBilling → 过期 Key 放行
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 其他端点：同一过期 Key 仍被 403 拦截
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	requireAPIKeyAuthError(t, w, "API_KEY_EXPIRED", "API key 已过期")
}
