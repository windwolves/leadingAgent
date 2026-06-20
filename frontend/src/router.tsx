import { createBrowserRouter, Navigate } from 'react-router-dom'
import AppLayout from './layouts/AppLayout'
import ChatPage from './pages/ChatPage'
import TokenDashboardPage from './pages/TokenDashboardPage'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppLayout />,
    children: [
      { index: true, element: <Navigate to="/chat" replace /> },
      { path: 'chat', element: <ChatPage /> },
      { path: 'tokens', element: <TokenDashboardPage /> },
    ],
  },
])
