interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  promptTokens?: number
  completionTokens?: number
  totalTokens?: number
}

interface ChatMessageProps {
  message: Message
  onDelete?: () => void
}

function ChatMessage({ message, onDelete }: ChatMessageProps) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
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
    </div>
  )
}

export default ChatMessage
