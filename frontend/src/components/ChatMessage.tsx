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
}

function ChatMessage({ message }: ChatMessageProps) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div className={`max-w-[80%] px-4 py-3 rounded-2xl ${
        isUser 
          ? 'bg-gradient-to-r from-purple-600 to-blue-600 text-white rounded-br-md' 
          : 'bg-gray-800/80 text-gray-100 rounded-bl-md'
      }`}>
        <div className={`text-xs mb-1 ${isUser ? 'text-white/70' : 'text-gray-500'}`}>
          {isUser ? '您' : 'AI 助手'}
        </div>
        <p className="text-sm leading-relaxed whitespace-pre-wrap">{message.content}</p>
        {!isUser && message.totalTokens !== undefined && (
          <div className="mt-2 pt-2 border-t border-gray-700">
            <div className="flex gap-4 text-xs text-gray-500">
              <span>输入: {message.promptTokens} tokens</span>
              <span>输出: {message.completionTokens} tokens</span>
              <span>总计: {message.totalTokens} tokens</span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default ChatMessage
