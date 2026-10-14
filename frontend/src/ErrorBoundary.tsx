import { Component, type ReactNode } from 'react';

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

// U-X1: ErrorBoundary верхнего уровня — крэш рендера больше не «убивает»
// приложение молча (белый экран): честный экран ошибки + кнопка перезагрузки.
// В проде стек показываем только в details (обычно нужен разработчикам локально).
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: { componentStack?: string }) {
    // eslint-заглушек нет — проект без eslint; стек в консоль для отладки
    console.error('[ui] render crash:', error, info);
  }

  render() {
    const { error } = this.state;
    if (!error) {
      return this.props.children;
    }
    return (
      <div className="error-boundary" role="alert" aria-live="assertive">
        <h2>Интерфейс сломался — это наша ошибка, не ваша</h2>
        <p className="muted">
          Ошибка рендера: <code>{error.message}</code>. Перезагрузка вернёт приложение
          (данные на сервере не затронуты).
        </p>
        <div className="error-boundary-actions">
          <button className="btn btn-primary" onClick={() => window.location.reload()}>
            ⟳ Перезагрузить
          </button>
          <button
            className="btn"
            onClick={() => this.setState({ error: null })}
            title="Попробовать отрисовать снова без перезагрузки"
          >
            Повторить рендер
          </button>
        </div>
      </div>
    );
  }
}

