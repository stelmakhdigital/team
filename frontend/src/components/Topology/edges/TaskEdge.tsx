import { memo } from 'react';
import { BaseEdge, getBezierPath, type EdgeProps } from '@xyflow/react';
import type { RelativeType } from '../../../types/api';

export interface TaskEdgeData extends Record<string, unknown> {
  kind: RelativeType;
  selected: boolean;
}

const KIND_STYLE: Record<RelativeType, { stroke: string; dash?: string }> = {
  delegates_to: { stroke: '#10b981' },
  spawned_by: { stroke: '#60a5fa' },
  can_observe: { stroke: '#f59e0b', dash: '6 5' },
  collaborates_with: { stroke: '#a78bfa', dash: '2 5' },
};

function TaskEdgeInner({
  id,
  sourceX,
  sourceY,
  sourcePosition,
  targetX,
  targetY,
  targetPosition,
  markerEnd,
  data,
}: EdgeProps) {
  const d = data as TaskEdgeData | undefined;
  const style = (d && KIND_STYLE[d.kind]) || KIND_STYLE.delegates_to;
  const [path] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const selected = d?.selected ?? false;
  return (
    <g>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: style.stroke,
          strokeWidth: selected ? 2.5 : 1.75,
          strokeDasharray: style.dash,
          vectorEffect: 'non-scaling-stroke',
          opacity: selected ? 1 : 0.85,
        }}
      />
    </g>
  );
}

export const TaskEdge = memo(TaskEdgeInner, (a, b) => {
  const da = a.data as TaskEdgeData | undefined;
  const db = b.data as TaskEdgeData | undefined;
  return (
    a.sourceX === b.sourceX &&
    a.sourceY === b.sourceY &&
    a.targetX === b.targetX &&
    a.targetY === b.targetY &&
    da?.kind === db?.kind &&
    da?.selected === db?.selected
  );
});
