import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { Segment } from '../../../types/api';

export interface SegmentGroupNodeData extends Record<string, unknown> {
  segment: Segment;
  count: number;
  selected: boolean;
}

function SegmentGroupNodeInner({ data }: NodeProps) {
  const { segment, count, selected } = data as SegmentGroupNodeData;
  return (
    <div
      className={`topo-group segment-node ${selected ? 'topo-selected' : ''}`}
      data-segment-id={segment.id}
      aria-label={`segment ${segment.name} ${count} roles`}
      title={segment.description ?? segment.name}
    >
      <Handle type="target" position={Position.Left} className="topo-handle topo-handle-none" />
      <Handle type="source" position={Position.Right} className="topo-handle topo-handle-none" />
      <div className="segment-node-head">
        <span className="segment-node-name">▣ {segment.name}</span>
        <span className="segment-node-count mono">{count}</span>
      </div>
    </div>
  );
}

export const SegmentGroupNode = memo(SegmentGroupNodeInner, (a, b) => {
  const da = a.data as SegmentGroupNodeData;
  const db = b.data as SegmentGroupNodeData;
  return (
    da.segment.id === db.segment.id &&
    da.segment.name === db.segment.name &&
    da.count === db.count &&
    da.selected === db.selected
  );
});
