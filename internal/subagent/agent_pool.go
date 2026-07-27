package subagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/good-fish-man/agent-runtime/log"
)

// AgentResult 单个 Agent 的结果
type AgentResult struct {
	TaskID   string
	AgentID  string
	Output   string
	Error    string
	Duration time.Duration
}

// PoolConfig 代理池配置
type PoolConfig struct {
	MaxConcurrent int           // 最大并发数，0 表示无限制
	Timeout       time.Duration // 单个任务超时
	PoolTimeout   time.Duration // 池级别超时
}

// AgentPool 子代理池 - 支持真正的并行执行
type AgentPool struct {
	config  PoolConfig
	manager *SubAgentManager
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// 运行中的 agent
	agents  map[string]*SubAgent
	agentMu sync.RWMutex
}

// NewAgentPool 创建代理池
func NewAgentPool(manager *SubAgentManager, config PoolConfig) *AgentPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &AgentPool{
		config:  config,
		manager: manager,
		ctx:     ctx,
		cancel:  cancel,
		agents:  make(map[string]*SubAgent),
	}
}

// Spawn 并行启动多个任务
func (p *AgentPool) Spawn(tasks map[string]string) []*AgentResult {
	results := make([]*AgentResult, 0, len(tasks))
	resultMap := make(map[string]*AgentResult)

	// 每次 Spawn 使用局部 results channel，避免复用已关闭的 channel 导致 panic。
	localResults := make(chan *AgentResult, len(tasks))

	// 信号量控制并发上限；MaxConcurrent<=0 表示不限制。
	var semaphore chan struct{}
	if p.config.MaxConcurrent > 0 {
		semaphore = make(chan struct{}, p.config.MaxConcurrent)
	}

	for agentID, task := range tasks {
		p.wg.Add(1)
		aid, t := agentID, task
		log.Go(func() {
			defer p.wg.Done()
			if semaphore != nil {
				semaphore <- struct{}{}        // 获取信号量
				defer func() { <-semaphore }() // 释放信号量
			}

			result := p.runTask(aid, t)
			localResults <- result
		})
	}

	// 等待所有任务完成或超时
	done := make(chan struct{})
	log.Go(func() {
		p.wg.Wait()
		close(done)
	})

	if p.config.PoolTimeout > 0 {
		select {
		case <-done:
			// 所有任务完成
		case <-time.After(p.config.PoolTimeout):
			// 超时，取消所有任务
			p.cancel()
			<-done // 等待被取消的 goroutine 退出，确保写入完成
		}
	} else {
		// 未设置池级超时：等待全部任务完成。
		<-done
	}

	// 收集结果
	close(localResults)
	for result := range localResults {
		resultMap[result.TaskID] = result
	}

	// 按原始顺序返回结果
	for taskID := range tasks {
		if r, ok := resultMap[taskID]; ok {
			results = append(results, r)
		}
	}

	return results
}

// runTask 运行单个任务
func (p *AgentPool) runTask(agentID, task string) *AgentResult {
	taskID := p.manager.NextTaskID()
	start := time.Now()

	result := &AgentResult{
		TaskID:  taskID,
		AgentID: agentID,
	}

	// 创建子 context 用于超时控制
	taskCtx := p.ctx
	if p.config.Timeout > 0 {
		var cancel context.CancelFunc
		taskCtx, cancel = context.WithTimeout(p.ctx, p.config.Timeout)
		defer cancel()
	}

	// 创建 agent
	agent, err := p.manager.Create(taskCtx, agentID)
	if err != nil {
		result.Error = fmt.Sprintf("create agent failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// 存储 agent
	p.agentMu.Lock()
	p.agents[taskID] = agent
	p.agentMu.Unlock()

	// 运行 agent
	err = agent.Run(taskCtx, task)
	if err != nil {
		result.Error = fmt.Sprintf("run failed: %v", err)
	}

	// 获取结果
	agentResult := agent.GetResult()
	if agentResult != nil {
		result.Output = agentResult.Output
		if agentResult.Error != "" {
			result.Error = agentResult.Error
		}
	}
	result.Duration = time.Since(start)

	// 清理
	p.agentMu.Lock()
	delete(p.agents, taskID)
	p.agentMu.Unlock()
	p.manager.Cleanup(agentID)

	return result
}

// Cancel 取消所有运行中的任务
func (p *AgentPool) Cancel() {
	p.cancel()
}

// Wait 等待所有任务完成
func (p *AgentPool) Wait() {
	p.wg.Wait()
}

// GetRunningAgents 获取运行中的 agent 列表
func (p *AgentPool) GetRunningAgents() []*SubAgent {
	p.agentMu.RLock()
	defer p.agentMu.RUnlock()

	agents := make([]*SubAgent, 0, len(p.agents))
	for _, a := range p.agents {
		agents = append(agents, a)
	}
	return agents
}

// ============ 改进的 SubAgent 等待机制 ============

// ResultChannel 返回一个 channel，当 agent 完成时发送结果
func (a *SubAgent) ResultChannel() <-chan *SubAgentResult {
	ch := make(chan *SubAgentResult, 1)
	log.Go(func() {
		// 等待直到有结果或被取消
		for {
			result := a.GetResult()
			if result != nil {
				ch <- result
				return
			}
			// 添加小延迟避免 CPU 忙等待
			time.Sleep(10 * time.Millisecond)
		}
	})
	return ch
}
