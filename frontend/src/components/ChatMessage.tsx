import { useState } from 'react'

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

interface ChatMessageProps {
  message: Message
  onDelete?: () => void
}

function pad2(n: number): string {
  return n.toString().padStart(2, '0')
}

function formatFullTimestamp(ts: number): string {
  const d = new Date(ts)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

function formatRelativeTime(ts: number): string {
  const diff = Date.now() - ts
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return sec <= 0 ? 'just now' : `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min} min${min === 1 ? '' : 's'} ago`
  const hr = Math.floor(min / 60)
  if (hr < 10) return `${hr} hour${hr === 1 ? '' : 's'} ago`
  // >= 10 hours: show short date + time
  const d = new Date(ts)
  return d.toLocaleDateString([], { month: '2-digit', day: '2-digit' }) + ' ' +
    d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function ThinkingBlock({ thinking }: { thinking: string }) {
  const [expanded, setExpanded] = useState(true)
  return (
    <div className="max-w-[80%] mb-2">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-400 mb-1 select-none"
      >
        <span>{expanded ? '▾' : '▸'}</span>
        <span>思考过程</span>
      </button>
      {expanded && (
        <div className="border-l-2 border-slate-600 pl-3 text-[12px] text-slate-500 leading-relaxed whitespace-pre-wrap max-h-48 overflow-y-auto">
          {thinking}
        </div>
      )}
    </div>
  )
}

function ChatMessage({ message, onDelete }: ChatMessageProps) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex flex-col ${isUser ? 'items-end' : 'items-start'}`}>
      {!isUser && message.thinking && <ThinkingBlock thinking={message.thinking} />}
      <div className={`group relative max-w-[80%] px-4 py-3 rounded-2xl ${
        isUser
          ? 'bg-gradient-to-r from-purple-600 to-blue-600 text-white rounded-br-md'
          : 'bg-gray-800/80 text-gray-100 rounded-bl-md'
      }`}>
        <div className={`text-xs mb-1 ${isUser ? 'text-white/70' : 'text-gray-500'}`}>
          {isUser ? '您' : 'AI 助手'}
        </div>
        <p className="text-sm leading-relaxed whitespace-pre-wrap pr-6">{message.content}</p>
        {!isUser && message.totalTokens !== undefined && (
          <div className="mt-2 pt-2 border-t border-gray-700">
            <div className="flex gap-4 text-xs text-gray-500">
              <span>输入: {message.promptTokens} tokens</span>
              <span>输出: {message.completionTokens} tokens</span>
              <span>总计: {message.totalTokens} tokens</span>
            </div>
          </div>
        )}
        {onDelete && (
          <button
            type="button"
            onClick={onDelete}
            title="删除这条消息"
            className={`absolute top-2 right-2 w-5 h-5 flex items-center justify-center rounded-full opacity-0 group-hover:opacity-100 transition-opacity text-xs ${
              isUser
                ? 'text-white/70 hover:text-white hover:bg-white/20'
                : 'text-gray-500 hover:text-red-400 hover:bg-white/10'
            }`}
          >
            ×
          </button>
        )}
      </div>
      {message.timestamp && (
        <span
          title={formatFullTimestamp(message.timestamp)}
          className="mt-1 text-[11px] text-gray-600 select-none"
        >
          {formatRelativeTime(message.timestamp)}
        </span>
      )}
    </div>
  )
}

export default ChatMessage
