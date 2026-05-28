import { useState } from 'react'
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

function App() {
  const [messages, setMessages] = useState<Message[]>([])
  const [isLoading, setIsLoading] = useState(false)

  const handleSendMessage = async (content: string) => {
    if (!content.trim()) return

    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      content: content.trim()
    }

    setMessages(prev => [...prev, userMessage])
    setIsLoading(true)

    try {
      const response = await fetch('/api/stream-chat', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ message: content.trim() })
      })

      if (!response.ok) {
        throw new Error('网络请求失败')
      }

      const reader = response.body?.getReader()
      if (!reader) {
        throw new Error('无法读取响应')
      }

      const decoder = new TextDecoder('utf-8')
      let accumulatedContent = ''
      let assistantMessageId = (Date.now() + 1).toString()
      let promptTokens: number | undefined
      let completionTokens: number | undefined
      let totalTokens: number | undefined

      let isLast = false

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        const chunk = decoder.decode(value, { stream: true })
        const lines = chunk.split('\n\n')
        
        for (const line of lines) {
          if (!line.trim()) continue
          
          try {
            const data = JSON.parse(line)
            
            if (data.response) {
              accumulatedContent += data.response
              
              if (data.prompt_tokens !== undefined) {
                promptTokens = data.prompt_tokens
              }
              if (data.completion_tokens !== undefined) {
                completionTokens = data.completion_tokens
              }
              if (data.total_tokens !== undefined) {
                totalTokens = data.total_tokens
              }

              setMessages(prev => {
                const existingIndex = prev.findIndex(m => m.id === assistantMessageId)
                if (existingIndex >= 0) {
                  const newMessages = [...prev]
                  newMessages[existingIndex] = {
                    id: assistantMessageId,
                    role: 'assistant',
                    content: accumulatedContent,
                    promptTokens,
                    completionTokens,
                    totalTokens
                  }
                  return newMessages
                } else {
                  return [...prev, {
                    id: assistantMessageId,
                    role: 'assistant',
                    content: accumulatedContent,
                    promptTokens,
                    completionTokens,
                    totalTokens
                  }]
                }
              })

              if (data.is_last) {
                isLast = true
              }
            }
          } catch (e) {
            console.warn('解析响应失败:', e)
          }
        }
        
        if (isLast) {
          break
        }
      }
    } catch (error) {
      console.error('流式请求失败:', error)
      const errorMessage: Message = {
        id: (Date.now() + 1).toString(),
        role: 'assistant',
        content: '网络错误，请稍后重试。'
      }
      setMessages(prev => [...prev, errorMessage])
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-purple-900 to-slate-900">
      <div className="max-w-4xl mx-auto h-screen flex flex-col">
        <header className="bg-black/30 backdrop-blur-md border-b border-white/10 px-6 py-4">
          <h1 className="text-xl font-semibold text-white">AI 助手</h1>
          <p className="text-sm text-gray-400 mt-1">输入您的问题，我来帮您解答</p>
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
