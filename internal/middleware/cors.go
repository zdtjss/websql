package middleware

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"websql/internal/config"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware 跨域与跨站请求防护。
//
// 安全背景：
//   - 旧实现把任意 Origin 原样回显到 Access-Control-Allow-Origin 并允许携带凭据，
//     任何网页都能跨域读取 API 响应；
//   - 本地/桌面模式下请求不带 token 也会被自动登录为本地用户（拥有全部权限），
//     叠加上一条，用户只要打开一个恶意网页，该网页就能驱动本机 API 执行任意 SQL、读取数据；
//     即使不读取响应，text/plain 的「简单请求」POST 也无需预检即可触发写操作（CSRF）；
//   - DNS 重绑定：攻击者域名解析到 127.0.0.1 后，浏览器视其为同源，可绕过同源策略。
//
// 策略：
//  1. 仅对同源或回环地址来源回显 CORS 头；
//  2. 本地模式拒绝跨站请求（Origin 不可信或 Sec-Fetch-Site=cross-site）；
//  3. 本地模式校验 Host 头只能是 IP / localhost / 单标签主机名 / .local，阻断 DNS 重绑定。
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		trusted := isTrustedOrigin(origin, c.Request.Host)

		if origin != "" && trusted {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Cache-Control", "no-store")

		if config.IsLocalMode() {
			if !isSafeLocalHost(c.Request.Host) {
				log.Printf("[Security] 拒绝可疑 Host（疑似 DNS 重绑定）- host=%s, ip=%s\n", c.Request.Host, c.ClientIP())
				c.Abort()
				response.WriteErr(c, http.StatusForbidden, 403, "非法的访问主机")
				return
			}
			crossSite := c.Request.Header.Get("Sec-Fetch-Site") == "cross-site"
			if (origin != "" && !trusted) || crossSite {
				log.Printf("[Security] 拒绝跨站请求 - origin=%s, host=%s, path=%s\n", origin, c.Request.Host, c.Request.URL.Path)
				c.Abort()
				response.WriteErr(c, http.StatusForbidden, 403, "拒绝跨站请求")
				return
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
	}
}

// isTrustedOrigin 同源，或来源与服务端均为回环地址（兼容 Vite 开发代理 changeOrigin 场景）
func isTrustedOrigin(origin, reqHost string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, reqHost) {
		return true
	}
	return isLoopbackHost(u.Host) && isLoopbackHost(reqHost)
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func isLoopbackHost(hostport string) bool {
	h := strings.ToLower(hostOnly(hostport))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// isSafeLocalHost 本地模式允许的 Host：IP 字面量、localhost、单标签主机名（局域网机器名）、.local（mDNS）。
// 带点的公网域名一律拒绝——DNS 重绑定攻击必然使用攻击者控制的域名。
func isSafeLocalHost(hostport string) bool {
	h := strings.ToLower(hostOnly(hostport))
	if h == "" {
		return true // HTTP/1.0 无 Host 头
	}
	if net.ParseIP(h) != nil || isLoopbackHost(h) {
		return true
	}
	if !strings.Contains(h, ".") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") {
		return true
	}
	return false
}
