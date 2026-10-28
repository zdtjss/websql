package agent

// 决策面边界：模型生成的分析结论与「产生副作用的决策执行」必须分离。
//
// 规则：
//   - 普通建议只读输出，不触发任何写入；
//   - DecisionIntent 只是不可信的「请求」，不是授权凭证；
//   - 任何副作用必须由服务端 DecisionActionGuard 重新校验当前权限、证据来源、
//     审批要求与幂等键，禁止「模型建议」直接变成数据库写入。

import (
	"errors"
	"fmt"
	"strings"
)

// ErrApprovalRequired 决策执行需要用户显式审批。
var ErrApprovalRequired = errors.New("决策执行需要用户确认")

// DecisionIntent 模型提出的决策执行意图（不可信输入）。
type DecisionIntent struct {
	// ID 本次意图标识（服务端生成或模型提供，仅用于审计关联）
	ID string
	// Action 目标工具名（必须登记为 AI 写能力）
	Action string
	// ConnID/Schema/Table 目标资源
	ConnID string
	Schema string
	Table  string
	// SQL 待执行语句（仅用于资源解析与审计摘要，不作为授权依据）
	SQL string
	// Reason 模型给出的理由（不可信，仅展示）
	Reason string
	// Evidence 结论所依赖的数据来源
	Evidence []ResourceRef
	// IdempotencyKey 幂等键：同一意图重复提交不得重复产生副作用
	IdempotencyKey string
}

// DecisionActionGuard 决策执行的唯一授权入口。
type DecisionActionGuard struct {
	sec *RunSecurityContext
}

// NewDecisionActionGuard 创建决策守卫。
func NewDecisionActionGuard(sec *RunSecurityContext) *DecisionActionGuard {
	return &DecisionActionGuard{sec: sec}
}

// Authorize 校验决策意图是否允许进入执行流程。
//
// 通过校验只表示「可以进入既有工具执行链」，仍需危险 SQL 审批；
// 本函数不执行任何语句。
func (g *DecisionActionGuard) Authorize(intent DecisionIntent) error {
	if g == nil || g.sec == nil {
		return fmt.Errorf("%w: missing run security context", ErrSecurityScope)
	}
	sec := g.sec
	if !sec.DataToolsAllowed() {
		return &PermissionError{Message: "数据访问安全依赖不可用，已拒绝决策执行"}
	}

	action := normalizeToolName(intent.Action)
	policy, ok := lookupToolPolicy(action)
	if !ok {
		return &PermissionError{Message: "决策动作未登记", Objects: []string{intent.Action}}
	}
	if policy.Capability != CapabilityAIWrite {
		return &PermissionError{
			Message: fmt.Sprintf("决策动作 %s 不属于 AI 写能力，不能产生数据副作用", intent.Action),
			Objects: []string{intent.Action},
		}
	}
	if strings.TrimSpace(intent.IdempotencyKey) == "" {
		return errors.New("决策执行缺少幂等键")
	}
	if strings.TrimSpace(intent.Table) == "" {
		return &PermissionError{Message: "决策执行缺少目标表"}
	}

	// 1. 目标资源必须仍被授权（以当前权限重新裁决，不信任意图中的自述）
	if err := sec.Authorize(AccessRequest{
		Action: ActionWrite,
		ConnID: intent.ConnID,
		Schema: intent.Schema,
		Table:  intent.Table,
	}); err != nil {
		recordSecurityEvent(StageTool, "decision", "deny")
		return err
	}

	// 2. 结论所依赖的证据必须仍可读，避免基于已回收数据做写决策
	for _, ref := range intent.Evidence {
		if ref.Table == "" {
			continue
		}
		if err := sec.Authorize(AccessRequest{
			Action: ActionRead,
			ConnID: ref.ConnID,
			Schema: ref.Schema,
			Table:  ref.Table,
			Column: ref.Column,
		}); err != nil {
			return &PermissionError{
				Message: "决策依据的数据已不在授权范围内，已拒绝执行",
				Objects: []string{ref.Table},
			}
		}
	}

	// 3. 需要审批的动作返回统一信号，由既有审批链路处理（不能在此直接放行）
	if policy.RequiresApproval {
		return ErrApprovalRequired
	}
	recordSecurityEvent(StageTool, "decision", "allow")
	return nil
}
