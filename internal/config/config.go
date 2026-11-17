package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config centraliza la configuración determinista y la resolución de rutas de Allostasis.
type Config struct {
	DatabasePath string
}

// ResolveDatabasePath aplica la jerarquía canónica de configuración:
// 1. Flag CLI explícito (--db)
// 2. Variable de entorno ALLOSTASIS_DB_PATH
// 3. Variable de entorno de compatibilidad PLANE_DB_PATH
// 4. Default de producción: ~/.cache/allostasis/allostasis.db (0700/0600)
func ResolveDatabasePath(cliFlag string) string {
	if cliFlag != "" {
		return cliFlag
	}

	if envPath := os.Getenv("ALLOSTASIS_DB_PATH"); envPath != "" {
		return envPath
	}
	if envPath := os.Getenv("PLANE_DB_PATH"); envPath != "" {
		return envPath
	}

	// Salvaguarda de prueba: si se detecta entorno de test y no se definió path aislado,
	// forzar una ruta en /tmp para prevenir contaminación del directorio de usuario.
	if isRunningUnderGoTest() {
		return filepath.Join(os.TempDir(), "alo_test_default.db")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "allostasis.db"
	}

	aloDir := filepath.Join(home, ".cache", "allostasis")
	_ = os.MkdirAll(aloDir, 0700)
	_ = os.Chmod(aloDir, 0700)
	aloDB := filepath.Join(aloDir, "allostasis.db")
	if fi, err := os.Stat(aloDB); err == nil && fi.Mode().Perm() != 0600 {
		_ = os.Chmod(aloDB, 0600)
	}
	return aloDB
}

// isRunningUnderGoTest detecta si el proceso actual se ejecuta dentro del harness de `go test`.
func isRunningUnderGoTest() bool {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") || strings.Contains(arg, "test.v") {
			return true
		}
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}
	return false
}

// ValidateTestSafety lanza un panic si detecta que un test intenta escribir en el directorio de usuario.
func ValidateTestSafety(dbPath string) {
	if !isRunningUnderGoTest() {
		return
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		prodDB := filepath.Join(home, ".cache", "allostasis", "allostasis.db")
		if dbPath == prodDB {
			panic(fmt.Sprintf("VIOLACIÓN DE AISLAMIENTO DETECTADA: el test intentó abrir la base de datos de producción: %s", dbPath))
		}
	}
}
