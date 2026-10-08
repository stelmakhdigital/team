#!/usr/bin/env python3
"""check_openapi.py — сверяет маршруты server.go с paths в openapi.yaml (R6).

Сравнение набора `METHOD /path` из backend/internal/api/http/server.go
(HandleFunc) с operations в docs/contracts/openapi.yaml. Падает с ненулевым
кодом, если есть маршрут без описания в спеке ИЛИ описан несуществующий маршрут.

Запуск:  python3 scripts/check_openapi.py   (с корня репозитория или откуда угодно)
Зависимости: только PyYAML (python3-yaml). Runtime-зависимости проекта не добавляет.
"""
import re
import sys
from pathlib import Path

HTTP_METHODS = {"get", "post", "put", "patch", "delete", "head", "options"}


def find_repo_root(start: Path) -> Path:
    for p in [start, *start.parents]:
        if (p / "backend" / "internal" / "api" / "http" / "server.go").exists() and \
           (p / "docs" / "contracts" / "openapi.yaml").exists():
            return p
    raise SystemExit("не найден корень репозитория (backend/.../server.go + docs/contracts/openapi.yaml)")


def routes_from_go(server_go: Path) -> set:
    text = server_go.read_text(encoding="utf-8")
    # mux.HandleFunc("METHOD /path", handler)
    pairs = re.findall(r'mux\.HandleFunc\(\s*"([A-Z]+)\s+([^"]+)"', text)
    return {(m.upper(), p) for m, p in pairs}


def ops_from_spec(openapi: Path) -> set:
    try:
        import yaml
    except ImportError:
        raise SystemExit("нужен PyYAML: pip install pyyaml (или python3-yaml)")
    spec = yaml.safe_load(openapi.read_text(encoding="utf-8"))
    ops = set()
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        for method in item:
            if method.lower() in HTTP_METHODS:
                ops.add((method.upper(), path))
    return ops


def main() -> int:
    root = find_repo_root(Path(__file__).resolve())
    go_routes = routes_from_go(root / "backend" / "internal" / "api" / "http" / "server.go")
    spec_ops = ops_from_spec(root / "docs" / "contracts" / "openapi.yaml")

    missing_in_spec = sorted(go_routes - spec_ops)   # маршрут есть в коде, но не в спеке
    extra_in_spec = sorted(spec_ops - go_routes)     # описан маршрут, которого нет в коде

    print(f"server.go routes : {len(go_routes)}")
    print(f"openapi operations: {len(spec_ops)}")

    ok = True
    if missing_in_spec:
        ok = False
        print("\n[FAIL] Маршруты в server.go, НО не описаны в openapi.yaml:")
        for m, p in missing_in_spec:
            print(f"       {m:6} {p}")
    if extra_in_spec:
        ok = False
        print("\n[FAIL] Описаны в openapi.yaml, но НЕТ в server.go:")
        for m, p in extra_in_spec:
            print(f"       {m:6} {p}")

    if ok:
        print("\n[OK] Покрытие маршрутов 1-в-1: все операции server.go описаны в openapi.yaml.")
        return 0
    print("\n[FAIL] Расхождение между server.go и openapi.yaml — синхронизируйте спеку.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
