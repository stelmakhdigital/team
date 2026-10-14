// U-X9: относительное время с абсолютным в тултипе (hover/focus).
// Паттерн «relative primary + absolute on hover» (Cloudscape Design System,
// https://cloudscape.design/patterns/general/timestamps/): относительное время
// читается быстро, точное — доступно по наведению без загромождения UI.
import { formatDateTime, formatRelative } from '../../lib/format';

export function Time({ iso }: { iso?: string }) {
  return (
    <span className="time-tip" title={formatDateTime(iso)} style={{ cursor: 'help' }}>
      {formatRelative(iso)}
    </span>
  );
}
