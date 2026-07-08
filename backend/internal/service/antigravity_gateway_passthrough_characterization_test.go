//go:build unit

package service

// TASK-003 特征化测试：gemini/antigravity 路径流式/非流式透传（INVARIANTS I-1.3）
// 及上游认证头替换 + 入站敏感头不外泄（I-1.6，antigravity 支路）。
//
// 既有 antigravity_gateway_service_test.go 已覆盖流式 handler 的 usage 计算、
// 断连/超时、Claude 转换等；本文件补齐两处缺口：
//  1. 流式下游 SSE 分帧的逐字节锁定（v1internal 解包重组 + 空行/[DONE] 回写怪癖）；
//  2. 非流式收集路径 handleGeminiStreamToNonStreaming（此前零覆盖）经
//     ForwardGemini(stream=false) 的端到端行为。
//
// 复用既有基础设施：queuedHTTPUpstreamStub / antigravitySettingRepoStub
//（antigravity_gateway_service_test.go）。期望值全部手工推导写死。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newAntigravityCharacterizationAccount() *Account {
	return &Account{
		ID:          401,
		Name:        "ag-characterization",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "ag-upstream-token",
			antigravityProjectIDFallbackCredentialKey: "proj-lock",
			"model_mapping": map[string]any{
				"gemini-2.5-flash": "gemini-2.5-flash",
			},
		},
	}
}

