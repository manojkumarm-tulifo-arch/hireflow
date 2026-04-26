import { jsx as _jsx } from "react/jsx-runtime";
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from 'react-router-dom';
import { AuthProvider } from '@/auth/AuthContext';
import { router } from '@/routes';
const qc = new QueryClient({
    defaultOptions: {
        queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 30_000 },
    },
});
export function App() {
    return (_jsx(QueryClientProvider, { client: qc, children: _jsx(AuthProvider, { children: _jsx(RouterProvider, { router: router }) }) }));
}
