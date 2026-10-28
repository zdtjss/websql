package agent

// 安全指标：进程内原子计数器 + JSON 快照端点。
//
// 低基数约束：label 只允许 stage/action/result/component/policy/reason 等代码内常量，
// 禁止把 userId、connId、table、column、sessionId 作为 label。

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// 安全阶段
const (
	StageInput    = "input"
	StageContext  = "context"
	StageScope    = "scope"
	StageTool     = "tool"
	StageOutput   = "output"
	StagePersist  = "persist"
	StageExport   = "export"
	StageRevision = "revision"
	StageRollout  = "rollout"
)

var securityCounters sync.Map // string -> *atomic.Int64

// securityDurations 记录各阶段耗时（仅聚合，不含任何资源标识）。
type securityDuration struct {
	sumNanos atomic.Int64
	count    atomic.Int64
}

var securityDurations sync.Map // stage -> *securityDuration

// recordSecurityDuration 记录一次安全阶段耗时。
func recordSecurityDuration(stage string, d time.Duration) {
	if d < 0 {
		return
	}
	value, ok := securityDurations.Load(stage)
	if !ok {
		actual, _ := securityDurations.LoadOrStore(stage, &securityDuration{})
		value = actual
	}
	stat := value.(*securityDuration)
	stat.sumNanos.Add(d.Nanoseconds())
	stat.count.Add(1)
}

func incSecurityCounter(key string) {
	if v, ok := securityCounters.Load(key); ok {
		v.(*atomic.Int64).Add(1)
		return
	}
	counter := &atomic.Int64{}
	counter.Add(1)
	actual, _ := securityCounters.LoadOrStore(key, counter)
	actual.(*atomic.Int64).Add(1)
}

// recordSecurityEvent 记录 ai_security_check_total{stage,action,result}。
func recordSecurityEvent(stage, action, result string) {
	incSecurityCounter("ai_security_check_total|" + stage + "|" + action + "|" + result)
}

// recordMaskEvent 记录 ai_security_mask_total{stage,action}。
func recordMaskEvent(stage, action string) {
	incSecurityCounter("ai_security_mask_total|" + stage + "|" + action)
}

// recordQuarantine 记录 ai_security_context_quarantined_total{reason}。
func recordQuarantine(reason string) {
	incSecurityCounter("ai_security_context_quarantined_total|" + reason)
}

// recordDegraded 记录 ai_security_degraded_total{component,policy}。
func recordDegraded(component, policy string) {
	incSecurityCounter("ai_security_degraded_total|" + component + "|" + policy)
}

// recordAuditDropped 记录 ai_security_audit_dropped_total{risk}。
func recordAuditDropped(risk string) {
	incSecurityCounter("ai_security_audit_dropped_total|" + risk)
}

// SecurityMetricsSnapshot 返回当前安全指标快照（name → value）。
func SecurityMetricsSnapshot() map[string]int64 {
	out := make(map[string]int64)
	securityCounters.Range(func(k, v any) bool {
		out[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

// SecurityDurationSnapshot 返回各阶段的平均耗时（毫秒），只含聚合值。
func SecurityDurationSnapshot() map[string]float64 {
	out := make(map[string]float64)
	securityDurations.Range(func(k, v any) bool {
		stat := v.(*securityDuration)
		count := stat.count.Load()
		if count <= 0 {
			return true
		}
		out[k.(string)] = float64(stat.sumNanos.Load()) / float64(count) / float64(time.Millisecond)
		return true
	})
	return out
}

// ResetSecurityMetrics 清空指标（仅用于测试）。
func ResetSecurityMetrics() {
	securityCounters.Range(func(k, _ any) bool {
		securityCounters.Delete(k)
		return true
	})
	securityDurations.Range(func(k, _ any) bool {
		securityDurations.Delete(k)
		return true
	})
}

// GetAISecurityMetrics 返回 AI 安全指标快照。
//
// 仅暴露聚合计数与阶段平均耗时，不含用户、连接、表、列与会话标识。
func GetAISecurityMetrics(c *gin.Context) {
	snapshot := SecurityMetricsSnapshot()
	keys := make([]string, 0, len(snapshot))
	for k := range snapshot {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	durations := make(map[string]float64)
	for stage, avg := range SecurityDurationSnapshot() {
		durations["ai_security_check_duration_ms|"+stage+"|avg"] = avg
	}
	for k, v := range durations {
		snapshot[k] = int64(v)
		keys = append(keys, k)
	}
	sort.Strings(keys)

	response.WriteOK(c, gin.H{"series": snapshot, "keys": keys, "durationMsAvg": durations, "durationKeys": durationKeys(durations)})
}

func durationKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
