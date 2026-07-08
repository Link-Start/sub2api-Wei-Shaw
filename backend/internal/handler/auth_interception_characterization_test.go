//go:build unit

// TASK-005 认证与拦截特征化测试（Phase-1 回归防线）。
//
// 本文件锁定 content_moderation_helper.go 当前的外部可观测行为：
//   - I-5.3 审核服务失败 = 放行（fail-open，正反双向）
//   - I-5.4 拦截状态码钳制：decision.StatusCode ∉ [400,599] → 403
//   - I-5.2 拦截错误码固定 content_policy_violation + 错误体组装
//
// 重要调研结论（不修，仅锁定现状）：
// service.ContentModerationService.Check 在当前实现中【永不返回非 nil error】——
// 全部 14 处 return 都是 (decision, nil)：配置加载失败、风控关闭、上游审核 API
// 超时/5xx/连接失败等一律在 service 层被吞掉并返回 Allowed=true 的决策。
// 因此 runContentModeration（content_moderation_helper.go:61-65）中
// "err != nil → warn 日志 → return nil" 的分支对真实 service 而言是不可达的
// 防御性代码。fail-open 的诚实可锁行为位于 service 层吞错 + helper 透传 +
// handler 门槛（decision != nil && decision.Blocked）三段，本文件按此锁定。
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newModerationCharacterizationService 构造启用 pre_block 审核的真实
// ContentModerationService（复用本包既有的 contentModerationHandlerSettingRepo /
// contentModerationHandlerTestRepo 夹具，见 openai_gateway_handler_test.go）。
// RetryCount 显式置 0（normalize 只钳负值），保证失败场景单次尝试、快速返回。
func newModerationCharacterizationService(t *testing.T, baseURL string, blockStatus int, blockMessage string) (*service.ContentModerationService, *contentModerationHandlerTestRepo) {
	t.Helper()
	cfg := &service.ContentModerationConfig{
		Enabled:      true,
		Mode:         service.ContentModerationModePreBlock,
		BaseURL:      baseURL,
		Model:        "omni-moderation-latest",
		APIKeys:      []string{"sk-moderation-characterization"},
		SampleRate:   100,
		AllGroups:    true,
		BlockStatus:  blockStatus,
		BlockMessage: blockMessage,
		RetryCount:   0,
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationHandlerTestRepo{}
	svc := service.NewContentModerationService(
		&contentModerationHandlerSettingRepo{values: map[string]string{
			service.SettingKeyRiskControlEnabled:      "true",
			service.SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return svc, repo
}

func newModerationCharacterizationContext(t *testing.T, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	return c
}

func newModerationCharacterizationAPIKey() *service.APIKey {
	groupID := int64(3)
	return &service.APIKey{
		ID:      7,
		Name:    "cm-characterization-key",
		GroupID: &groupID,
		User:    &service.User{ID: 11, Email: "cm@example.com", Status: service.StatusActive},
		Group:   &service.Group{ID: groupID, Name: "cm-group", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
	}
}

var moderationCharacterizationBody = []byte(`{"messages":[{"role":"user","content":"characterization moderation prompt"}]}`)

// TestContentModerationStatusCharacterization_ClampsOutOfRangeTo403 锁定 I-5.4：
// contentModerationStatus（content_moderation_helper.go:22-27）纯函数——
// decision 为 nil 或 StatusCode ∉ [400,599] → 403；范围内原样透传。
// 期望值全部手工从代码条件（<400 || >599）推导写死。
func TestContentModerationStatusCharacterization_ClampsOutOfRangeTo403(t *testing.T) {
	cases := []struct {
		name     string
		decision *service.ContentModerationDecision
		want     int
	}{
		{"nil_decision", nil, http.StatusForbidden},                                              // I-5.4
		{"zero_status", &service.ContentModerationDecision{StatusCode: 0}, http.StatusForbidden}, // I-5.4
		{"negative_status", &service.ContentModerationDecision{StatusCode: -1}, http.StatusForbidden},
		{"status_200_below_range", &service.ContentModerationDecision{StatusCode: 200}, http.StatusForbidden},
		{"status_399_boundary_below", &service.ContentModerationDecision{StatusCode: 399}, http.StatusForbidden},
		{"status_400_lower_bound_passthrough", &service.ContentModerationDecision{StatusCode: 400}, 400},
		{"status_403_passthrough", &service.ContentModerationDecision{StatusCode: 403}, 403},
		{"status_429_passthrough", &service.ContentModerationDecision{StatusCode: 429}, 429},
		{"status_451_passthrough", &service.ContentModerationDecision{StatusCode: 451}, 451},
		{"status_599_upper_bound_passthrough", &service.ContentModerationDecision{StatusCode: 599}, 599},
		{"status_600_boundary_above", &service.ContentModerationDecision{StatusCode: 600}, http.StatusForbidden},
		{"status_1000_above_range", &service.ContentModerationDecision{StatusCode: 1000}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, contentModerationStatus(tc.decision))
		})
	}

	// I-5.2 / I-5.4：错误码与 decision 内容无关，固定 content_policy_violation
	//（content_moderation_helper.go:30）。
	require.Equal(t, "content_policy_violation", contentModerationErrorCode(nil))
	require.Equal(t, "content_policy_violation", contentModerationErrorCode(&service.ContentModerationDecision{
		Blocked: true,
		Action:  service.ContentModerationActionBlock,
	}))
}

// TestRunContentModerationCharacterization_UpstreamFailureFailsOpen 锁定 I-5.3 正向：
// 审核上游失败（5xx / 连接拒绝）→ 请求放行。
//
// 当前实现的失败被吞点在 service 层（checkSync 的 audit_api_failed 分支返回
// Allowed=true 决策，Check 返回 nil error），因此 helper 返回的是【非 nil 的
// allow 决策】而非 nil——本断言同时锁定"吞错发生在 service 层"这一事实：
// 若未来有人把 Check 改为向上抛错（runContentModeration 转而返回 nil），
// 或把失败改为拦截（Blocked=true），本测试都会失败。
func TestRunContentModerationCharacterization_UpstreamFailureFailsOpen(t *testing.T) {
	newFailingUpstream := map[string]func(t *testing.T) string{
		// 上游审核 API 返回 500
		"upstream_http_500": func(t *testing.T) string {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			return server.URL
		},
		// 上游审核 API 连接拒绝（模拟宕机/网络故障）
		"upstream_connection_refused": func(t *testing.T) string {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			url := server.URL
			server.Close()
			return url
		},
	}

	for name, buildUpstream := range newFailingUpstream {
		t.Run(name, func(t *testing.T) {
			svc, repo := newModerationCharacterizationService(t, buildUpstream(t), 0, "")
			c := newModerationCharacterizationContext(t, moderationCharacterizationBody)

			decision := runContentModeration(
				c,
				zap.NewNop(),
				svc,
				newModerationCharacterizationAPIKey(),
				middleware2.AuthSubject{UserID: 11},
				service.ContentModerationProtocolAnthropicMessages,
				"claude-sonnet-4-5",
				moderationCharacterizationBody,
			)

			// I-5.3：失败 → 放行。当前行为是返回 Allowed=true 的决策
			//（service 层吞错），而非 helper err 分支的 nil。
			require.NotNil(t, decision, "当前实现中 Check 吞掉上游错误并返回 allow 决策，helper 不应返回 nil")
			require.True(t, decision.Allowed)
			require.False(t, decision.Blocked)
			require.Equal(t, service.ContentModerationActionAllow, decision.Action)

			// handler 门槛复刻（gateway_handler.go:200 等 9 处调用点的统一写法）：
			// decision != nil && decision.Blocked 才拦截 → 此处必须放行。
			intercepted := decision != nil && decision.Blocked
			require.False(t, intercepted, "审核服务失败必须 fail-open 放行请求")

			// RecordNonHits=false：错误路径不落审计日志（同步路径，无异步入队）。
			require.Empty(t, repo.logSnapshot())
		})
	}
}

// TestCheckContentModerationCharacterization_BlockedDecisionPropagates 锁定 I-5.3 反向
// 与 I-5.2：审核命中（pre_block）→ 拦截决策原样透出 handler 包装方法，
// 状态码/消息取自配置，错误码固定，错误体按两种网关格式组装。
func TestCheckContentModerationCharacterization_BlockedDecisionPropagates(t *testing.T) {
	// 上游打分 sexual=0.9，超过默认阈值 0.65 → flagged → pre_block 拦截。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		_, _ = w.Write([]byte(`{"results":[{"category_scores":{"sexual":0.9}}]}`))
	}))
	defer server.Close()

	const blockMessage = "内容审计特征化阻断"
	svc, repo := newModerationCharacterizationService(t, server.URL, http.StatusUnavailableForLegalReasons, blockMessage)
	apiKey := newModerationCharacterizationAPIKey()
	subject := middleware2.AuthSubject{UserID: 11}

	// anthropic 路径包装方法（GatewayHandler.checkContentModeration）
	h := &GatewayHandler{contentModerationHandle: moderationHandleFor(svc)}
	c := newModerationCharacterizationContext(t, moderationCharacterizationBody)
	decision := h.checkContentModeration(c, zap.NewNop(), apiKey, subject, service.ContentModerationProtocolAnthropicMessages, "claude-sonnet-4-5", moderationCharacterizationBody)

	require.NotNil(t, decision)
	require.True(t, decision.Blocked) // I-5.3 反向：命中必须拦截
	require.False(t, decision.Allowed)
	require.True(t, decision.Flagged)
	require.Equal(t, service.ContentModerationActionBlock, decision.Action)
	require.Equal(t, http.StatusUnavailableForLegalReasons, decision.StatusCode) // 配置 BlockStatus 透传
	require.Equal(t, blockMessage, decision.Message)                             // 配置 BlockMessage 透传

	// I-5.4：451 ∈ [400,599] → 不钳制；I-5.2：错误码固定。
	require.Equal(t, http.StatusUnavailableForLegalReasons, contentModerationStatus(decision))
	require.Equal(t, "content_policy_violation", contentModerationErrorCode(decision))

	// I-5.2：/v1/messages 路径拦截错误体（复刻 gateway_handler.go:200-201 的组装）。
	anthropicRec := httptest.NewRecorder()
	anthropicCtx, _ := gin.CreateTestContext(anthropicRec)
	h.errorResponse(anthropicCtx, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	require.Equal(t, http.StatusUnavailableForLegalReasons, anthropicRec.Code)
	require.JSONEq(t,
		`{"type":"error","error":{"type":"content_policy_violation","message":"内容审计特征化阻断"}}`,
		anthropicRec.Body.String(),
	)

	// openai 兼容路径包装方法（OpenAIGatewayHandler.checkContentModeration）同样透出拦截决策。
	oh := &OpenAIGatewayHandler{contentModerationHandle: moderationHandleFor(svc)}
	c2 := newModerationCharacterizationContext(t, moderationCharacterizationBody)
	decision2 := oh.checkContentModeration(c2, zap.NewNop(), apiKey, subject, service.ContentModerationProtocolOpenAIChat, "gpt-5.5", moderationCharacterizationBody)
	require.NotNil(t, decision2)
	require.True(t, decision2.Blocked)
	require.Equal(t, http.StatusUnavailableForLegalReasons, decision2.StatusCode)

	// I-5.2：openai 路径拦截错误体（复刻 openai_chat_completions.go:89-90 的组装）。
	openaiRec := httptest.NewRecorder()
	openaiCtx, _ := gin.CreateTestContext(openaiRec)
	oh.errorResponse(openaiCtx, contentModerationStatus(decision2), contentModerationErrorCode(decision2), decision2.Message)
	require.Equal(t, http.StatusUnavailableForLegalReasons, openaiRec.Code)
	require.JSONEq(t,
		`{"error":{"type":"content_policy_violation","message":"内容审计特征化阻断"}}`,
		openaiRec.Body.String(),
	)

	// 拦截命中会异步落审计日志（enqueueRecord → worker），两次调用各一条。
	var logs []service.ContentModerationLog
	require.Eventually(t, func() bool {
		logs = repo.logSnapshot()
		return len(logs) == 2
	}, time.Second, 10*time.Millisecond)
	for _, log := range logs {
		require.True(t, log.Flagged)
		require.Equal(t, service.ContentModerationActionBlock, log.Action)
	}
}

// TestCheckContentModerationCharacterization_NilServiceSkipsModeration 锁定
// I-5.3 相邻守卫：handler/service 未配置审核时直接返回 nil 决策 =
// 门槛判定放行（decision != nil && decision.Blocked 为 false）。
func TestCheckContentModerationCharacterization_NilServiceSkipsModeration(t *testing.T) {
	c := newModerationCharacterizationContext(t, moderationCharacterizationBody)
	apiKey := newModerationCharacterizationAPIKey()
	subject := middleware2.AuthSubject{UserID: 11}

	var nilGw *GatewayHandler
	require.Nil(t, nilGw.checkContentModeration(c, zap.NewNop(), apiKey, subject, service.ContentModerationProtocolAnthropicMessages, "m", moderationCharacterizationBody))
	require.Nil(t, (&GatewayHandler{}).checkContentModeration(c, zap.NewNop(), apiKey, subject, service.ContentModerationProtocolAnthropicMessages, "m", moderationCharacterizationBody))

	var nilOAI *OpenAIGatewayHandler
	require.Nil(t, nilOAI.checkContentModeration(c, zap.NewNop(), apiKey, subject, service.ContentModerationProtocolOpenAIChat, "m", moderationCharacterizationBody))
	require.Nil(t, (&OpenAIGatewayHandler{}).checkContentModeration(c, zap.NewNop(), apiKey, subject, service.ContentModerationProtocolOpenAIChat, "m", moderationCharacterizationBody))

	// runContentModeration 自身的入参守卫：svc/c/c.Request 任一为 nil → nil。
	zeroSvc := &service.ContentModerationService{}
	require.Nil(t, runContentModeration(nil, zap.NewNop(), zeroSvc, apiKey, subject, "p", "m", moderationCharacterizationBody))
	noReqCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Nil(t, runContentModeration(noReqCtx, zap.NewNop(), zeroSvc, apiKey, subject, "p", "m", moderationCharacterizationBody))
	require.Nil(t, runContentModeration(c, zap.NewNop(), nil, apiKey, subject, "p", "m", moderationCharacterizationBody))

	// 零值 service（settingRepo/repo 为 nil）：Check 走 skip_unavailable 分支，
	// 返回 Allowed=true 决策（放行），同样锁定为当前行为。
	decision := runContentModeration(c, zap.NewNop(), zeroSvc, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, "m", moderationCharacterizationBody)
	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, service.ContentModerationActionAllow, decision.Action)
}
