// Command teamctl — агентский CLI поверх REST-эндпоинтов демона (P2.12).
//
// teamctl — клиент (не обход, не дублирование бизнес-логики): парсит flags,
// ходит в демон (HTTP + X-API-Key), печатает результат (--json / human).
//
// Конфигурация: --server / $TEAMCTL_SERVER (default http://localhost:8080);
// --api-key / $TEAMCTL_API_KEY. Exit: 0 успех, 1 HTTP/сеть, 2 usage.
package main

import (
	"os"

	"daemon/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
