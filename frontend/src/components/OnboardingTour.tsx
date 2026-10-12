import { useCallback, useEffect, useRef, useState } from 'react';
import { useEscapeStack } from '../hooks/useEscapeStack';

// W4.2: Onboarding-тур (first visit). Оверлей с «дыркой» (box-shadow на
// spotlight-див по getBoundingClientRect цели) + tooltip-карточка (auto-flip),
// Далее/Пропустить, счётчик, Esc (через useEscapeStack), a11y (aria-live, tab-trap).
// Недоступная цель (нет [data-tour="id"] в DOM) → шаг пропускается автоматически.
// Без новых зависимостей. Reduced-motion — в CSS (transition/animation off).

export interface TourStep {
  /** значение data-tour цели */
  id: string;
  title: string;
  body: string;
}

interface Props {
  steps: TourStep[];
  onClose: () => void;
}

interface Spot {
  top: number;
  left: number;
  width: number;
  height: number;
}
interface Tip {
  top: number;
  left: number;
  below: boolean;
}

const PAD = 8; // «дырка» вокруг цели
const TIP_W = 320;
const TIP_H = 170;
const MARGIN = 12;

function findTarget(id: string): HTMLElement | null {
  // «цель доступна» = элемент в DOM. Для пустых/закрытых целей (Attention-бейдж при
  // count=0, палитра закрыта) querySelector → null → шаг пропускается. getClientRects-проверку
  // сознательно не делаем: jsdom не раскладывает элементы (rects пустые), а в production
  // нулевой rect у скрытого элемента — редкий edge-case.
  return document.querySelector<HTMLElement>(`[data-tour="${id}"]`) ?? null;
}

export default function OnboardingTour({ steps, onClose }: Props) {
  const [idx, setIdx] = useState(0);
  const [spot, setSpot] = useState<Spot | null>(null);
  const [tip, setTip] = useState<Tip | null>(null);
  const [tick, setTick] = useState(0); // resize → пересчёт
  const nextRef = useRef<HTMLButtonElement | null>(null);
  const skipRef = useRef<HTMLButtonElement | null>(null);

  const current = idx < steps.length ? steps[idx] : null;
  const finish = useCallback(() => onClose(), [onClose]);

  // Esc закрывает тур (если он верхний оверлей) — единый механизм
  useEscapeStack(finish, true);

  // resize → пересчёт позиции дырки/tooltip
  useEffect(() => {
    const onResize = () => setTick((t) => t + 1);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  // позиция текущего шага + пропуск недоступных целей
  useEffect(() => {
    if (!current) {
      finish();
      return;
    }
    let cancelled = false;
    const compute = () => {
      if (cancelled) return;
      const el = findTarget(current.id);
      if (!el) {
        // цель недоступна → пропустить шаг (вперёд; в конце — завершить)
        if (idx + 1 >= steps.length) finish();
        else setIdx(idx + 1);
        return;
      }
      el.scrollIntoView?.({ block: 'center' });
      const r = el.getBoundingClientRect();
      setSpot({
        top: Math.max(0, r.top - PAD),
        left: Math.max(0, r.left - PAD),
        width: r.width + PAD * 2,
        height: r.height + PAD * 2,
      });
      const below = r.top + r.height + TIP_H + MARGIN < window.innerHeight;
      const top = below ? r.top + r.height + MARGIN : Math.max(MARGIN, r.top - TIP_H - MARGIN);
      let left = r.left + r.width / 2 - TIP_W / 2;
      left = Math.max(MARGIN, Math.min(left, window.innerWidth - TIP_W - MARGIN));
      setTip({ top, left, below });
    };
    const id = window.setTimeout(compute, 60);
    return () => {
      cancelled = true;
      window.clearTimeout(id);
    };
  }, [idx, tick, current, steps.length, finish]);

  // focus на «Далее» при смене шага
  useEffect(() => {
    if (current && spot && tip) requestAnimationFrame(() => nextRef.current?.focus());
  }, [idx, current, spot, tip]);

  // tab-trap между «Пропустить» и «Далее» (2 элемента: Tab/Shift+Tab → другой)
  const onTipKey = (e: React.KeyboardEvent) => {
    if (e.key !== 'Tab') return;
    e.preventDefault();
    const a = skipRef.current;
    const b = nextRef.current;
    if (!a || !b) return;
    (document.activeElement === b ? a : b).focus();
  };

  // клик мимо цели → закрыть (внутри «дырки» — игнор)
  const onOverlayClick = (e: React.MouseEvent) => {
    if (spot && e.clientX >= spot.left && e.clientX <= spot.left + spot.width && e.clientY >= spot.top && e.clientY <= spot.top + spot.height) {
      return;
    }
    finish();
  };

  if (!current) return null;
  const isLast = idx === steps.length - 1;

  return (
    <div className="tour-overlay" role="dialog" aria-modal="true" aria-label="Онбординг тур" onMouseDown={onOverlayClick}>
      {spot && (
        <div
          className="tour-spot"
          style={{ top: spot.top, left: spot.left, width: spot.width, height: spot.height }}
          aria-hidden="true"
        />
      )}
      {tip && (
        <div
          className={`tour-tip${tip.below ? ' tour-tip--below' : ' tour-tip--above'}`}
          style={{ top: tip.top, left: tip.left, width: TIP_W }}
          onKeyDown={onTipKey}
        >
          <div className="tour-tip-head">
            <span className="tour-tip-title">{current.title}</span>
            <span className="tour-tip-counter">{idx + 1} / {steps.length}</span>
          </div>
          <p className="tour-tip-body" aria-live="polite">
            {current.body}
          </p>
          <div className="tour-tip-actions">
            <button type="button" className="btn btn-small" ref={skipRef} onClick={finish}>
              Пропустить
            </button>
            <button type="button" className="btn btn-small btn-primary" ref={nextRef} onClick={() => setIdx(idx + 1)}>
              {isLast ? 'Готово' : 'Далее'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
