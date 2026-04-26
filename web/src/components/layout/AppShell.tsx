import { NavLink, Outlet } from 'react-router-dom';
import { LogOut, MessageSquarePlus, ListChecks, Briefcase } from 'lucide-react';
import { useAuth } from '@/auth/AuthContext';
import { cn } from '@/lib/cn';

const navItems = [
  { to: '/intents/new', label: 'Capture Intent', icon: MessageSquarePlus },
  { to: '/intents', label: 'Intents', icon: ListChecks },
  { to: '/postings', label: 'Postings', icon: Briefcase },
];

export function AppShell() {
  const { signOut } = useAuth();

  return (
    <div className="min-h-screen flex">
      <aside className="w-60 bg-white border-r border-line flex flex-col">
        <div className="px-5 py-5 border-b border-line">
          <h1 className="font-display text-xl font-bold text-ink">hireflow</h1>
          <p className="text-[11px] text-ink-mute mt-0.5">Recruiter workspace</p>
        </div>
        <nav className="flex-1 px-3 py-4 space-y-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === '/intents'}
                className={({ isActive }) =>
                  cn(
                    'flex items-center gap-2 px-3 py-2 rounded-md text-sm font-semibold transition-colors',
                    isActive ? 'bg-accent-soft text-accent' : 'text-ink-sub hover:bg-line-soft hover:text-ink',
                  )
                }
              >
                <Icon className="w-4 h-4" />
                {item.label}
              </NavLink>
            );
          })}
        </nav>
        <button
          onClick={signOut}
          className="m-3 flex items-center gap-2 px-3 py-2 text-sm text-ink-sub hover:text-ink rounded-md hover:bg-line-soft"
        >
          <LogOut className="w-4 h-4" /> Sign out
        </button>
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
