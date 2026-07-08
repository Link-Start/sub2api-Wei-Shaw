//go:build unit

// Phase-1 回归防线（TASK-004）：调度域 failover 不变量锁定。
// 本文件锁定"当前代码的实际行为"，供插件化改造全程回归比对：
//   - I-4.2 换号上限：默认值（anthropic=10 / gemini=3 / openai=3）+ 配置覆盖（>0 生效、≤0 忽略）双语义；
//   - I-4.3 /v1/messages 路径换号耗尽后的状态码与错误体结构
//     （openai 兼容路径已由 openai_gateway_handler_test.go 覆盖，此处不重复）。
//
// 期望值全部手工推导自 gateway_handler.go / openai_gateway_handler.go 当前实现并硬编码，
// 不从被测函数输出反填；生产常量被改动时本文件必须变红。
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newSwitchLimitGatewayHandler 以最小依赖构造 GatewayHandler，仅用于观察换号上限字段。
// 构造函数对 nil 服务不解引用（cfg 分支有 nil 守卫），与现网构造路径一致。
func newSwitchLimitGatewayHandler(t *testing.T, cfg *config.Config) *GatewayHandler {
	t.Helper()
	return NewGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil)
}

func newSwitchLimitOpenAIGatewayHandler(t *testing.T, cfg *config.Config) *OpenAIGatewayHandler {
	t.Helper()
	return NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)
}

// TestInvariant_I42_MaxAccountSwitches_Defaults 锁定换号上限默认值语义（I-4.2）。
// 默认值是构造函数内的字面量（gateway_handler.go:78-79、openai_gateway_handler.go:130），
// 无命名常量可引用，因此这里的硬编码断言就是唯一防线：
//   - anthropic /v1/messages 主循环: 10
//   - gemini 兼容路径: 3
//   - openai 兼容路径: 3（与 anthropic 默认值不同，是当前刻意保留的路径差异）
func TestInvariant_I42_MaxAccountSwitches_Defaults(t *testing.T) {
	t.Run("nil config 时使用硬编码默认值", func(t *testing.T) {
		h := newSwitchLimitGatewayHandler(t, nil)
		require.Equal(t, 10, h.maxAccountSwitches, "I-4.2: anthropic 默认换号上限必须为 10")
		require.Equal(t, 3, h.maxAccountSwitchesGemini, "I-4.2: gemini 默认换号上限必须为 3")

		oh := newSwitchLimitOpenAIGatewayHandler(t, nil)
		require.Equal(t, 3, oh.maxAccountSwitches, "I-4.2: openai 默认换号上限必须为 3")
	})

	t.Run("零值 config（未配置）保持默认值", func(t *testing.T) {
		cfg := &config.Config{} // Gateway.MaxAccountSwitches == 0, MaxAccountSwitchesGemini == 0
		h := newSwitchLimitGatewayHandler(t, cfg)
		require.Equal(t, 10, h.maxAccountSwitches, "I-4.2: 配置为 0 视为未配置，不得覆盖默认值 10")
		require.Equal(t, 3, h.maxAccountSwitchesGemini, "I-4.2: 配置为 0 视为未配置，不得覆盖默认值 3")

		oh := newSwitchLimitOpenAIGatewayHandler(t, cfg)
		require.Equal(t, 3, oh.maxAccountSwitches, "I-4.2: openai 配置为 0 视为未配置，不得覆盖默认值 3")
	})
}

// TestInvariant_I42_MaxAccountSwitches_ConfigOverride 锁定配置覆盖语义（I-4.2）：
// cfg.Gateway.MaxAccountSwitches / MaxAccountSwitchesGemini > 0 时覆盖默认值。
// 注意跨 handler 联动 quirk：anthropic 与 openai handler 读取的是同一个
// cfg.Gateway.MaxAccountSwitches 字段，覆盖后两者取值相同（默认值不同这一差异随之消失）。
func TestInvariant_I42_MaxAccountSwitches_ConfigOverride(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 7
	cfg.Gateway.MaxAccountSwitchesGemini = 5

	h := newSwitchLimitGatewayHandler(t, cfg)
	require.Equal(t, 7, h.maxAccountSwitches, "I-4.2: MaxAccountSwitches=7 应覆盖 anthropic 默认值")
	require.Equal(t, 5, h.maxAccountSwitchesGemini, "I-4.2: MaxAccountSwitchesGemini=5 应覆盖 gemini 默认值")

	oh := newSwitchLimitOpenAIGatewayHandler(t, cfg)
	require.Equal(t, 7, oh.maxAccountSwitches,
		"I-4.2: openai handler 复用同一 MaxAccountSwitches 配置字段，覆盖后与 anthropic 相同")
}

