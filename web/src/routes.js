import { jsx as _jsx } from "react/jsx-runtime";
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
    { path: '/login', element: _jsx(LoginPage, {}) },
    {
        path: '/',
        element: _jsx(ProtectedRoute, { children: _jsx(AppShell, {}) }),
        children: [
            { index: true, element: _jsx(Navigate, { to: "/intents", replace: true }) },
            { path: 'intents', element: _jsx(IntentListPage, {}) },
            { path: 'intents/new', element: _jsx(IntentCapturePage, {}) },
            { path: 'intents/:id', element: _jsx(IntentDetailPage, {}) },
            { path: 'postings', element: _jsx(PostingListPage, {}) },
            { path: 'postings/:id', element: _jsx(PostingDetailPage, {}) },
        ],
    },
]);
