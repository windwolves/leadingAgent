import { useState, useEffect, useRef } from 'react'
import ChatInput from './components/ChatInput'
import ChatMessage from './components/ChatMessage'

const DEFAULT_USER_ID = 'leading'

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
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
  type: 'thinking' | 'text_delta' | 'tool_call' | 'tool_result' | 'done' | 'error'
  content?: string
  tool?: string
  input?: Record<string, unknown>
  result?: string
  turns?: number
}

function App() {
  const [userId] = useState<string>(DEFAULT_USER_ID)
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string>('')
  const [messages, setMessages] = useState<Message[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const didInit = useRef(false)

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

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, isLoading])

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
      const out: Message[] = (data || []).map((m: { role: string; content: string }, i: number) => ({
        id: `m-${sessionId}-${i}`,
        role: m.role === 'user' ? 'user' : 'assistant',
        content: m.content ?? '',
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
    try {
      await fetch('/api/sessions', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, userId }),
      })
    } catch {
      // ignore failure silently
    }
    const remaining = sessions.filter((s) => s.id !== sessionId)
    setSessions(remaining)
    if (activeSessionId === sessionId) {
      setActiveSessionId('')
      setMessages([])
    }
  }

  async function handleSendMessage(content: string) {
    if (!content.trim() || isLoading) return

    const userMessage: Message = {
      id: `u-${Date.now()}`,
      role: 'user',
      content: content.trim(),
    }
    setMessages((prev) => [...prev, userMessage])
    setIsLoading(true)

    const assistantId = `a-${Date.now()}`
    let accumulatedContent = ''

    // 新会话（activeSessionId 为空）时第一次发消息后端会自动创建 session。
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
                      role: 'assistant',
                      content: '思考中...' + (evt.turns ? ` (第${evt.turns}轮)` : ''),
                    },
                  ]
                })
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
              case 'done':
                if (evt.content) {
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantId ? { ...m, content: evt.content! } : m,
                    ),
                  )
                }
                break
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

      // 新会话：把 activeSessionId 设为后端返回的 id，并刷新列表。
      if (!activeSessionId && receivedSessionId) {
        setActiveSessionId(receivedSessionId)
      }
      await loadSessions()
    } catch (error) {
      const errMsg = error instanceof Error ? error.message : '网络错误'
      setMessages((prev) => [...prev, { id: assistantId, role: 'assistant', content: errMsg }])
    } finally {
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

        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
          {messages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-slate-500">
              <div className="text-5xl mb-4">🤖</div>
              <p className="text-lg">欢迎使用 AI 助手</p>
              <p className="text-sm mt-2">
                {activeSessionId ? '在当前会话继续聊天' : '输入问题开始新对话'}
              </p>
            </div>
          ) : (
            messages.map((m) => <ChatMessage key={m.id} message={m} />)
          )}
          {isLoading && (
            <div className="flex justify-center">
              <div className="w-5 h-5 border-2 border-white/30 border-t-white rounded-full animate-spin" />
            </div>
          )}
          <div ref={messagesEndRef} />
        </div>

        <footer className="bg-slate-900 border-t border-white/10 px-6 py-4">
          <ChatInput onSend={handleSendMessage} disabled={isLoading} />
        </footer>
      </main>
    </div>
  )
}

export default App