// I-1.3 + I-1.6: ForwardGemini 流式透传——
//   - 下游 SSE 分帧逐字节锁定：data 行解包 v1internal 后重组为 "data: <inner>\n\n"，
//     上游原有空行再额外回写一个 "\n"（帧间出现三连换行，当前实际行为/怪癖），
//     [DONE] 行按原样回写 + 单个 "\n"；
//   - 下游头：200 + 上游 Content-Type + x-request-id 回显 + SSE 控制头；
//   - usage 由 usageMetadata 推导（input = prompt - cached, output = candidates + thoughts）；
//   - 上游请求为全新构建：Authorization 替换为 Bearer access_token，
//     入站 Authorization/Cookie/X-Api-Key 一概不外泄（I-1.6）。
func TestAntigravityForwardGemini_Characterization_StreamUnwrapFramingAndAuthHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body, err := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "hello"}}},
		},
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/antigravity/v1beta/models/gemini-2.5-flash:streamGenerateContent", bytes.NewReader(body))
	// 入站敏感头：绝不能到达上游
	c.Request.Header.Set("Authorization", "Bearer inbound-client-key")
	c.Request.Header.Set("Cookie", "session=secret")
	c.Request.Header.Set("X-Api-Key", "inbound-api-key")

	inner := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"cachedContentTokenCount":1}}`
	upstreamSSE := "data: " + `{"response":` + inner + `}` + "\n\n" + "data: [DONE]\n"

	var capturedReq *http.Request
	upstream := &queuedHTTPUpstreamStub{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"rid-ag-stream-lock"},
				},
				Body: io.NopCloser(bytes.NewReader([]byte(upstreamSSE))),
			},
		},
		onCall: func(req *http.Request, _ *queuedHTTPUpstreamStub) {
			capturedReq = req
		},
	}
	svc := &AntigravityGatewayService{
		settingService: NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}),
		tokenProvider:  &AntigravityTokenProvider{},
		httpUpstream:   upstream,
	}

	result, err := svc.ForwardGemini(context.Background(), c, newAntigravityCharacterizationAccount(), "gemini-2.5-flash", "streamGenerateContent", true, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)

	// 下游 SSE 分帧逐字节锁定（手工推导）：
	//   "data: {response 包装}" 行 → 解包后重组 "data: <inner>\n\n"
	//   上游空行 → 原样回写 "\n"（与 data 行自带的 \n\n 叠加为三连换行，怪癖）
	//   "data: [DONE]" 行 → 原样回写 + "\n"
	expectedDownstream := "data: " + inner + "\n\n" + "\n" + "data: [DONE]\n"
	require.Equal(t, expectedDownstream, rec.Body.String(), "流式下游分帧应与手工推导的解包重组序列逐字节一致")

	// 下游响应头
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"), "Content-Type 应取上游值")
	require.Equal(t, "rid-ag-stream-lock", rec.Header().Get("X-Request-Id"), "上游 x-request-id 应回显给客户端")
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))

	// usage 手工推导：prompt=5 cached=1 candidates=2 → input=4 output=2 cacheRead=1
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.Equal(t, "rid-ag-stream-lock", result.RequestID)

	// I-1.6: 上游请求为全新构建，认证头替换 + 敏感头不外泄
	require.NotNil(t, capturedReq)
	require.Equal(t, "Bearer ag-upstream-token", capturedReq.Header.Get("Authorization"))
	require.Empty(t, capturedReq.Header.Get("Cookie"))
	require.Empty(t, capturedReq.Header.Get("X-Api-Key"))
	require.Equal(t, "application/json", capturedReq.Header.Get("Content-Type"))
	require.Contains(t, capturedReq.URL.Path, "v1internal:streamGenerateContent", "上游走 v1internal 流式端点")
	require.Equal(t, "sse", capturedReq.URL.Query().Get("alt"))

	// 上游请求体：v1internal 包装（project/model/request），原始 contents 保留
	require.Len(t, upstream.requestBodies, 1)
	wrapped := upstream.requestBodies[0]
	require.Equal(t, "proj-lock", gjson.GetBytes(wrapped, "project").String())
	require.Equal(t, "gemini-2.5-flash", gjson.GetBytes(wrapped, "model").String())
	require.Contains(t, gjson.GetBytes(wrapped, "request").Raw, `"hello"`, "原始用户内容应包含在包装后的 request 中")
}

// I-1.3: ForwardGemini 非流式（handleGeminiStreamToNonStreaming，此前零覆盖）——
// 客户端要求非流式时，网关收集上游流式 chunk 合并为单个 JSON：
//   - 文本片段按序拼接进最终响应（"Hello" + " world" → "Hello world"）；
//   - 最终响应取最后一个含 parts 的 chunk（finishReason/usageMetadata 保留）；
//   - 下游 200 + application/json；usage 取最后一次 usageMetadata。
func TestAntigravityForwardGemini_Characterization_NonStreamCollectMergesChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body, err := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "hi"}}},
		},
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/antigravity/v1beta/models/gemini-2.5-flash:generateContent", bytes.NewReader(body))

	frame1 := `{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}}`
	frame2 := `{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"cachedContentTokenCount":2}}}`
	upstreamSSE := "data: " + frame1 + "\n\n" + "data: " + frame2 + "\n\n"

	upstream := &queuedHTTPUpstreamStub{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"rid-ag-nonstream-lock"},
				},
				Body: io.NopCloser(bytes.NewReader([]byte(upstreamSSE))),
			},
		},
	}
	svc := &AntigravityGatewayService{
		settingService: NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}),
		tokenProvider:  &AntigravityTokenProvider{},
		httpUpstream:   upstream,
	}

	// 客户端非流式请求（stream=false），上游仍统一走 streamGenerateContent
	result, err := svc.ForwardGemini(context.Background(), c, newAntigravityCharacterizationAccount(), "gemini-2.5-flash", "generateContent", false, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "rid-ag-nonstream-lock", rec.Header().Get("X-Request-Id"))

	// 合并后的响应体（手工推导）：
	//   基底为最后一个含 parts 的 chunk（frame2），文本合并为 "Hello world"
	merged := rec.Body.String()
	require.Equal(t, "Hello world", gjson.Get(merged, "candidates.0.content.parts.0.text").String(),
		"非流式收集应把各 chunk 文本按序拼接")
	require.Len(t, gjson.Get(merged, "candidates.0.content.parts").Array(), 1, "文本 parts 应合并为单个 part")
	require.Equal(t, "STOP", gjson.Get(merged, "candidates.0.finishReason").String())
	require.Equal(t, int64(10), gjson.Get(merged, "usageMetadata.promptTokenCount").Int(), "usageMetadata 取最后一个 chunk")
	require.Equal(t, int64(2), gjson.Get(merged, "usageMetadata.cachedContentTokenCount").Int())

	// usage 手工推导：prompt=10 cached=2 candidates=8 → input=8 output=8 cacheRead=2
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.NotNil(t, result.FirstTokenMs)

	// 上游请求仍为流式端点（非流式由网关收集，不透传 generateContent）
	require.Len(t, upstream.requestBodies, 1)
}
