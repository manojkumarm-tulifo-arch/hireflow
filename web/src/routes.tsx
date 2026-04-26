import { createBrowserRouter, Navigate } from 'react-router-dom';
import { AppShell } from '@/components/layout/AppShell';
import { ProtectedRoute } from '@/auth/ProtectedRoute';
import { LoginPage } from '@/auth/LoginPage';
import { IntentCapturePage } from '@/features/intent/IntentCapturePage';
import { IntentListPage } from '@/features/intent/IntentListPage';
import { IntentDetailPage } from '@/features/intent/IntentDetailPage';
import { PostingListPage } from '@/features/posting/PostingListPage';
import { PostingDetailPage } from '@/features/posting/PostingDetailPage';

export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  {
    path: '/',
    element: <ProtectedRoute><AppShell /></ProtectedRoute>,
    children: [
      { index: true, element: <Navigate to="/intents" replace /> },
      { path: 'intents', element: <IntentListPage /> },
      { path: 'intents/new', element: <IntentCapturePage /> },
      { path: 'intents/:id', element: <IntentDetailPage /> },
      { path: 'postings', element: <PostingListPage /> },
      { path: 'postings/:id', element: <PostingDetailPage /> },
    ],
  },
]);
