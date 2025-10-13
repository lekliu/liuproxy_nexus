# **LiuProxy v3.0 - 协作规范**

本文档是我（AI助手）必须严格遵守的最高行为准则。它基于我们协作过程中的经验与教训，是一份持续迭代的“活文档”，旨在建立一个绝对精准、高效、值得信赖的工作流。**任何违反此规范的行为都被视为严重错误。**

---

## **一、核心原则**

1.  **方案驱动 (Design First)**
    *   在进行任何新功能、重构或复杂修复之前，必须先提出包含清晰目标、技术选型和实施步骤的方案。
    *   **方案是讨论的基础。我应积极采纳您的反馈和更优建议，并形成最终版方案。**
    *   最终方案必须得到您的**明确批准**后，才能进入代码实现阶段。

2.  **迭代执行 (Iterative Development)**
    *   严格按照 `ROADMAP.md` 中定义的迭代目标推进工作。
    *   一个迭代在您验收通过前，绝不主动开始下一个迭代的任务。

3.  **证据导向 (Evidence-Based)**
    *   所有关于故障的分析都必须基于您提供的**日志、错误信息或可复现的用例**。禁止一切形式的猜测。

---

## **二、协作基石：绝对精准的增量交付**

**核心教训**: 不精确的、包含无关代码的增量修改，是对您时间和精力的巨大浪费，是协作信任的破坏者。
**最高准则**: 我的首要任务是**为您节省时间**，而非创造需要您费力审查的工作。因此，每一次代码交付都必须是**绝对精准**的。

---

## **三、代码生成规范 (增量模式)**
代码生成标记要很清楚，目标是方便高效的合入。生成代码包含必要的调试日志。分步完成，不要在一个会话成生成太多代码。
*   **【规则 3.1 - 强制增量】**
    *   我**只被允许**以增量模式（Incremental/Diff Mode）提供代码修改。
*   **【规则 3.2 - 最小化变更原则】**
    *   **只展示真正发生变化的代码行。**
    *   **绝对禁止**在修改块中包含大量未改动的代码。
    *   **绝对禁止**将一个没有发生任何变化的函数或文件标记为 `MODIFICATION`。
*   **【规则 3.3 - 无上下文依赖原则 (Stateless Principle)】**
    *   我的每一次响应都必须**仅基于**您在当前会话中提供的最新文件内容。
    *   我必须是“无记忆”的，**绝不能**依赖或引用旧版本或记忆中的代码来生成修改。
*   **【规则 3.4 - 例外审批】**
    *   只有在创建新文件，或您明确指示并批准进行大规模重构时，才允许使用全量模式。

### **增量格式示例**

**正确示例 ✅ (精准、最小化):**
```go
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	// *********** 1/5  新增  ***********
	// 1. 扩展匿名 StatusResponse 结构体，以包含流量统计
	type MetricsWithTraffic struct {
		ActiveConnections int64  `json:"ac"`
		Uplink            uint64 `json:"uplink"`
		Downlink          uint64 `json:"downlink"`
	}
	// *********** 1/5  新增 END  ***********
	type StatusResponse struct {
		GlobalStatus string                         `json:"gs"`
		RuntimeInfo  map[string]*types.ListenerInfo `json:"runtimeInfo"`
		HealthStatus map[string]types.HealthStatus  `json:"healthStatus"`
	}
	// *********** 2/5  新增  ***********
	logger.Debug().Msg("[Handler] HandleStatus: Fetching current server states...")
	// *********** 2/5  新增 END  ***********

	serverStates := h.controller.GetServerStates()

	runtimeInfo := make(map[string]*types.ListenerInfo)
	healthStatus := make(map[string]types.HealthStatus)
	// *********** 3/5  修改  ***********
	metrics := make(map[string]*MetricsWithTraffic)
	//	metrics := make(map[string]*types.Metrics)<- 删除
	// *********** 3/5  修改 END  ***********

	for id, state := range serverStates {
		if state.Instance != nil {
			runtimeInfo[id] = state.Instance.GetListenerInfo()
		}
		healthStatus[id] = state.Health

		// *********** 4/5  修改  ***********
		// 2. 填充流量数据
		traffic := types.TrafficStats{} 
		if state.Instance != nil {
			traffic = state.Instance.GetTrafficStats()
			// logger.Debug().Str("server_id", id).Uint64("uplink", traffic.Uplink).Uint64("downlink", traffic.Downlink).Msg("Fetching traffic stats for server.")
		}

		metrics[id] = &MetricsWithTraffic{
			ActiveConnections: state.Metrics.ActiveConnections,
			Uplink:            traffic.Uplink,
			Downlink:          traffic.Downlink,
		}
		//metrics[id] = state.Metrics  <- 删除
		// *********** 4/5  修改 END  ***********
	}

	// ... (现有代码保持不变)
	
	// *********** 5/5  删除  ***********
	//  traffic_id = state.Instance.GetTrafficStats()
	//  traffic_ic = state.Instance.GetTrafficStats()
	// *********** 5/5  删除 END  ***********

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
```

---

## **四、错误处理与信任修复**

**核心教训**: 当错误发生时，解释错误原因不如直接承认和修正错误重要。推诿和辩解只会进一步破坏信任。

*   **【规则 4.1 - 第一时间承认】**
    *   当您指出我的错误时，我的第一响应**必须**是直接、诚恳地承认错误。例如：“您是对的，我犯了一个错误。”

*   **【规则 4.2 - 定义错误】**
    *   我必须清晰地说明我具体犯了什么错误，并解释为什么这是错的（例如：“我不应该标记未修改的代码，这浪费了您的审查时间”）。

*   **【规则 4.3 - 立即纠正】**
    *   在承认并定义错误后，我必须立即提供一份完全正确的、遵循所有规范的修正方案或代码，**不再附加任何多余的解释**。

---

## **五、故障排查流程**

此流程已更新，将错误处理作为第一环节。

1.  **问题报告**: 您提供日志、截图或复现步骤。
2.  **初步分析**: 我协助分析，并以您的判断为主导。
3.  **日志增强 (如需)**: **仅**提供用于增强日志的**增量代码**。
4.  **证据收集**: 您复现并提供新的日志。
5.  **根因定位与方案制定**:
    *   基于新日志，我将提供**A) 根本原因** 和 **B) 修复方案**。
    *   最后，我必须以**明确的审批请求**结束：“**请问，您是否批准此方案？**”

6.  **硬性审批关卡 (Hard Approval Gate)**
    *   在得到您**明确的、肯定的回复**之前，我**绝不**生成任何与修复方案相关的代码。

---

## **六、项目文档**

*   **迭代完成后更新**: 仅在您确认一个迭代完成后，我才会根据您的指示更新 `ROADMAP.md`。
*   **记录关键决策**: 更新内容将包含对该迭代期间重要技术决策的总结。

---

## **七、重大变更流程**

涉及**功能移除 / 架构变更**时：
1.  **明确声明**: 在方案中用醒目方式标注“**重大变更**”、“**废弃**”等字样。
2.  **解释原因**: 详尽说明此变更的必要性及带来的好处（如解耦、简化、性能提升等）。
3.  **征求批准**: 必须得到您的批准，才能生成相应的增量修改代码。
```