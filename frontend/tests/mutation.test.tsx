import { describe, expect, it } from 'vitest';
import { render, renderHook, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { useMutation } from '../src/hooks/useMutation';

// A real button wired to useMutation with the standard disabled+pending pattern
// used across the app (Settings save, rename, Team Builder save, …).
function SaveButton({ run }: { run: () => Promise<void> }) {
  const { mutate, pending } = useMutation(run);
  return (
    <button className="btn btn-primary" onClick={() => void mutate()} disabled={pending}>
      {pending ? 'Saving…' : 'Save'}
    </button>
  );
}

describe('P2: mutation disabled+pending pattern (no double-submit)', () => {
  it('useMutation re-entry: 2nd mutate() while in flight returns the same promise and does not fire a 2nd request', async () => {
    const calls: number[] = [];
    let resolve!: () => void;
    const run = () => {
      calls.push(1);
      return new Promise<void>((r) => {
        resolve = r;
      });
    };
    const { result } = renderHook(() => useMutation(run));

    let p1!: Promise<void>;
    await act(async () => {
      p1 = result.current.mutate();
    });
    expect(calls.length).toBe(1);
    expect(result.current.pending).toBe(true);

    // re-entry while in flight → same promise, no 2nd request
    let p2!: Promise<void>;
    await act(async () => {
      p2 = result.current.mutate();
    });
    expect(p2).toBe(p1);
    expect(calls.length).toBe(1);
    expect(result.current.pending).toBe(true);

    // resolve → pending clears, in-flight resets
    act(() => {
      resolve();
    });
    await waitFor(() => expect(result.current.pending).toBe(false));

    // a fresh mutate after completion fires the request again
    await act(async () => {
      result.current.mutate();
    });
    expect(calls.length).toBe(2);
    act(() => {
      resolve();
    });
    await waitFor(() => expect(result.current.pending).toBe(false));
  });

  it('button: pending → disabled + loading indicator; re-click does not duplicate the request', async () => {
    const calls: number[] = [];
    let resolve!: () => void;
    function Ctx() {
      const run = () => {
        calls.push(1);
        return new Promise<void>((r) => {
          resolve = r;
        });
      };
      return <SaveButton run={run} />;
    }
    render(<Ctx />);

    const btn = screen.getByRole('button', { name: 'Save' });
    expect(btn).not.toBeDisabled();

    // click once → request starts and stays pending (deferred promise)
    fireEvent.click(btn);
    const pendingBtn = await screen.findByRole('button', { name: 'Saving…' });
    await waitFor(() => expect(pendingBtn).toBeDisabled());
    expect(calls.length).toBe(1);

    // re-click while pending — even if the handler fires, the guard stops a duplicate
    fireEvent.click(pendingBtn);
    fireEvent.click(pendingBtn);
    expect(calls.length).toBe(1);

    // resolve → button re-enabled with its normal label
    act(() => {
      resolve();
    });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).not.toBeDisabled());
  });
});
