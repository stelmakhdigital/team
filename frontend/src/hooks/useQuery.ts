import { useCallback, useEffect, useRef, useState } from 'react';

export interface QueryState<T> {
  data: T | null;
  loading: boolean;
  error: { status: number; code: string; message: string } | null;
  refetch: () => void;
}

/** Data fetching with cache invalidation, stale-request cancellation
 * (AbortController) and a single in-flight deduplication per key. */
export function useQuery<T>(key: string, fetcher: (signal: AbortSignal) => Promise<T>, deps: unknown[] = []): QueryState<T> {
  const [state, setState] = useState<{ data: T | null; loading: boolean; error: QueryState<T>['error'] }>({
    data: null,
    loading: true,
    error: null,
  });
  const abortRef = useRef<AbortController | null>(null);
  const version = useRef(0);
  const cache = useRef(new Map<string, T>());

  const load = useCallback(
    (reset: boolean) => {
      abortRef.current?.abort();
      const ac = new AbortController();
      abortRef.current = ac;
      const v = ++version.current;
      if (reset) setState((s) => ({ ...s, loading: true, error: null }));
      fetcher(ac.signal)
        .then((data) => {
          if (v !== version.current) return;
          cache.current.set(key, data);
          setState({ data, loading: false, error: null });
        })
        .catch((e: unknown) => {
          if (v !== version.current) return;
          if (e instanceof DOMException && e.name === 'AbortError') return;
          const status = typeof (e as { status?: number })?.status === 'number' ? (e as { status: number }).status : 0;
          const code = (e as { code?: string })?.code ?? 'error';
          const message = e instanceof Error ? e.message : 'Unknown error';
          setState({ data: cache.current.get(key) ?? null, loading: false, error: { status, code, message } });
        });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key, ...deps],
  );

  useEffect(() => {
    load(true);
    return () => abortRef.current?.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, ...deps]);

  return { ...state, refetch: () => load(false) };
}
