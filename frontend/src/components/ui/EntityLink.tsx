import { Link } from 'react-router-dom';
import type { ReactNode } from 'react';

// U4b: unified clickable wrapper for elements referencing an entity/event.
// Any element that references a task/session/etc. uses EntityLink so it always
// leads to a detail page/panel (fetch-by-id, no duplicated data).
export default function EntityLink({
  to,
  children,
  title,
  className,
}: {
  to: string;
  children: ReactNode;
  title?: string;
  className?: string;
}) {
  return (
    <Link to={to} className={`entity-link${className ? ` ${className}` : ''}`} title={title}>
      {children}
    </Link>
  );
}
