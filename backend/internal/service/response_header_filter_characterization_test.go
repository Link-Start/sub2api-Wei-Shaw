//go:build unit

package service

// TASK-003 特征化测试：响应 header 白名单过滤语义（INVARIANTS I-1.4）。
//
// 锁定对象：service/response_header_filter.go（compileResponseHeaderFilter 薄封装）
// 及其底层 internal/util/responseheaders 的当前行为，包括怪癖。
// 所有期望值均从当前代码手工推导写死，不从被测函数输出反填。

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/stretchr/testify/require"
)

// I-1.4: cfg 为 nil 时 compileResponseHeaderFilter 返回 nil；
// FilterHeaders 收到 nil filter 时回退到"默认白名单 + 空强删清单"。
func TestResponseHeaderFilter_Characterization_NilConfigFallsBackToDefaultAllowlist(t *testing.T) {
	require.Nil(t, compileResponseHeaderFilter(nil), "nil 配置应编译为 nil filter")

	src := http.Header{}
	src.Set("Content-Type", "application/json")
	src.Set("Set-Cookie", "session=secret")
	src.Set("X-Custom", "leak")

	filtered := responseheaders.FilterHeaders(src, nil)
	require.Equal(t, "application/json", filtered.Get("Content-Type"), "nil filter 应走默认白名单")
	require.Empty(t, filtered.Get("Set-Cookie"), "Set-Cookie 不在默认白名单，必须被删除")
	require.Empty(t, filtered.Get("X-Custom"))
}

// I-1.4: 默认保留清单的精确成员语义（19 项）。
// 该清单是安全边界：任何增删都属于外部可观测行为变化，必须显式过此测试。
func TestResponseHeaderFilter_Characterization_DefaultAllowlistExactMembership(t *testing.T) {
	// 手工抄自 responseheaders.go defaultAllowed（2026-07-08 实测）
	defaultAllowed := []string{
		"content-type",
		"content-encoding",
		"content-language",
		"cache-control",
		"etag",
		"last-modified",
		"expires",
		"vary",
		"date",
		"x-request-id",
		"x-ratelimit-limit-requests",
		"x-ratelimit-limit-tokens",
		"x-ratelimit-remaining-requests",
		"x-ratelimit-remaining-tokens",
		"x-ratelimit-reset-requests",
		"x-ratelimit-reset-tokens",
		"retry-after",
		"location",
		"www-authenticate",
	}
	// 代表性敏感/非白名单头：必须被删除
	blocked := []string{
		"Set-Cookie",
		"Strict-Transport-Security",
		"X-Custom-Upstream",
		"Anthropic-Ratelimit-Unified-Status",
		"Content-Disposition",
		"Access-Control-Allow-Origin",
		"Server",
	}
	// hop-by-hop：由 HTTP 库管理，始终删除
	hopByHop := []string{"Content-Length", "Transfer-Encoding", "Connection"}

	src := http.Header{}
	for _, key := range defaultAllowed {
		src.Set(key, "v-"+key)
	}
	for _, key := range blocked {
		src.Set(key, "blocked")
	}
	for _, key := range hopByHop {
		src.Set(key, "hop")
	}

	// filter 走 service 层封装：默认配置（Enabled=false）
	filter := compileResponseHeaderFilter(&config.Config{})
	require.NotNil(t, filter)
	filtered := responseheaders.FilterHeaders(src, filter)

	for _, key := range defaultAllowed {
		require.Equal(t, "v-"+key, filtered.Get(key), "默认白名单头 %q 应保留", key)
	}
	for _, key := range blocked {
		require.Empty(t, filtered.Get(key), "非白名单头 %q 应被删除", key)
	}
	for _, key := range hopByHop {
		require.Empty(t, filtered.Get(key), "hop-by-hop 头 %q 应被删除", key)
	}
	// 保留头总数恰为白名单命中数（无额外泄漏）
	require.Len(t, filtered, len(defaultAllowed))
}

