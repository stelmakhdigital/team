import { useCallback, useRef, useState } from 'react';

export interface MutationState {
  pending: boolean;
  error: { status: number; code: string; message: string } | null;
}

export function useMutation<TArgs extends unknown[], TResult>(
  fn: (...args: TArgs) => Promise<TResult>,
): { mutate: (...args: TArgs) => Promise<TResult>; pending: boolean; error: MutationState['error'] } {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<MutationState['error']>(null);
  const alive = useRef(true);
  // Re-entry guard: a second mutate() while one is in flight returns the same
  // promise instead of firing a duplicate request. Defense-in-depth on top of
  // the `disabled={pending}` button pattern — makes "no double-submit" hold
  // even if a click lands before React re-renders the disabled attribute.
  const inFlight = useRef<Promise<TResult> | null>(null);

  const mutate = useCallback(
    (...args: TArgs): Promise<TResult> => {
      if (inFlight.current) return inFlight.current;
      setPending(true);
      setError(null);
      const run = (async () => {
        try {
          return await fn(...args);
        } catch (e: unknown) {
          if (alive.current) {
            const status = typeof (e as { status?: number })?.status === 'number' ? (e as { status: number }).status : 0;
            const code = (e as { code?: string })?.code ?? 'error';
            const message = e instanceof Error ? e.message : 'Unknown error';
            setError({ status, code, message });
          }
          throw e;
        } finally {
          if (alive.current) {
            setPending(false);
            inFlight.current = null;
          }
        }
      })();
      inFlight.current = run;
      return run;
    },
    [fn],
  );

  return { mutate, pending, error };
}
