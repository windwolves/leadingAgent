import { useState, useRef } from 'react'
import ChatInput from './components/ChatInput'
import ChatMessage from './components/ChatMessage'

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  promptTokens?: number
  completionTokens?: number
  totalTokens?: number
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
  const [messages, setMessages] = useState<Message[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const sessionIdRef = useRef(crypto.randomUUID())

  const handleSendMessage = async (content: string) => {
    if (!content.trim() || isLoading) return

    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      content: content.trim()
    }
    setMessages(prev => [...prev, userMessage])
    setIsLoading(true)

    const assistantId = (Date.now() + 1).toString()
    let accumulatedContent = ''
    let toolCallsInfo: string[] = []

    try {
      const resp = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: content.trim(),
          sessionId: sessionIdRef.current
        })
      })

      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`)
      }

      const reader = resp.body?.getReader()
      if (!reader) throw new Error('No response body')

      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })

        // Parse SSE events: "data: {...}\n\n"
        const lines = buffer.split('\n\n')
        buffer = lines.pop() || ''  // Keep incomplete chunk in buffer

        for (const line of lines) {
          const dataLine = line.trim()
          if (!dataLine.startsWith('data: ')) continue

          const jsonStr = dataLine.slice(6)
          try {
            const evt: StreamEvent = JSON.parse(jsonStr)

            switch (evt.type) {
              case 'thinking':
                // Show thinking indicator
                setMessages(prev => {
                  const found = prev.find(m => m.id === assistantId)
                  if (found) return prev
                  return [...prev, {
                    id: assistantId,
                    role: 'assistant',
                    content: '思考中...' + (evt.turns ? ` (第${evt.turns}轮)` : '')
                  }]
                })
                break

              case 'text_delta':
                if (evt.content) {
                  accumulatedContent += evt.content
                  setMessages(prev =>
                    prev.map(m =>
                      m.id === assistantId
                        ? { ...m, content: accumulatedContent }
                        : m
                    )
                  )
                }
                break

              case 'tool_call':
                if (evt.tool) {
                  toolCallsInfo.push(`调用工具: ${evt.tool}`)
                  setMessages(prev =>
                    prev.map(m =>
                      m.id === assistantId
                        ? { ...m, content: `正在${toolCallsInfo.join(', ')}...` }
                        : m
                    )
                  )
                }
                break

              case 'tool_result':
                if (evt.result) {
                  toolCallsInfo.push('工具执行完成')
                }
                break

              case 'done':
                setMessages(prev =>
                  prev.map(m =>
                    m.id === assistantId
                      ? { ...m, content: evt.content || accumulatedContent }
                      : m
                  )
                )
                break

              case 'error':
                setMessages(prev =>
                  prev.map(m =>
                    m.id === assistantId
                      ? { ...m, content: `错误: ${evt.content}` }
                      : m
                  )
                )
                break
            }
          } catch {
            // Skip unparseable lines
          }
        }
      }
    } catch (error) {
      const errMsg = error instanceof Error ? error.message : '网络错误'
      setMessages(prev => [
        ...prev,
        { id: assistantId, role: 'assistant', content: errMsg }
      ])
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-purple-900 to-slate-900">
      <div className="max-w-4xl mx-auto h-screen flex flex-col">
        <header className="bg-black/30 backdrop-blur-md border-b border-white/10 px-6 py-4">
          <h1 className="text-xl font-semibold text-white">AI 助手</h1>
          <p className="text-sm text-gray-400 mt-1">基于 ReAct Agent，支持工具调用的智能对话</p>
        </header>

        <main className="flex-1 overflow-y-auto p-6 space-y-4">
          {messages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-gray-500">
              <div className="text-6xl mb-4">🤖</div>
              <p className="text-lg">欢迎使用 AI 助手</p>
              <p className="text-sm mt-2">输入问题开始对话</p>
            </div>
          ) : (
            messages.map((message) => (
              <ChatMessage key={message.id} message={message} />
            ))
          )}
          {isLoading && (
            <div className="flex justify-center">
              <div className="w-6 h-6 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
            </div>
          )}
        </main>

        <footer className="bg-black/30 backdrop-blur-md border-t border-white/10 px-6 py-4">
          <ChatInput onSend={handleSendMessage} disabled={isLoading} />
        </footer>
      </div>
    </div>
  )
}

export default App