// I-1.4: Enabled=false 时 additional_allowed 与 force_remove 均不生效——
// additional_allowed 不放行新头，force_remove 不删除默认白名单头。
func TestResponseHeaderFilter_Characterization_DisabledIgnoresAdditionalAndForceRemove(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.ResponseHeaders = config.ResponseHeaderConfig{
		Enabled:           false,
		AdditionalAllowed: []string{"x-extra"},
		ForceRemove:       []string{"x-request-id"},
	}

	src := http.Header{}
	src.Set("X-Extra", "should-not-pass")
	src.Set("X-Request-Id", "rid-keep")

	filtered := responseheaders.FilterHeaders(src, compileResponseHeaderFilter(cfg))
	require.Empty(t, filtered.Get("X-Extra"), "关闭时 additional_allowed 不应放行额外头")
	require.Equal(t, "rid-keep", filtered.Get("X-Request-Id"), "关闭时 force_remove 不应删除默认白名单头")
}

// I-1.4: Enabled=true 的配置语义：
//   - additional_allowed 扩展白名单，条目做 lower+trim 归一化，空串跳过；
//   - force_remove 优先级最高：既能删默认白名单头，也能压过 additional_allowed 中的同名项。
func TestResponseHeaderFilter_Characterization_EnabledConfigSemantics(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.ResponseHeaders = config.ResponseHeaderConfig{
		Enabled: true,
		// 大小写混合 + 前后空白：应归一化后生效；空串条目应被跳过
		AdditionalAllowed: []string{"  X-Trace-Id  ", "x-both", "   "},
		// force_remove 同时命中：默认白名单头(etag) + additional_allowed 同名项(x-both)
		ForceRemove: []string{"ETag ", "x-both", ""},
	}

	src := http.Header{}
	src.Set("X-Trace-Id", "trace-1")
	src.Set("X-Both", "conflict")
	src.Set("Etag", `"abc"`)
	src.Set("Content-Type", "text/plain")
	src.Set("X-Not-Listed", "drop")

	filtered := responseheaders.FilterHeaders(src, compileResponseHeaderFilter(cfg))
	require.Equal(t, "trace-1", filtered.Get("X-Trace-Id"), "additional_allowed 条目应归一化后放行")
	require.Empty(t, filtered.Get("X-Both"), "force_remove 应压过 additional_allowed 中的同名项")
	require.Empty(t, filtered.Get("Etag"), "force_remove 应能删除默认白名单头")
	require.Equal(t, "text/plain", filtered.Get("Content-Type"))
	require.Empty(t, filtered.Get("X-Not-Listed"))
}

// I-1.4 怪癖锁定：hop-by-hop 头（content-length/transfer-encoding/connection）
// 即使被显式加入 additional_allowed 也不放行——hop-by-hop 检查在白名单命中之后执行。
func TestResponseHeaderFilter_Characterization_HopByHopUnremovableViaAdditionalAllowed(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.ResponseHeaders = config.ResponseHeaderConfig{
		Enabled:           true,
		AdditionalAllowed: []string{"content-length", "transfer-encoding", "connection"},
	}

	src := http.Header{}
	src.Set("Content-Length", "123")
	src.Set("Transfer-Encoding", "chunked")
	src.Set("Connection", "keep-alive")

	filtered := responseheaders.FilterHeaders(src, compileResponseHeaderFilter(cfg))
	require.Empty(t, filtered.Get("Content-Length"), "content-length 即使加入 additional_allowed 也应被删除")
	require.Empty(t, filtered.Get("Transfer-Encoding"))
	require.Empty(t, filtered.Get("Connection"))
	require.Len(t, filtered, 0)
}

// I-1.4: 多值头全部保留（值序不变）；WriteFilteredHeaders 为追加语义，
// 不清空 dst 中已有的同名值。
func TestResponseHeaderFilter_Characterization_MultiValuePreservedAndWriteMergeAppends(t *testing.T) {
	src := http.Header{}
	src.Add("Vary", "Accept")
	src.Add("Vary", "Origin")
	src.Add("Set-Cookie", "a=1")
	src.Add("Set-Cookie", "b=2")

	filter := compileResponseHeaderFilter(&config.Config{})

	filtered := responseheaders.FilterHeaders(src, filter)
	require.Equal(t, []string{"Accept", "Origin"}, filtered.Values("Vary"), "多值头应按原值序全部保留")
	require.Nil(t, filtered.Values("Set-Cookie"), "多值 Set-Cookie 也应全部删除")

	dst := http.Header{}
	dst.Add("Vary", "pre-existing")
	responseheaders.WriteFilteredHeaders(dst, src, filter)
	require.Equal(t, []string{"pre-existing", "Accept", "Origin"}, dst.Values("Vary"),
		"WriteFilteredHeaders 应为追加语义，保留 dst 已有值")
	require.Empty(t, dst.Get("Set-Cookie"))
}
