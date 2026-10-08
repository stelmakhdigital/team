import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { WorkflowBlock } from '../../../types/api';

export interface BlockNodeData extends Record<string, unknown> {
  block: WorkflowBlock;
  selected: boolean;
  editMode: boolean;
}

export const BLOCK_META: Record<WorkflowBlock['type'], { icon: string; cls: string; hint: string }> = {
  task: { icon: '▣', cls: 'wf-task', hint: 'Задача для роли' },
  decision: { icon: '◇', cls: 'wf-decision', hint: 'Ветка (yes/no)' },
  parallel: { icon: '⑂', cls: 'wf-parallel', hint: 'Параллельные ветки' },
  loop: { icon: '↻', cls: 'wf-loop', hint: 'Повторять N раз' },
  agent: { icon: '⚙', cls: 'wf-agent', hint: 'Шаг агента' },
  manual: { icon: '✋', cls: 'wf-manual', hint: 'Ручное утверждение' },
};

function configSummary(block: WorkflowBlock): string | null {
  const c = block.config ?? {};
  if (block.type === 'task' || block.type === 'agent') {
    if (c.role) return `→ ${c.role}`;
    return null;
  }
  if (block.type === 'loop' && c.count != null) return `× ${c.count}`;
  if (block.type === 'manual' && c.note) return String(c.note);
  return null;
}

function BlockNodeInner({ data }: NodeProps) {
  const { block, selected, editMode } = data as BlockNodeData;
  const meta = BLOCK_META[block.type];
  const summary = configSummary(block);
  return (
    <div
      className={`wf-node ${meta.cls}${selected ? ' wf-selected' : ''}`}
      data-block-id={block.id}
      aria-label={`block ${block.type} ${block.label ?? ''}`}
    >
      <Handle type="target" position={Position.Left} className={`wf-handle ${editMode ? 'wf-handle-edit' : ''}`} />
      <Handle type="source" position={Position.Right} className={`wf-handle ${editMode ? 'wf-handle-edit' : ''}`} />
      <div className="wf-node-head">
        <span className="wf-node-icon" aria-hidden="true">{meta.icon}</span>
        <span className="wf-node-label">{block.label ?? block.type}</span>
      </div>
      <div className="wf-node-sub mono">
        <span>{block.type}</span>
        {summary && <span className="wf-node-summary">{summary}</span>}
      </div>
    </div>
  );
}

export const BlockNode = memo(BlockNodeInner, (a, b) => {
  const da = a.data as BlockNodeData;
  const db = b.data as BlockNodeData;
  return (
    da.block.id === db.block.id &&
    da.block.type === db.block.type &&
    da.block.label === db.block.label &&
    JSON.stringify(da.block.config) === JSON.stringify(db.block.config) &&
    da.selected === db.selected &&
    da.editMode === db.editMode
  );
});
