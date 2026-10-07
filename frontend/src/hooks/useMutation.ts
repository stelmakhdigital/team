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

  const mutate = useCallback(
    async (...args: TArgs): Promise<TResult> => {
      setPending(true);
      setError(null);
      try {
        const res = await fn(...args);
        return res;
      } catch (e: unknown) {
        if (alive.current) {
          const status = typeof (e as { status?: number })?.status === 'number' ? (e as { status: number }).status : 0;
          const code = (e as { code?: string })?.code ?? 'error';
          const message = e instanceof Error ? e.message : 'Unknown error';
          setError({ status, code, message });
        }
        throw e;
      } finally {
        if (alive.current) setPending(false);
      }
    },
    [fn],
  );

  return { mutate, pending, error };
}
