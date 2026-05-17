import { cn } from '@/lib/cn';
import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';

interface MenuProps {
  trigger: ReactNode;
  children: ReactNode;
}

export function Menu({ trigger, children }: MenuProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    }
    if (open) document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open]);

  return (
    <div ref={ref} className="relative inline-block">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="px-2 py-1 border border-line rounded text-sm font-semibold hover:bg-line-soft"
      >
        {trigger}
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 bg-white border border-line rounded shadow-lg min-w-[160px] z-10">
          <div onClick={() => setOpen(false)}>{children}</div>
        </div>
      )}
    </div>
  );
}

export function MenuItem({
  onClick,
  children,
  danger = false,
}: {
  onClick: () => void;
  children: ReactNode;
  danger?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'w-full text-left px-3 py-2 text-sm hover:bg-line-soft',
        danger ? 'text-red-600' : 'text-ink',
      )}
    >
      {children}
    </button>
  );
}