// TestInvariant_I42_MaxAccountSwitches_NonPositiveConfigIgnored 锁定 ≤0 忽略语义（I-4.2）：
// 负数配置与 0 一样不生效，保持默认值（条件为 > 0 才覆盖）。
func TestInvariant_I42_MaxAccountSwitches_NonPositiveConfigIgnored(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = -1
	cfg.Gateway.MaxAccountSwitchesGemini = -2

	h := newSwitchLimitGatewayHandler(t, cfg)
	require.Equal(t, 10, h.maxAccountSwitches, "I-4.2: 负数配置必须被忽略，保持默认值 10")
	require.Equal(t, 3, h.maxAccountSwitchesGemini, "I-4.2: 负数配置必须被忽略，保持默认值 3")

	oh := newSwitchLimitOpenAIGatewayHandler(t, cfg)
	require.Equal(t, 3, oh.maxAccountSwitches, "I-4.2: openai 负数配置必须被忽略，保持默认值 3")
}

// ---------------------------------------------------------------------------
// I-4.3 /v1/messages 换号耗尽出口：状态码 + 错误体结构
// ---------------------------------------------------------------------------

// newMessagesExhaustionContext 构造 /v1/messages 非流式耗尽场景的 gin 测试上下文，
// 复用 gateway_handler_stream_failover_test.go 的既有驱动模式。
func newMessagesExhaustionContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c, w
}

// TestInvariant_I43_MessagesExhausted_ErrorBodyByUpstreamStatus 锁定 /v1/messages
// 主循环耗尽出口（gateway_handler.go handleFailoverExhausted → mapUpstreamError →
// errorResponse）的完整客户端可观测行为（I-4.3）：
//  1. 上游状态码 → 客户端状态码/错误类型/错误消息 的映射表（期望值手工推导自 mapUpstreamError）；
//  2. 错误体固定 schema：{"type":"error","error":{"type":...,"message":...}}；
//  3. 无透传规则命中时，上游原始错误消息不得泄露给客户端。
//
// 驱动方式完整复刻主循环出口：先经 FailoverState.HandleFailoverError 得到
// FailoverExhausted，再以 fs.LastFailoverErr 调用 handleFailoverExhausted（streamStarted=false）。
func TestInvariant_I43_MessagesExhausted_ErrorBodyByUpstreamStatus(t *testing.T) {
	const upstreamRawMessage = "raw-upstream-secret-detail"
	upstreamBody := []byte(`{"type":"error","error":{"type":"whatever","message":"` + upstreamRawMessage + `"}}`)

	tests := []struct {
		name           string
		upstreamStatus int
		wantStatus     int
		wantBody       string
	}{
		{
			name:           "401 上游认证失败映射 502",
			upstreamStatus: 401,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"type":"error","error":{"type":"upstream_error","message":"Upstream authentication failed, please contact administrator"}}`,
		},
		{
			name:           "403 上游禁止访问映射 502",
			upstreamStatus: 403,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"type":"error","error":{"type":"upstream_error","message":"Upstream access forbidden, please contact administrator"}}`,
		},
		{
			name:           "429 限流原样保持 429 且错误类型为 rate_limit_error",
			upstreamStatus: 429,
			wantStatus:     http.StatusTooManyRequests,
			wantBody:       `{"type":"error","error":{"type":"rate_limit_error","message":"Upstream rate limit exceeded, please retry later"}}`,
		},
		{
			name:           "529 过载映射 503 overloaded_error",
			upstreamStatus: 529,
			wantStatus:     http.StatusServiceUnavailable,
			wantBody:       `{"type":"error","error":{"type":"overloaded_error","message":"Upstream service overloaded, please retry later"}}`,
		},
		{
			name:           "500 映射 502 临时不可用",
			upstreamStatus: 500,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"type":"error","error":{"type":"upstream_error","message":"Upstream service temporarily unavailable"}}`,
		},
		{
			name:           "503 映射 502 临时不可用",
			upstreamStatus: 503,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"type":"error","error":{"type":"upstream_error","message":"Upstream service temporarily unavailable"}}`,
		},
		{
			name:           "504 映射 502 临时不可用",
			upstreamStatus: 504,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"type":"error","error":{"type":"upstream_error","message":"Upstream service temporarily unavailable"}}`,
		},
		{
			name:           "未知状态码(418)兜底映射 502 Upstream request failed",
			upstreamStatus: 418,
			wantStatus:     http.StatusBadGateway,
			wantBody:       `{"type":"error","error":{"type":"upstream_error","message":"Upstream request failed"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// MaxSwitches=0：首枚 failover 错误即耗尽，复刻主循环的 FailoverExhausted 分支。
			fs := NewFailoverState(0, false)
			failoverErr := &service.UpstreamFailoverError{
				StatusCode:   tt.upstreamStatus,
				ResponseBody: upstreamBody,
			}
			action := fs.HandleFailoverError(context.Background(), &mockTempUnscheduler{}, 100, service.PlatformAnthropic, failoverErr)
			require.Equal(t, FailoverExhausted, action, "I-4.3: MaxSwitches=0 时首个错误必须直接耗尽")

			c, w := newMessagesExhaustionContext(t)
			// errorPassthroughService 为 nil：走默认 mapUpstreamError 映射（无透传规则命中）。
			h := &GatewayHandler{}
			h.handleFailoverExhausted(c, fs.LastFailoverErr, service.PlatformAnthropic, false)

			require.Equal(t, tt.wantStatus, w.Code, "I-4.3: 客户端状态码映射被改动")
			require.JSONEq(t, tt.wantBody, w.Body.String(), "I-4.3: 错误体结构/文案被改动")
			require.NotContains(t, w.Body.String(), upstreamRawMessage,
				"I-4.3: 无透传规则命中时上游原始错误消息不得泄露给客户端")
		})
	}
}

