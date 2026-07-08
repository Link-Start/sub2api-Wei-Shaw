package pluginhost

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"

	"github.com/gin-gonic/gin"
)

// 插件进程侧的鉴权面前缀，与 pluginkit.Manager 私有子路由器的
// /*path 重写语义严格对齐（phase-2 契约）：
//
//	/api/v1/admin/plugins/:id/api/*path → <socket>/admin/*path
//	/api/v1/plugins/:id/api/*path       → <socket>/user/*path
const (
	proxyAdminPathPrefix = "/admin"
	proxyUserPathPrefix  = "/user"
)

// strippedInboundHeaders 是转发给插件进程前必须删除的入站请求头：
// 插件的身份来自宿主注入的 token，绝不应看到调用者的凭据。
var strippedInboundHeaders = []string{
	"Authorization",
	"Cookie",
	"Proxy-Authorization",
}

// proxyConfig 是构造反代所需的宿主侧配置。
type proxyConfig struct {
	// headerFilter 对插件响应施加与核心网关一致的响应头白名单过滤
	// （phase-4 风险对策 3：防插件泄内部头）；nil 走默认白名单。
	headerFilter *responseheaders.CompiledHeaderFilter
	logger       *slog.Logger
}

// Dispatch 是外部插件的数据面分发器入口：把宿主侧请求经 unix socket
// 反代到插件进程。门控 enabled && healthy；未安装、未启用、进程未就绪
// 一律同一个 404（与 Manager.Dispatch 的防探测语义一致）。
func (s *Supervisor) Dispatch(side pluginkit.Side, id string, c *gin.Context) {
	var prefix string
	switch side {
	case pluginkit.SideAdmin:
		prefix = proxyAdminPathPrefix
	case pluginkit.SideUser:
		prefix = proxyUserPathPrefix
	default:
		response.NotFound(c, "plugin not found")
		return
	}

	pid := pluginkit.ID(id)
	e := s.entryOf(pid)
	if e == nil || !s.states.Enabled(pid) {
		response.NotFound(c, "plugin not found")
		return
	}
	e.mu.Lock()
	proc := e.proc
	e.mu.Unlock()
	if proc == nil || !proc.healthy.Load() {
		response.NotFound(c, "plugin not found")
		return
	}

	rel := c.Param("path") // gin 的 *path 参数自带前导 /；路由无尾段时为空
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	req := c.Request
	origPath, origRawPath := req.URL.Path, req.URL.RawPath
	req.URL.Path = prefix + rel
	req.URL.RawPath = ""
	// 转发后还原，宿主侧后置中间件（日志/指标）看到的仍是原始路径。
	defer func() { req.URL.Path, req.URL.RawPath = origPath, origRawPath }()
	proc.proxy.ServeHTTP(c.Writer, req)
}

// newSocketReverseProxy 构造经 unix socket 拨号的反向代理：
//   - FlushInterval -1：对所有响应立即冲刷，SSE/流式天然可用（phase-4 决策 1）；
//   - ModifyResponse：复用核心网关的响应头白名单过滤；
//   - ErrorHandler：socket 拨号/中途失败统一 502（连接期失败才会走到，
//     已开始写响应体的流中断由 HTTP 库断连兜底）。
func newSocketReverseProxy(id pluginkit.ID, socketPath string, cfg proxyConfig) http.Handler {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// URL.Path 已由 Dispatch 重写为插件相对路径；Host 仅为占位
			//（实际经 unix socket 拨号，不解析主机名）。
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "plugin"
			pr.SetXForwarded()
			// 剥离入站用户凭据：插件经宿主注入的 token 建立身份，绝不应看到
			// 调用者的 JWT/Cookie。宿主鉴权已在分发前完成，转发这些头只会
			// 把终端用户/管理员凭据无谓地扩散进插件进程。
			for _, h := range strippedInboundHeaders {
				pr.Out.Header.Del(h)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header = responseheaders.FilterHeaders(resp.Header, cfg.headerFilter)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			cfg.logger.Error("plugin_proxy_error", "plugin", string(id), "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"code":502,"message":"plugin upstream unavailable"}`))
		},
	}
}
