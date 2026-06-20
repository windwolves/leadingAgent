import { NavLink, Outlet } from 'react-router-dom'

export default function AppLayout() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      <nav style={{
        height: '48px',
        display: 'flex',
        alignItems: 'center',
        padding: '0 16px',
        borderBottom: '1px solid #1e293b',
        background: '#0f172a',
        gap: '24px',
        flexShrink: 0,
      }}>
        <span style={{ fontWeight: 600, fontSize: '15px', color: '#f1f5f9' }}>LeadingAgent</span>
        <NavLink
          to="/chat"
          style={({ isActive }) => ({
            color: isActive ? '#818cf8' : '#94a3b8',
            textDecoration: 'none',
            fontSize: '14px',
            fontWeight: isActive ? 600 : 400,
          })}
        >
          聊天
        </NavLink>
        <NavLink
          to="/tokens"
          style={({ isActive }) => ({
            color: isActive ? '#818cf8' : '#94a3b8',
            textDecoration: 'none',
            fontSize: '14px',
            fontWeight: isActive ? 600 : 400,
          })}
        >
          Token 统计
        </NavLink>
      </nav>
      <div style={{ flex: 1, overflow: 'hidden' }}>
        <Outlet />
      </div>
    </div>
  )
}
