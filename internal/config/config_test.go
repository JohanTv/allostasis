package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohanTv/allostasis/internal/config"
)

func TestResolveDatabasePath_Hierarchy(t *testing.T) {
	origAllostasisEnv := os.Getenv("ALLOSTASIS_DB_PATH")
	origPlaneEnv := os.Getenv("PLANE_DB_PATH")
	defer func() {
		_ = os.Setenv("ALLOSTASIS_DB_PATH", origAllostasisEnv)
		_ = os.Setenv("PLANE_DB_PATH", origPlaneEnv)
	}()

	// 1. Flag tiene prioridad absoluta
	_ = os.Setenv("ALLOSTASIS_DB_PATH", "/tmp/env_alo.db")
	_ = os.Setenv("PLANE_DB_PATH", "/tmp/env_plane.db")
	res := config.ResolveDatabasePath("/custom/flag.db")
	if res != "/custom/flag.db" {
		t.Errorf("esperado flag '/custom/flag.db', obtenido: %s", res)
	}

	// 2. ALLOSTASIS_DB_PATH precede a PLANE_DB_PATH
	res = config.ResolveDatabasePath("")
	if res != "/tmp/env_alo.db" {
		t.Errorf("esperado ALLOSTASIS_DB_PATH '/tmp/env_alo.db', obtenido: %s", res)
	}

	// 3. PLANE_DB_PATH cuando ALLOSTASIS_DB_PATH está vacío
	_ = os.Unsetenv("ALLOSTASIS_DB_PATH")
	res = config.ResolveDatabasePath("")
	if res != "/tmp/env_plane.db" {
		t.Errorf("esperado PLANE_DB_PATH '/tmp/env_plane.db', obtenido: %s", res)
	}

	// 4. Salvaguarda en entorno de test cuando ambos envs están vacíos
	_ = os.Unsetenv("PLANE_DB_PATH")
	res = config.ResolveDatabasePath("")
	expectedTestDB := filepath.Join(os.TempDir(), "alo_test_default.db")
	if res != expectedTestDB {
		t.Errorf("esperado default de test '%s', obtenido: %s", expectedTestDB, res)
	}
}

func TestValidateTestSafety_PanicsOnProdDB(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir no disponible")
	}
	prodDB := filepath.Join(home, ".cache", "allostasis", "allostasis.db")

	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("se esperaba pánico por violación de aislamiento con base de producción")
		}
	}()

	config.ValidateTestSafety(prodDB)
}