// TestInvariant_I43_MessagesExhaustedSimple_SelectionExhaustedExit 锁定选号耗尽出口（I-4.3）：
// 主循环在 LastFailoverErr==nil 时走 handleFailoverExhaustedSimple(c, 502, ...)
// （gateway_handler.go Messages 主循环 FailoverExhausted 默认分支），
// 客户端可观测行为为 502 + upstream_error + 固定文案。
func TestInvariant_I43_MessagesExhaustedSimple_SelectionExhaustedExit(t *testing.T) {
	c, w := newMessagesExhaustionContext(t)
	h := &GatewayHandler{}
	h.handleFailoverExhaustedSimple(c, 502, false)

	require.Equal(t, http.StatusBadGateway, w.Code)
	require.JSONEq(t,
		`{"type":"error","error":{"type":"upstream_error","message":"Upstream service temporarily unavailable"}}`,
		w.Body.String(),
		"I-4.3: 选号耗尽（无最后错误）出口的错误体被改动")
}

// TestInvariant_I43_MessagesExhausted_SilentRefusalBodyShortCircuits 锁定静默拒答短路分支（I-4.3）：
// 耗尽时若上游错误体 error.code == "openai_silent_refusal"，
// 优先于状态码映射，固定返回 502 + 专属客户端文案。
func TestInvariant_I43_MessagesExhausted_SilentRefusalBodyShortCircuits(t *testing.T) {
	c, w := newMessagesExhaustionContext(t)
	h := &GatewayHandler{}
	failoverErr := &service.UpstreamFailoverError{
		StatusCode:   429, // 若走普通映射应为 429/rate_limit_error；静默拒答分支必须短路它
		ResponseBody: []byte(`{"error":{"code":"openai_silent_refusal","message":"upstream detail"}}`),
	}
	h.handleFailoverExhausted(c, failoverErr, service.PlatformAnthropic, false)

	require.Equal(t, http.StatusBadGateway, w.Code, "I-4.3: 静默拒答分支必须固定映射 502")
	require.JSONEq(t,
		`{"type":"error","error":{"type":"upstream_error","message":"Upstream returned an empty completion without usage; no fallback account was available"}}`,
		w.Body.String(),
		"I-4.3: 静默拒答专属客户端文案被改动")
}
