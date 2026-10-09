package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileLayerPath — путь к опциональному YAML-файлу конфига (слой "file", U4):
// env DAEMON_CONFIG_FILE, по умолчанию daemon.yaml в cwd.
func FileLayerPath() string {
	p := os.Getenv("DAEMON_CONFIG_FILE")
	if p == "" {
		p = "daemon.yaml"
	}
	return p
}

// LoadFileLayer — читает опциональный YAML-файл конфига и возвращает известные
// ключи "section.key" → каноническая строка. Файла нет → пустой слой (ошибки нет).
// Неизвестные секции/ключи и пустые значения игнорируются.
//
// Формат файла (2 уровня — секция.ключ):
//
//	server:
//	  listen_addr: ":9000"
//	logging:
//	  level: debug
func LoadFileLayer(path string) (map[string]string, error) {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	var tree map[string]map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}
	for section, kv := range tree {
		for k, v := range kv {
			s := scalarString(v)
			if s == "" {
				continue
			}
			out[section+"."+k] = s
		}
	}
	return out, nil
}

// scalarString — YAML-скаляр → каноническая строка (list → comma-separated).
func scalarString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s := scalarString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprintf("%v", x)
	}
}
