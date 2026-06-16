import { useState, KeyboardEvent, useEffect } from 'react'

interface ChatInputProps {
  onSend: (content: string) => void
  disabled?: boolean
  value?: string
  onValueChange?: (v: string) => void
}

function ChatInput({ onSend, disabled, value, onValueChange }: ChatInputProps) {
  const [internalInput, setInternalInput] = useState('')

  const input = value !== undefined ? value : internalInput

  // 当外部传入 value 变化时，同步到内部（兜底，value 优先使用外部值）
  useEffect(() => {
    if (value !== undefined) {
      setInternalInput(value)
    }
  }, [value])

  const handleChange = (v: string) => {
    setInternalInput(v)
    onValueChange?.(v)
  }

  const handleSubmit = () => {
    if (input.trim() && !disabled) {
      onSend(input)
      handleChange('')
    }
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  return (
    <div className="relative">
      <textarea
        value={input}
        onChange={(e) => handleChange(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="输入您的问题..."
        disabled={disabled}
        rows={1}
        autoFocus
        className="w-full bg-gray-800/50 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent resize-none transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
        style={{ minHeight: '48px', maxHeight: '120px' }}
      />
      <button
        onClick={handleSubmit}
        disabled={!input.trim() || disabled}
        className="absolute right-2 bottom-2 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-700 hover:to-blue-700 text-white px-4 py-2 rounded-lg font-medium transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
        </svg>
        发送
      </button>
    </div>
  )
}

export default ChatInput
