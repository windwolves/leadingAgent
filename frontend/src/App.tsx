import { useState, useEffect, useRef } from 'react'
import ChatInput from './components/ChatInput'
import ChatMessage from './components/ChatMessage'

const DEFAULT_USER_ID = 'leading'

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  thinking?: string
  promptTokens?: number
  completionTokens?: number
  totalTokens?: number
  timestamp?: number
}

interface SessionSummary {
  id: string
  user_id: string
  state: string
  updated_at: string
  created_at: string
  preview: string
}

// Matches agent.StreamEvent from the backend
interface StreamEvent {
  type: 'thinking' | 'reasoning' | 'text_delta' | 'tool_call' | 'tool_result' | 'done' | 'error'
  content?: string
  tool?: string
  input?: Record<string, unknown>
  result?: string
  turns?: number
  usage?: { promptTokens: number; completionTokens: number; totalTokens: number }
}

function App() {
  const [userId] = useState<string>(DEFAULT_USER_ID)
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string>('')
  const [messages, setMessages] = useState<Message[]>([])
  const [inputValue, setInputValue] = useState<string>('')
  const [isLoading, setIsLoading] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  // 标记是否应自动跟随滚动；用户滚走时置 false，滚回底部时恢复 true
  const shouldAutoScrollRef = useRef(true)
  const didInit = useRef(false)
  const abortRef = useRef<AbortController | null>(null)
  const loadingRef = useRef(false)

  useEffect(() => {
    if (didInit.current) return
    didInit.current = true
    loadSessions().then((first) => {
      if (first) {
        setActiveSessionId(first)
        loadMessages(first)
      }
    })
  }, [])

  // 智能自动滚动：用户未操作时跟随 streaming 底部，用户滚走后不再跟随
  useEffect(() => {
    if (shouldAutoScrollRef.current) {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [messages])

  // 监听用户滚动操作，判断是否还在底部附近（阈值 80px）
  useEffect(() => {
    const el = scrollContainerRef.current
    if (!el) return
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 80
      shouldAutoScrollRef.current = isNearBottom
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [])

  // 组件卸载时取消正在进行的请求
  useEffect(() => {
    return () => {
      if (abortRef.current) {
        abortRef.current.abort()
      }
    }
  }, [])

  async function loadSessions(): Promise<string | null> {
    try {
      const res = await fetch(`/api/sessions?userId=${encodeURIComponent(userId)}`, {
        method: 'GET',
      })
      if (!res.ok) return null
      const data: SessionSummary[] = await res.json()
      setSessions(data || [])
      if (data && data.length > 0) return data[0].id
      return null
    } catch {
      return null
    }
  }

  async function loadMessages(sessionId: string) {
    try {
      const res = await fetch(
        `/api/sessions/messages?sessionId=${encodeURIComponent(sessionId)}&userId=${encodeURIComponent(userId)}`,
        { method: 'GET' },
      )
      if (!res.ok) {
        setMessages([])
        return
      }
      const data = await res.json()
      const out: Message[] = (data || []).map((m: { id: string; role: string; content: string; created_at?: string; reasoning?: string }, i: number) => ({
        id: m.id || `m-${sessionId}-${i}`,
        role: m.role === 'user' ? 'user' : 'assistant',
        content: m.content ?? '',
        thinking: m.reasoning || undefined,
        timestamp: m.created_at ? new Date(m.created_at).getTime() : undefined,
      }))
      setMessages(out)
    } catch {
      setMessages([])
    }
  }

  async function handleSelectSession(sessionId: string) {
    setActiveSessionId(sessionId)
    setMessages([])
    await loadMessages(sessionId)
  }

  function handleNewSession() {
    setActiveSessionId('')
    setMessages([])
  }

  async function handleDeleteSession(sessionId: string, event: React.MouseEvent) {
    event.stopPropagation()
    event.preventDefault()
    // 乐观删除：先更新 UI，再发请求
    setSessions((prev) => prev.filter((s) => s.id !== sessionId))
    if (activeSessionId === sessionId) {
      setActiveSessionId('')
      setMessages([])
    }
    try {
      const ctl = new AbortController()
      const t = setTimeout(() => ctl.abort(), 5000)
      await fetch('/api/sessions', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, userId }),
        signal: ctl.signal,
      })
      clearTimeout(t)
    } catch {
      // ignore network errors; UI already reflects deletion
    }
  }

  // 成对删除：
  //   点击用户消息 → 删除该用户消息 + 紧随的 AI 回复；
  //   点击 AI 回复  → 删除前一条用户消息 + 该 AI 回复；
  //   并把用户消息的内容回填到输入框，方便用户重新编辑。
  async function handleDeleteMessage(clickedIndex: number) {
    if (!activeSessionId) return
    if (clickedIndex < 0 || clickedIndex >= messages.length) return

    const clicked = messages[clickedIndex]
    let userIndex = -1
    let assistantIndex = -1

    if (clicked.role === 'user') {
      userIndex = clickedIndex
      // 查找下一条 AI 回复（中间可能有连续 user 消息，虽然这里通常是交替的，先取紧随其后的那条）
      if (clickedIndex + 1 < messages.length && messages[clickedIndex + 1].role === 'assistant') {
        assistantIndex = clickedIndex + 1
      }
    } else {
      // 点击的是 assistant：找前一条 user 消息（通常就是前一条）
      assistantIndex = clickedIndex
      if (clickedIndex - 1 >= 0 && messages[clickedIndex - 1].role === 'user') {
        userIndex = clickedIndex - 1
      }
    }

    const indexesToDelete: number[] = []
    if (userIndex >= 0) indexesToDelete.push(userIndex)
    if (assistantIndex >= 0) indexesToDelete.push(assistantIndex)
    if (indexesToDelete.length === 0) return

    // 保存删除操作前的输入框内容，请求失败时恢复（而不是粗暴清空）
    const originalInput = inputValue
    const userQueryContent = userIndex >= 0 ? messages[userIndex].content : ''

    // 1) 乐观更新前端 UI
    const deleteSet = new Set(indexesToDelete)
    setMessages((prev) => prev.filter((_, i) => !deleteSet.has(i)))

    // 2) 把用户的 query 回填到输入框（以便用户重新编辑 / 再发送）
    setInputValue(userQueryContent)

    // 3) 向后端发批量删除
    try {
      const ctl = new AbortController()
      const t = setTimeout(() => ctl.abort(), 5000)
      await fetch('/api/sessions/messages', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: activeSessionId, userId, indexes: indexesToDelete }),
        signal: ctl.signal,
      })
      clearTimeout(t)
      await loadSessions()
    } catch {
      // 请求失败：恢复删除前输入框内容，重新拉取消息
      setInputValue(originalInput)
      await loadMessages(activeSessionId)
    }
  }

  async function handleSendMessage(content: string) {
    // 用同步 ref 防重复提交，避免 React 状态更新的异步延迟问题
    if (!content.trim() || loadingRef.current) return
    loadingRef.current = true
    shouldAutoScrollRef.current = true

    // 取消上一次未完成的流式请求
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    const abortController = new AbortController()
    abortRef.current = abortController

    const now = Date.now()
    const userMessage: Message = {
      id: `u-${now}`,
      role: 'user',
      content: content.trim(),
      timestamp: now,
    }
    setMessages((prev) => [...prev, userMessage])
    setIsLoading(true)

    const assistantId = `a-${now}`
    const assistantTimestamp = Date.now()
    let accumulatedContent = ''
    let accumulatedThinking = ''

    const sessionIdToSend = activeSessionId

    try {
      const resp = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: content.trim(),
          sessionId: sessionIdToSend,
          userId,
        }),
        signal: abortController.signal,
      })

      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`)
      }

      const reader = resp.body?.getReader()
      if (!reader) throw new Error('No response body')

      const decoder = new TextDecoder()
      let buffer = ''
      let receivedSessionId = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })

        const lines = buffer.split('\n\n')
        buffer = lines.pop() || ''

        for (const line of lines) {
          const dataLine = line.trim()
          if (!dataLine.startsWith('data: ')) continue
          const jsonStr = dataLine.slice(6)
          try {
            const evt: StreamEvent & { sessionId?: string } = JSON.parse(jsonStr)
            if (evt.sessionId && !receivedSessionId) {
              receivedSessionId = evt.sessionId
            }

            switch (evt.type) {
              case 'thinking':
                setMessages((prev) => {
                  const found = prev.find((m) => m.id === assistantId)
                  if (found) return prev
                  return [
                    ...prev,
                    {
                      id: assistantId,
                      role: 'assistant' as const,
                      content: '思考中...' + (evt.turns ? ` (第${evt.turns}轮)` : ''),
                      timestamp: assistantTimestamp,
                    },
                  ]
                })
                break
              case 'reasoning':
                if (evt.content) {
                  accumulatedThinking += evt.content
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantId ? { ...m, thinking: accumulatedThinking } : m,
                    ),
                  )
                }
                break
              case 'text_delta':
                if (evt.content) {
                  accumulatedContent += evt.content
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantId ? { ...m, content: accumulatedContent } : m,
                    ),
                  )
                }
                break
              case 'tool_call':
                if (evt.tool) {
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantId
                        ? { ...m, content: `正在调用工具: ${evt.tool}...` }
                        : m,
                    ),
                  )
                }
                break
              case 'done': {
                const finalText = evt.content && evt.content.trim() !== '' ? evt.content : accumulatedContent
                if (finalText && finalText.trim() !== '') {
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantId
                        ? {
                            ...m,
                            content: finalText,
                            ...(evt.usage && {
                              promptTokens: evt.usage.promptTokens,
                              completionTokens: evt.usage.completionTokens,
                              totalTokens: evt.usage.totalTokens,
                            }),
                          }
                        : m,
                    ),
                  )
                }
                break
              }
              case 'error':
                setMessages((prev) =>
                  prev.map((m) =>
                    m.id === assistantId ? { ...m, content: `错误: ${evt.content}` } : m,
                  ),
                )
                break
            }
          } catch {
            // skip malformed line
          }
        }
      }

      // 流式完成后：如果 assistant 消息没有有效内容，清理 "思考中..." 占位。
      if (accumulatedContent.trim() === '') {
        setMessages((prev) => prev.filter((m) => m.id !== assistantId))
      }

      // 后端返回的 sessionId 与当前不一致时（旧 session 过期/不存在，后端创建了新 session），
      // 统一用后端返回的 id 作为 activeSessionId，并重新加载该会话的完整历史。
      if (receivedSessionId && receivedSessionId !== activeSessionId) {
        setActiveSessionId(receivedSessionId)
        await loadMessages(receivedSessionId)
      }

      // 请求正常完成后立即清除 abortRef，避免下一次调用时误 abort 已完成的请求
      abortRef.current = null
      setInputValue('')
      await loadSessions()
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        // 请求被取消（用户发新消息或组件卸载），静默处理
        return
      }
      const errMsg = error instanceof Error ? error.message : '网络错误'
      setMessages((prev) => [...prev, { id: assistantId, role: 'assistant', content: errMsg, timestamp: assistantTimestamp }])
    } finally {
      abortRef.current = null
      loadingRef.current = false
      setIsLoading(false)
    }
  }

  return (
    <div className="min-h-screen h-screen flex bg-slate-950 text-slate-100">
      {/* 左侧会话列表 */}
      <aside className="w-72 flex flex-col bg-slate-900 border-r border-white/10">
        <div className="px-4 py-4 border-b border-white/10">
          <div className="text-xs uppercase tracking-wider text-slate-400">User</div>
          <div className="mt-1 text-sm font-medium truncate">{userId}</div>
        </div>
        <div className="p-3">
          <button
          type="button"
          onClick={handleNewSession}
          className="w-full rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm font-medium py-2 transition-colors"
        >
          + 新建会话
        </button>
        </div>
        <div className="flex-1 overflow-y-auto">
          {sessions.length === 0 ? (
            <div className="p-4 text-sm text-slate-400">暂无会话，发送第一条消息即可创建。</div>
          ) : (
            <ul className="space-y-1 px-2 pb-4">
              {sessions.map((s) => (
              <li key={s.id}>
                <button
                  type="button"
                  onClick={() => handleSelectSession(s.id)}
                  className={
                    'group w-full text-left px-3 py-2 rounded-md transition-colors ' +
                    (activeSessionId === s.id
                      ? 'bg-white/10 text-white'
                      : 'hover:bg-white/5 text-slate-200')
                  }
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-sm font-medium truncate">
                      {s.preview ? s.preview : '(空会话)'}
                    </div>
                    <span
                      role="button"
                      tabIndex={0}
                      onClick={(e) => handleDeleteSession(s.id, e)}
                      className="opacity-0 group-hover:opacity-100 text-slate-400 hover:text-red-400 text-xs cursor-pointer"
                    >
                      删除
                    </span>
                  </div>
                  <div className="text-xs text-slate-500 mt-1">
                    {new Date(s.updated_at).toLocaleString()}
                  </div>
                </button>
              </li>
            ))}
            </ul>
          )}
        </div>
      </aside>

      {/* 右侧对话主区域 */}
      <main className="flex-1 flex flex-col">
        <header className="bg-slate-900/60 backdrop-blur border-b border-white/10 px-6 py-4">
          <h1 className="text-lg font-semibold">AI 助手</h1>
          <p className="text-sm text-slate-400 mt-0.5">
            基于 ReAct Agent，支持工具调用与多轮上下文
          </p>
        </header>

        <div ref={scrollContainerRef} className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
          {messages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-slate-500">
              <div className="text-5xl mb-4">🤖</div>
              <p className="text-lg">欢迎使用 AI 助手</p>
              <p className="text-sm mt-2">
                {activeSessionId ? '在当前会话继续聊天' : '输入问题开始新对话'}
              </p>
            </div>
          ) : (
            messages.map((m, idx) => (
              <ChatMessage
                key={m.id}
                message={m}
                onDelete={activeSessionId ? () => handleDeleteMessage(idx) : undefined}
              />
            ))
          )}
          {isLoading && (
            <div className="flex justify-center">
              <div className="w-5 h-5 border-2 border-white/30 border-t-white rounded-full animate-spin" />
            </div>
          )}
          <div ref={messagesEndRef} />
        </div>

        <footer className="bg-slate-900 border-t border-white/10 px-6 py-4">
          <ChatInput onSend={handleSendMessage} disabled={isLoading} value={inputValue} onValueChange={setInputValue} />
        </footer>
      </main>
    </div>
  )
}

export default App
