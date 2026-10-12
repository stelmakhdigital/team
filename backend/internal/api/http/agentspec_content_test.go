package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGetAgentSpecEndpoint — W3.2: GET /api/v1/agentspecs/{name} (read-only).
// 200 (name+yaml), 404 (нет), traversal (a%2Fb / ..%2Fetc%2Fpasswd → не 200), 401 (без auth).
func TestGetAgentSpecEndpoint(t *testing.T) {
	srv, _, _, svc := newProjectTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}

	// top-level spec-файл (list читает верхний уровень SpecsDir)
	if err := os.WriteFile(filepath.Join(svc.SpecsDir, "w3spec.yaml"), []byte("name: w3spec\nmodel: test-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// (a) 200: содержимое
	st, out, _ := do(t, "GET", srv.URL+"/api/v1/agentspecs/w3spec", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("get spec: %d %v", st, out)
	}
	if out["name"] != "w3spec" {
		t.Errorf("name = %v, want w3spec", out["name"])
	}
	yamlStr, _ := out["yaml"].(string)
	if !strings.Contains(yamlStr, "model: test-model") {
		t.Errorf("yaml = %q, want content", yamlStr)
	}

	// (b) 404: не существует
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/agentspecs/missing", nil, admin)
	if st != http.StatusNotFound {
		t.Errorf("missing: %d, want 404", st)
	}

	// (c) traversal: закодированные разделители/.. → не 200 (mux cleanPath или 400 в service)
	for _, enc := range []string{"a%2Fb", "..%2Fetc%2Fpasswd"} {
		st, _, _ = do(t, "GET", srv.URL+"/api/v1/agentspecs/"+enc, nil, admin)
		if st == http.StatusOK {
			t.Errorf("traversal %q: 200, want non-200", enc)
		}
	}

	// (d) 401: без auth
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/agentspecs/w3spec", nil, nil)
	if st != http.StatusUnauthorized {
		t.Errorf("no auth: %d, want 401", st)
	}

	// гейт teams.read — в middleware (case "agentspecs"); здесь проверяем 401/200.
}
