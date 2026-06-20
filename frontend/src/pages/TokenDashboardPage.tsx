import { useState, useEffect, useMemo } from 'react'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts'

interface TokenCost {
  id: string
  session_id: string
  provider: string
  model: string
  request_type: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  created_at: string
}

type TimeRange = 7 | 30

type DetailTab = 'session' | 'model'

const cardStyle: React.CSSProperties = {
  border: '1px solid #1e293b',
  borderRadius: '8px',
  padding: '20px',
  background: '#0f172a',
  flex: 1,
}

const sectionStyle: React.CSSProperties = {
  marginBottom: '32px',
}

const tableStyle: React.CSSProperties = {
  width: '100%',
  borderCollapse: 'collapse',
  fontSize: '13px',
}

const thStyle: React.CSSProperties = {
  textAlign: 'left',
  padding: '8px 12px',
  borderBottom: '1px solid #1e293b',
  color: '#94a3b8',
  fontWeight: 500,
}

const tdStyle: React.CSSProperties = {
  padding: '8px 12px',
  borderBottom: '1px solid #0f172a',
  color: '#e2e8f0',
}

function fmt(n: number): string {
  return n.toLocaleString()
}

export default function TokenDashboardPage() {
  const [costs, setCosts] = useState<TokenCost[]>([])
  const [loading, setLoading] = useState(true)
  const [timeRange, setTimeRange] = useState<TimeRange>(7)
  const [activeTab, setActiveTab] = useState<DetailTab>('session')

  useEffect(() => {
    fetch('/api/costs?userId=leading')
      .then((r) => r.json())
      .then((d) => setCosts(d.costs ?? []))
      .catch(() => setCosts([]))
      .finally(() => setLoading(false))
  }, [])

  const totalPrompt = useMemo(() => costs.reduce((s, c) => s + c.prompt_tokens, 0), [costs])
  const totalCompletion = useMemo(() => costs.reduce((s, c) => s + c.completion_tokens, 0), [costs])
  const totalTokens = useMemo(() => costs.reduce((s, c) => s + c.total_tokens, 0), [costs])

  const trendData = useMemo(() => {
    const cutoff = new Date()
    cutoff.setDate(cutoff.getDate() - timeRange)
    const filtered = costs.filter((c) => new Date(c.created_at) >= cutoff)
    const byDay: Record<string, { date: string; prompt: number; completion: number }> = {}
    for (const c of filtered) {
      const day = c.created_at.slice(0, 10)
      if (!byDay[day]) byDay[day] = { date: day, prompt: 0, completion: 0 }
      byDay[day].prompt += c.prompt_tokens
      byDay[day].completion += c.completion_tokens
    }
    return Object.values(byDay).sort((a, b) => a.date.localeCompare(b.date))
  }, [costs, timeRange])

  const sessionData = useMemo(() => {
    const bySession: Record<string, { sessionId: string; count: number; prompt: number; completion: number; total: number; lastAt: string }> = {}
    for (const c of costs) {
      const sid = c.session_id || '(未知)'
      if (!bySession[sid]) bySession[sid] = { sessionId: sid, count: 0, prompt: 0, completion: 0, total: 0, lastAt: '' }
      bySession[sid].count++
      bySession[sid].prompt += c.prompt_tokens
      bySession[sid].completion += c.completion_tokens
      bySession[sid].total += c.total_tokens
      if (!bySession[sid].lastAt || c.created_at > bySession[sid].lastAt) {
        bySession[sid].lastAt = c.created_at
      }
    }
    return Object.values(bySession).sort((a, b) => b.total - a.total)
  }, [costs])

  const modelData = useMemo(() => {
    const byModel: Record<string, { model: string; provider: string; count: number; prompt: number; completion: number; total: number }> = {}
    for (const c of costs) {
      const key = c.model || '(未知)'
      if (!byModel[key]) byModel[key] = { model: key, provider: c.provider, count: 0, prompt: 0, completion: 0, total: 0 }
      byModel[key].count++
      byModel[key].prompt += c.prompt_tokens
      byModel[key].completion += c.completion_tokens
      byModel[key].total += c.total_tokens
    }
    return Object.values(byModel).sort((a, b) => b.total - a.total)
  }, [costs])

  if (loading) {
    return (
      <div style={{ padding: '48px', textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
    )
  }

  return (
    <div style={{ padding: '24px', height: '100%', overflowY: 'auto', background: '#020817', color: '#e2e8f0' }}>
      <h2 style={{ fontSize: '20px', fontWeight: 600, marginBottom: '24px' }}>Token 消耗统计</h2>

      {/* 汇总卡片 */}
      <div style={{ ...sectionStyle, display: 'flex', gap: '16px' }}>
        {[
          { label: '总 Prompt Tokens', value: fmt(totalPrompt) },
          { label: '总 Completion Tokens', value: fmt(totalCompletion) },
          { label: '总 Token 消耗', value: fmt(totalTokens) },
          { label: '请求总次数', value: fmt(costs.length) },
        ].map((card) => (
          <div key={card.label} style={cardStyle}>
            <div style={{ fontSize: '12px', color: '#64748b', marginBottom: '8px' }}>{card.label}</div>
            <div style={{ fontSize: '24px', fontWeight: 700, color: '#818cf8' }}>{card.value}</div>
          </div>
        ))}
      </div>

      {/* 趋势图 */}
      <div style={{ ...sectionStyle, border: '1px solid #1e293b', borderRadius: '8px', padding: '20px', background: '#0f172a' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <div style={{ fontWeight: 600, fontSize: '15px' }}>时间趋势</div>
          <div style={{ display: 'flex', gap: '8px' }}>
            {([7, 30] as TimeRange[]).map((r) => (
              <button
                key={r}
                onClick={() => setTimeRange(r)}
                style={{
                  padding: '4px 12px',
                  borderRadius: '4px',
                  border: '1px solid #1e293b',
                  background: timeRange === r ? '#4f46e5' : 'transparent',
                  color: timeRange === r ? '#fff' : '#94a3b8',
                  cursor: 'pointer',
                  fontSize: '13px',
                }}
              >
                {r === 7 ? '最近 7 天' : '最近 30 天'}
              </button>
            ))}
          </div>
        </div>
        {trendData.length === 0 ? (
          <div style={{ textAlign: 'center', color: '#475569', padding: '40px' }}>暂无数据</div>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <LineChart data={trendData}>
              <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" />
              <XAxis dataKey="date" tick={{ fill: '#64748b', fontSize: 12 }} />
              <YAxis tick={{ fill: '#64748b', fontSize: 12 }} />
              <Tooltip
                contentStyle={{ background: '#0f172a', border: '1px solid #1e293b', color: '#e2e8f0' }}
              />
              <Legend wrapperStyle={{ color: '#94a3b8', fontSize: 13 }} />
              <Line type="monotone" dataKey="prompt" name="Prompt Tokens" stroke="#818cf8" dot={false} strokeWidth={2} />
              <Line type="monotone" dataKey="completion" name="Completion Tokens" stroke="#34d399" dot={false} strokeWidth={2} />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>

      {/* 明细表格 */}
      <div style={{ border: '1px solid #1e293b', borderRadius: '8px', background: '#0f172a', overflow: 'hidden' }}>
        <div style={{ display: 'flex', borderBottom: '1px solid #1e293b' }}>
          {(['session', 'model'] as DetailTab[]).map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              style={{
                padding: '12px 20px',
                border: 'none',
                background: 'transparent',
                color: activeTab === tab ? '#818cf8' : '#64748b',
                fontWeight: activeTab === tab ? 600 : 400,
                borderBottom: activeTab === tab ? '2px solid #818cf8' : '2px solid transparent',
                cursor: 'pointer',
                fontSize: '14px',
              }}
            >
              {tab === 'session' ? '按 Session' : '按模型'}
            </button>
          ))}
        </div>

        <div style={{ overflowX: 'auto' }}>
          {activeTab === 'session' ? (
            sessionData.length === 0 ? (
              <div style={{ textAlign: 'center', color: '#475569', padding: '40px' }}>暂无数据</div>
            ) : (
              <table style={tableStyle}>
                <thead>
                  <tr>
                    <th style={thStyle}>Session ID</th>
                    <th style={thStyle}>请求次数</th>
                    <th style={thStyle}>Prompt Tokens</th>
                    <th style={thStyle}>Completion Tokens</th>
                    <th style={thStyle}>Total Tokens</th>
                    <th style={thStyle}>最后请求时间</th>
                  </tr>
                </thead>
                <tbody>
                  {sessionData.map((row) => (
                    <tr key={row.sessionId}>
                      <td style={tdStyle}><code style={{ fontSize: '12px' }}>{row.sessionId.slice(0, 8)}</code></td>
                      <td style={tdStyle}>{fmt(row.count)}</td>
                      <td style={tdStyle}>{fmt(row.prompt)}</td>
                      <td style={tdStyle}>{fmt(row.completion)}</td>
                      <td style={{ ...tdStyle, color: '#818cf8', fontWeight: 600 }}>{fmt(row.total)}</td>
                      <td style={{ ...tdStyle, color: '#64748b', fontSize: '12px' }}>{new Date(row.lastAt).toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          ) : (
            modelData.length === 0 ? (
              <div style={{ textAlign: 'center', color: '#475569', padding: '40px' }}>暂无数据</div>
            ) : (
              <table style={tableStyle}>
                <thead>
                  <tr>
                    <th style={thStyle}>模型</th>
                    <th style={thStyle}>Provider</th>
                    <th style={thStyle}>请求次数</th>
                    <th style={thStyle}>Prompt Tokens</th>
                    <th style={thStyle}>Completion Tokens</th>
                    <th style={thStyle}>Total Tokens</th>
                  </tr>
                </thead>
                <tbody>
                  {modelData.map((row) => (
                    <tr key={row.model}>
                      <td style={tdStyle}>{row.model}</td>
                      <td style={{ ...tdStyle, color: '#64748b' }}>{row.provider}</td>
                      <td style={tdStyle}>{fmt(row.count)}</td>
                      <td style={tdStyle}>{fmt(row.prompt)}</td>
                      <td style={tdStyle}>{fmt(row.completion)}</td>
                      <td style={{ ...tdStyle, color: '#818cf8', fontWeight: 600 }}>{fmt(row.total)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          )}
        </div>
      </div>
    </div>
  )
}
