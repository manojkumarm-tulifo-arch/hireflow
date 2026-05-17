import { cn } from '@/lib/cn';
import { useCallback, useRef, useState } from 'react';
import type { ReactNode } from 'react';

interface DropZoneProps {
  accept: string;
  multiple?: boolean;
  onFiles: (files: File[]) => void;
  children?: ReactNode;
}

export function DropZone({
  accept,
  multiple = true,
  onFiles,
  children,
}: DropZoneProps) {
  const [dragging, setDragging] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragging(true);
  }, []);

  const onDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragging(false);
  }, []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragging(false);
      const files = Array.from(e.dataTransfer.files);
      if (files.length) onFiles(files);
    },
    [onFiles],
  );

  const onChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(e.target.files ?? []);
      if (files.length) onFiles(files);
      if (inputRef.current) inputRef.current.value = '';
    },
    [onFiles],
  );

  return (
    <div
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
      className={cn(
        'border-2 border-dashed rounded-lg p-10 text-center transition-colors',
        dragging ? 'border-accent bg-accent-soft' : 'border-line bg-white',
      )}
    >
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        multiple={multiple}
        onChange={onChange}
        className="hidden"
      />
      {children}
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        className="mt-2 text-accent font-semibold hover:text-accent-hover underline-offset-2 hover:underline"
      >
        browse files
      </button>
    </div>
  );
}
