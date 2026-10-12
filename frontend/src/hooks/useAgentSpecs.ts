import { useMemo } from 'react';
import { api } from '../api';
import { useQuery } from './useQuery';

// Fallback, если эндпоинт недоступен (старый backend / mock без данных).
const FALLBACK = ['pi-lead', 'pi-worker', 'pi-reviewer', 'pi-watchdog'];

/** Доступные agent spec (GET /agentspecs) для автодополнения в UI.
 * Возвращает список имён; пока грузится/ошибка — fallback. */
export function useAgentSpecs(): string[] {
  const { data } = useQuery('agentspecs.list', () => api.teams.listAgentSpecs(), []);
  return useMemo(() => {
    const list = data?.specs;
    if (list && list.length > 0) return list;
    return FALLBACK;
  }, [data]);
}
