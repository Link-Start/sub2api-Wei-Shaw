//go:build unit

package service

// TASK-003 特征化测试：openai 路径流式/非流式透传（INVARIANTS I-1.2）
// 及入站敏感头清除 + 上游认证头替换（I-1.6，openai raw CC 直转支路）。
//
// 既有 openai_gateway_chat_completions_raw_test.go 的断言为 Contains 级别；
// 本文件补齐"SSE data: 行分帧序列逐字节"与"非流式逐字节 body + 状态码 + header 过滤"
// 两类精确锁定。期望值全部手工推导写死。
//
// 复用既有基础设施：httpUpstreamRecorder（openai_oauth_passthrough_test.go）、
// rawChatCompletionsTestConfig/rawChatCompletionsTestAccount
// （openai_gateway_chat_completions_raw_test.go）。

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// I-1.2 + I-1.6: raw CC 流式透传——
//   - 下游 body 与上游 SSE 字节序列完全一致（逐行回写 line+"\n"，分帧不重排不改写）；
//   - 下游响应头：200 + text/event-stream + 白名单过滤（x-request-id 保留 / Set-Cookie 删除）；
//   - 上游请求：入站 Authorization/Cookie/X-Api-Key 不外泄，认证头替换为账号 api_key，
//     仅 user-agent / accept-language 走白名单透传。
func TestOpenAIRawChatCompletions_Characterization_StreamFrameBytesAndSensitiveHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	// 入站敏感头：绝不能到达上游
	c.Request.Header.Set("Authorization", "Bearer inbound-client-key")
	c.Request.Header.Set("Cookie", "session=secret")
	c.Request.Header.Set("X-Api-Key", "inbound-api-key")
	c.Request.Header.Set("Proxy-Authorization", "Basic abc")
	c.Request.Header.Set("X-Custom-Client", "leak")
	// 白名单头：应透传
	c.Request.Header.Set("User-Agent", "client-ua/1.0")
	c.Request.Header.Set("Accept-Language", "zh-CN")

	chunk1 := `data: {"id":"chatcmpl_lock","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"content":"hel"}}]}`
	chunk2 := `data: {"id":"chatcmpl_lock","object":"chat.completion.chunk","model":"gpt-test","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":3}}}`
	upstreamSSE := chunk1 + "\n\n" + chunk2 + "\n\n" + "data: [DONE]\n"

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"rid-raw-stream-lock"},
			"Set-Cookie":   []string{"upstream=secret"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamSSE)),
	}}

	cfg := rawChatCompletionsTestConfig()
	svc := &OpenAIGatewayService{
		cfg:                  cfg,
		httpUpstream:         upstream,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
	}
	account := rawChatCompletionsTestAccount()

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)

	// 下游 SSE 帧序列逐字节等于上游（行扫描按 "\n" 回写，帧结构与顺序不变）
	require.Equal(t, upstreamSSE, rec.Body.String(), "流式下游 body 应与上游 SSE 字节序列完全一致")

	// 下游响应头
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
	require.Equal(t, "rid-raw-stream-lock", rec.Header().Get("X-Request-Id"), "x-request-id 在默认白名单内应透传")
	require.Empty(t, rec.Header().Get("Set-Cookie"), "Set-Cookie 应被响应头白名单过滤")

	// usage 提取（手工推导：prompt=9 completion=4 cached=3）
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
	require.Equal(t, "rid-raw-stream-lock", result.RequestID)

	// I-1.6 上游请求头：认证替换 + 敏感头清除 + 白名单透传
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"), "上游认证头应替换为账号 api_key")
	require.Empty(t, upstream.lastReq.Header.Get("Cookie"))
	require.Empty(t, upstream.lastReq.Header.Get("X-Api-Key"))
	require.Empty(t, upstream.lastReq.Header.Get("Proxy-Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("X-Custom-Client"), "非白名单客户端头不应透传")
	require.Equal(t, "client-ua/1.0", upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "zh-CN", upstream.lastReq.Header.Get("Accept-Language"))
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	// 流式请求会强制打开 include_usage（既有行为，顺带锁定）
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
}

// I-1.2: raw CC 非流式透传——下游 body 与上游 JSON 逐字节一致（不重新序列化，
// 保留原始空白/未知字段），状态码 200，Content-Type 原样透传，header 走白名单过滤。
func TestOpenAIRawChatCompletions_Characterization_NonStreamByteForByteBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer inbound-client-key")
	c.Request.Header.Set("Cookie", "session=secret")

	// 故意包含双空格与未知字段：逐字节透传下必须原样保留
	upstreamJSON := `{"id":"chatcmpl_bytes","object":"chat.completion",  "unknown_field":{"nested":[1,2,3]},"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9,"prompt_tokens_details":{"cached_tokens":1}}}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json; charset=utf-8"},
			"X-Request-Id": []string{"rid-raw-nonstream-lock"},
			"Set-Cookie":   []string{"upstream=secret"},
			"X-Upstream":   []string{"leak"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamJSON)),
	}}

	cfg := rawChatCompletionsTestConfig()
	svc := &OpenAIGatewayService{
		cfg:                  cfg,
		httpUpstream:         upstream,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
	}
	account := rawChatCompletionsTestAccount()

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, upstreamJSON, rec.Body.String(), "非流式下游 body 应与上游逐字节一致")
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"), "Content-Type 应原样透传")
	require.Equal(t, "rid-raw-nonstream-lock", rec.Header().Get("X-Request-Id"))
	require.Empty(t, rec.Header().Get("Set-Cookie"))
	require.Empty(t, rec.Header().Get("X-Upstream"), "非白名单上游头不应透传给客户端")

	// usage 手工推导：prompt=7 completion=2 cached=1
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)

	// I-1.6: 入站敏感头不外泄 + 认证替换（非流式 Accept 为 application/json）
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("Cookie"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Accept"))
	// 请求体未做协议转换（模型未映射时原样直转）
	require.Equal(t, "gpt-test", gjson.GetBytes(upstream.lastBody, "model").String())
}

// I-1.2 怪癖锁定：raw CC 非流式路径对 <400 的非 200 上游状态码（如 201/206）
// 一律以 200 回写下游——bufferRawChatCompletions 固定 WriteHeader(http.StatusOK)。
// 这是当前实际行为；如未来改为透传原状态码，属外部可观测行为变化，须显式过此测试。
func TestOpenAIRawChatCompletions_Characterization_NonStream2xxNormalizedTo200(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamJSON := `{"id":"chatcmpl_201","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusCreated, // 201
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(upstreamJSON)),
	}}

	cfg := rawChatCompletionsTestConfig()
	svc := &OpenAIGatewayService{
		cfg:          cfg,
		httpUpstream: upstream,
	}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code, "上游 201 当前被归一为 200 回写（怪癖锁定）")
	require.Equal(t, upstreamJSON, rec.Body.String())
}
