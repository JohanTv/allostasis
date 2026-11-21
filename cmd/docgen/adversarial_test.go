package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTempDocs inicializa un entorno de pruebas aislado con go.mod y la estructura docs/
func setupTempDocs(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module testrepo\n"), 0644); err != nil {
		t.Fatalf("setupTempDocs: error creando go.mod: %v", err)
	}
	docsDir := filepath.Join(tmp, "docs")
	if err := os.MkdirAll(filepath.Join(docsDir, "decisions"), 0755); err != nil {
		t.Fatalf("setupTempDocs: error creando decisions: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(docsDir, "rfcs"), 0755); err != nil {
		t.Fatalf("setupTempDocs: error creando rfcs: %v", err)
	}
	return tmp, docsDir
}

func writeDocFile(t *testing.T, dir, filename, content string) string {
	t.Helper()
	targetPath := filepath.Join(dir, filename)
	if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
		t.Fatalf("error escribiendo %s: %v", targetPath, err)
	}
	return targetPath
}

// ---------------------------------------------------------------------------
// 1. Colisión de IDs y Nombres
// ---------------------------------------------------------------------------

func TestAdversarial_IDAndNameCollisions(t *testing.T) {
	t.Run("Collision_ExistingIDInSameFolder", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		content := `---
id: "0002"
title: Test Uno
status: proposed
created: 2026-11-15
summary: Primer RFC
---
# RFC
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0002-test-uno.md", content)
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0002-test-dos.md", strings.Replace(content, "Test Uno", "Test Dos", 1))

		_, errs := CollectDocs(docsDir)
		if len(errs) == 0 {
			t.Fatal("se esperaba error por ID duplicado en la misma carpeta, pero pasó sin errores")
		}
		found := false
		for _, e := range errs {
			if strings.Contains(e, "ID duplicado '0002'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("error esperado de ID duplicado no encontrado. Errores: %v", errs)
		}
	})

	t.Run("Collision_ExistingIDAcrossFolders", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		decContent := `---
id: "0010"
title: Decision Diez
status: accepted
created: 2026-11-15
summary: Decision 10
---
# ADR
`
		rfcContent := `---
id: "0010"
title: RFC Diez
status: proposed
created: 2026-11-15
summary: RFC 10
---
# RFC
`
		writeDocFile(t, filepath.Join(docsDir, "decisions"), "0010-decision-diez.md", decContent)
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0010-rfc-diez.md", rfcContent)

		_, errs := CollectDocs(docsDir)
		if len(errs) == 0 {
			t.Fatal("se esperaba colisión de ID entre decisions y rfcs, pero pasó sin errores")
		}
		found := false
		for _, e := range errs {
			if strings.Contains(e, "ID duplicado '0010'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("error esperado de ID duplicado cruzado no encontrado: %v", errs)
		}
	})

	t.Run("Collision_IDMismatchWithFilename", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		content := `---
id: "0004"
title: Mismatch
status: proposed
created: 2026-11-15
summary: ID 0004 en archivo 0003
---
# RFC
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0003-mismatch.md", content)
		_, errs := CollectDocs(docsDir)
		if len(errs) == 0 {
			t.Fatal("se esperaba error por discrepancia de ID en front-matter vs archivo")
		}
		found := false
		for _, e := range errs {
			if strings.Contains(e, "'id' (0004) no coincide con prefijo del archivo (0003)") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("error esperado de discrepancia de ID no encontrado: %v", errs)
		}
	})

	t.Run("InvalidName_Patterns", func(t *testing.T) {
		invalidNames := []string{
			"31-test.md",               // Falta 4 dígitos
			"3-test.md",                // 1 dígito
			"123-test.md",              // 3 dígitos
			"00001-test.md",            // 5 dígitos
			"0031-TEST-MAYUSCULAS.md",  // Mayúsculas
			"0031-test_underscores.md", // Guiones bajos
			"0031-test espacios.md",    // Espacios
			"0031-test$caracteres.md",  // Caracteres especiales
			"0031-test!alerta.md",      // Signos de exclamación
			"0031-.md",                 // Slug vacío
			"0031--.md",                // Solo guión
			"0031-test-.md",            // Guión colgante al final
			"0031--test.md",            // Doble guión consecutivo
		}

		for _, name := range invalidNames {
			t.Run(name, func(t *testing.T) {
				_, docsDir := setupTempDocs(t)
				content := `---
id: "0031"
title: Test Inválido
status: proposed
created: 2026-11-15
summary: Prueba
---
# Doc
`
				writeDocFile(t, filepath.Join(docsDir, "rfcs"), name, content)
				_, errs := CollectDocs(docsDir)
				if len(errs) == 0 {
					t.Fatalf("se esperaba rechazo para nombre inválido '%s', pero fue aceptado", name)
				}
				found := false
				for _, e := range errs {
					if strings.Contains(e, "nombre no sigue convención NNNN-slug-en-minusculas.md") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("nombre '%s' falló con error inesperado: %v", name, errs)
				}
			})
		}
	})

	t.Run("Scaffold_RejectsInvalidSlugAndPathTraversal", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)

		maliciousSlugs := []struct {
			name string
			slug string
		}{
			{"PathTraversalSlash", "../../etc/passwd"},
			{"PathTraversalBackslash", "..\\..\\etc"},
			{"InvalidUnderscore", "slug_with_underscores"},
			{"InvalidSpecialChars", "slug$danger"},
			{"EmptySlug", ""},
			{"WhitespaceOnly", "   "},
			{"ConsecutiveHyphens", "slug--danger"},
			{"TrailingHyphen", "slug-"},
		}

		for _, tc := range maliciousSlugs {
			t.Run(tc.name, func(t *testing.T) {
				_, err := CreateDocScaffold(docsDir, "rfcs", tc.slug, "Test Title")
				if err == nil {
					t.Fatalf("CreateDocScaffold debió rechazar el slug malicioso '%s'", tc.slug)
				}
			})
		}
	})

	t.Run("Scaffold_RejectsInvalidKind", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		_, err := CreateDocScaffold(docsDir, "malicious_folder", "valid-slug", "Title")
		if err == nil {
			t.Fatal("CreateDocScaffold debió rechazar 'malicious_folder'")
		}
	})

	t.Run("Scaffold_RejectsEmptyTitle", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		_, err := CreateDocScaffold(docsDir, "rfcs", "valid-slug", "   ")
		if err == nil {
			t.Fatal("CreateDocScaffold debió rechazar título vacío")
		}
	})
}

// ---------------------------------------------------------------------------
// 2. Corrupción de Front-Matter
// ---------------------------------------------------------------------------

func TestAdversarial_FrontMatterCorruption(t *testing.T) {
	t.Run("MissingRequiredFields", func(t *testing.T) {
		fields := []string{"id", "title", "status", "created", "summary"}

		for _, missingField := range fields {
			t.Run("Missing_"+missingField, func(t *testing.T) {
				_, docsDir := setupTempDocs(t)

				metaMap := map[string]string{
					"id":      "0001",
					"title":   "Título Válido",
					"status":  "proposed",
					"created": "2026-11-15",
					"summary": "Resumen válido",
				}
				delete(metaMap, missingField)

				var sb strings.Builder
				sb.WriteString("---\n")
				for k, v := range metaMap {
					sb.WriteString(fmt.Sprintf("%s: \"%s\"\n", k, v))
				}
				sb.WriteString("---\n# Body\n")

				writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-test-doc.md", sb.String())
				_, errs := CollectDocs(docsDir)
				if len(errs) == 0 {
					t.Fatalf("se esperaba error por falta de campo obligatorio '%s'", missingField)
				}

				expectedMsg := fmt.Sprintf("falta el campo obligatorio '%s' en front-matter", missingField)
				found := false
				for _, e := range errs {
					if strings.Contains(e, expectedMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("error esperado '%s' no encontrado: %v", expectedMsg, errs)
				}
			})
		}
	})

	t.Run("MutilatedDelimiters", func(t *testing.T) {
		testCases := []struct {
			name        string
			content     string
			expectedErr string
		}{
			{
				name:        "EmptyFile",
				content:     "",
				expectedErr: "el archivo no comienza con el bloque '---'",
			},
			{
				name: "NoOpeningDashes",
				content: `id: 0001
title: Sin guiones
---
# Body
`,
				expectedErr: "el archivo no comienza con el bloque '---'",
			},
			{
				name: "UnclosedDashes",
				content: `---
id: 0001
title: Sin cierre
status: proposed
created: 2026-11-15
summary: Sin cierre
# Body sin cerrar front-matter
`,
				expectedErr: "bloque front-matter '---' no cerrado",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, docsDir := setupTempDocs(t)
				writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-mutilated.md", tc.content)
				_, errs := CollectDocs(docsDir)
				if len(errs) == 0 {
					t.Fatalf("caso '%s' debió fallar por front-matter mutilado", tc.name)
				}
				found := false
				for _, e := range errs {
					if strings.Contains(e, tc.expectedErr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("caso '%s': error esperado '%s' no encontrado: %v", tc.name, tc.expectedErr, errs)
				}
			})
		}
	})

	t.Run("InvalidStatusValues", func(t *testing.T) {
		invalidCases := []struct {
			kind   string
			status string
		}{
			{"decisions", "yolo"},
			{"decisions", "pending"},
			{"decisions", "proposed"}, // Válido para RFCs pero no para ADRs
			{"decisions", "rejected"}, // Válido para RFCs pero no para ADRs
			{"rfcs", "yolo"},
			{"rfcs", "pending"},
			{"rfcs", "deprecated"}, // Válido para ADRs pero no para RFCs
			{"rfcs", "UNKNOWN"},
		}

		for _, tc := range invalidCases {
			t.Run(tc.kind+"_"+tc.status, func(t *testing.T) {
				_, docsDir := setupTempDocs(t)
				content := fmt.Sprintf(`---
id: "0001"
title: Test Status
status: %s
created: 2026-11-15
summary: Test status invalido
---
# Content
`, tc.status)
				writeDocFile(t, filepath.Join(docsDir, tc.kind), "0001-test-status.md", content)
				_, errs := CollectDocs(docsDir)
				if len(errs) == 0 {
					t.Fatalf("se esperaba rechazo de status '%s' en '%s'", tc.status, tc.kind)
				}
				expectedSubstr := fmt.Sprintf("estado '%s' no es válido para %s", tc.status, tc.kind)
				found := false
				for _, e := range errs {
					if strings.Contains(e, expectedSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("error esperado '%s' no encontrado: %v", expectedSubstr, errs)
				}
			})
		}
	})

	t.Run("InvalidDateFormatsAndCalendarAnomalies", func(t *testing.T) {
		invalidDates := []struct {
			date string
			desc string
		}{
			{"2026/11/15", "delimitador slash"},
			{"15-11-2026", "formato DD-MM-YYYY"},
			{"2026-11", "falta dia"},
			{"20261115", "sin delimitadores"},
			{"invalid-date", "texto no numerico"},
			{"2026-02-31", "dia imposible en febrero"},
			{"2026-13-10", "mes 13 imposible"},
			{"2026-00-15", "mes 00 imposible"},
			{"2026-04-31", "abril tiene 30 dias"},
		}

		for _, tc := range invalidDates {
			t.Run(tc.desc, func(t *testing.T) {
				_, docsDir := setupTempDocs(t)
				content := fmt.Sprintf(`---
id: "0001"
title: Test Fecha
status: proposed
created: %s
summary: Test fecha anomala
---
# Content
`, tc.date)
				writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-test-date.md", content)
				_, errs := CollectDocs(docsDir)
				if len(errs) == 0 {
					t.Fatalf("fecha anómala '%s' (%s) debió ser rechazada", tc.date, tc.desc)
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// 3. Inyección de Enlaces Rotos y Path Traversal
// ---------------------------------------------------------------------------

func TestAdversarial_BrokenLinkInjection(t *testing.T) {
	t.Run("BrokenLink_NonExistentFile", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		content := `# Doc
Enlace roto a [Archivo Fantasma](archivo-que-no-existe.md).
`
		writeDocFile(t, docsDir, "test.md", content)
		errs := CheckRelativeLinks(docsDir)
		if len(errs) == 0 {
			t.Fatal("CheckRelativeLinks debió detectar el enlace al archivo inexistente")
		}
		found := false
		for _, e := range errs {
			if strings.Contains(e, "enlace roto a 'archivo-que-no-existe.md'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("error esperado de enlace roto no encontrado: %v", errs)
		}
	})

	t.Run("PathTraversal_OutsideRepoBoundary", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		// Intentos de path traversal hacia el exterior del repo
		// Desde docs/rfcs/ (profundidad 2 desde raíz): ../../.. o superior escapa la raíz del repositorio
		traversalTargets := []string{
			"../../../../../../etc/passwd",
			"../../../../etc/shadow",
			"../../../some-external-file.txt",
			"../../../../outside.txt",
		}

		for _, target := range traversalTargets {
			t.Run(target, func(t *testing.T) {
				content := fmt.Sprintf("# Security Test\n[Leak](%s)\n", target)
				writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-leak.md", content)

				errs := CheckRelativeLinks(docsDir)
				if len(errs) == 0 {
					t.Fatalf("CheckRelativeLinks debió rechazar el path traversal '%s'", target)
				}

				found := false
				for _, e := range errs {
					if strings.Contains(e, "path traversal") || strings.Contains(e, "enlace hacia el exterior del repositorio") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("path traversal no fue catalogado como tal: %v", errs)
				}
			})
		}
	})

	t.Run("AbsoluteLinks_Rejected", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		content := `# Test
[Root](/etc/passwd)
[FileProto](file:///etc/hosts)
`
		writeDocFile(t, docsDir, "test.md", content)
		errs := CheckRelativeLinks(docsDir)
		if len(errs) < 2 {
			t.Fatalf("se esperaban al menos 2 errores para enlaces absolutos (/ y file://), obtenidos: %v", errs)
		}
	})

	t.Run("Anchor_AnomaliesAndFragmentation", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		// Crear un archivo destino válido
		writeDocFile(t, docsDir, "valid-target.md", "# Valid Target\n## Section One\n")

		anomalyCases := []struct {
			name        string
			link        string
			expectedErr string
		}{
			{
				name:        "MultipleHashes",
				link:        "[Broken](valid-target.md#section#subsection)",
				expectedErr: "ancla fragmentada o malformada (múltiples '#')",
			},
			{
				name:        "DoubleHashPrefix",
				link:        "[Broken](valid-target.md##double)",
				expectedErr: "ancla fragmentada o malformada (múltiples '#')",
			},
			{
				name:        "DanglingEmptyHash",
				link:        "[Empty](valid-target.md#)",
				expectedErr: "ancla vacía en enlace",
			},
			{
				name:        "EmptyPageHash",
				link:        "[EmptyPage](#)",
				expectedErr: "ancla vacía en enlace",
			},
			{
				name:        "AnchorWithInvalidChars",
				link:        "[Weird](valid-target.md#section<tag>)",
				expectedErr: "ancla con caracteres inválidos",
			},
			{
				name:        "AnchorWithCaretAndBraces",
				link:        "[Weird](valid-target.md#section^{brace})",
				expectedErr: "ancla con caracteres inválidos",
			},
			{
				name:        "AnchorWithWhitespace",
				link:        "[Weird](valid-target.md#section with space)",
				expectedErr: "enlace con espacios en blanco no permitidos",
			},
			{
				name:        "EmptyLink",
				link:        "[Click Here]()",
				expectedErr: "enlace vacío '[]()'",
			},
		}

		for _, tc := range anomalyCases {
			t.Run(tc.name, func(t *testing.T) {
				writeDocFile(t, docsDir, "test-anchor.md", fmt.Sprintf("# Test\n%s\n", tc.link))
				errs := CheckRelativeLinks(docsDir)
				if len(errs) == 0 {
					t.Fatalf("enlace anómalo '%s' debió fallar", tc.link)
				}
				found := false
				for _, e := range errs {
					if strings.Contains(e, tc.expectedErr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("error esperado '%s' no encontrado en: %v", tc.expectedErr, errs)
				}
			})
		}
	})

	t.Run("ValidLinks_PassSuccessfully", func(t *testing.T) {
		tmp, docsDir := setupTempDocs(t)
		// Archivo en la raíz del repo
		writeDocFile(t, tmp, "README.md", "# Project Root\n")
		// Archivo en docs
		writeDocFile(t, docsDir, "BACKLOG.md", "# Backlog\n")
		// RFC válido que enlaza al backlog, al readme y a un ancla intra-doc
		rfcContent := `# RFC
Related: [Backlog](../BACKLOG.md), [Root README](../../README.md).
See [Section Below](#conclusions).

## Conclusions
Everything ok.
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-valid.md", rfcContent)

		errs := CheckRelativeLinks(docsDir)
		if len(errs) > 0 {
			t.Fatalf("enlaces legítimos dentro del repositorio fallaron: %v", errs)
		}
	})
}

// ---------------------------------------------------------------------------
// 4. Desincronización de Índice y Fail-Closed
// ---------------------------------------------------------------------------

func TestAdversarial_IndexDesynchronization_FailClosed(t *testing.T) {
	t.Run("ManualRFCAdded_WithoutMakeDocs_FailClosed", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		indexPath := filepath.Join(docsDir, "INDEX.md")

		// 1. Estado inicial sincronizado con 1 RFC
		rfc1 := `---
id: "0001"
title: RFC Uno
status: proposed
created: 2026-11-15
summary: Primer RFC
---
# RFC Uno
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-rfc-uno.md", rfc1)
		initialItems, _ := CollectDocs(docsDir)
		initialIndex := RenderIndex(initialItems)
		writeDocFile(t, docsDir, "INDEX.md", initialIndex)

		// Verificar que inicialmente está sincronizado
		currentIndex, err := os.ReadFile(indexPath)
		if err != nil || string(currentIndex) != initialIndex {
			t.Fatal("estado inicial falló al sincronizar")
		}

		// 2. Ataque adversarial: añadir manualmente un segundo RFC en docs/rfcs/ sin correr make docs
		rfc2 := `---
id: "0002"
title: RFC Desincronizado
status: proposed
created: 2026-11-15
summary: Este RFC no está en INDEX.md
---
# RFC Desincronizado
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0002-rfc-desincronizado.md", rfc2)

		// 3. Comprobación Fail-Closed: CollectDocs + RenderIndex contra el INDEX.md actual
		updatedItems, docErrs := CollectDocs(docsDir)
		if len(docErrs) > 0 {
			t.Fatalf("documentos con errores de sintaxis: %v", docErrs)
		}
		expectedIndex := RenderIndex(updatedItems)

		actualDiskIndex, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatalf("error leyendo INDEX.md: %v", err)
		}

		// Si string(actualDiskIndex) == expectedIndex, el sistema falló en detectar desincronización
		if string(actualDiskIndex) == expectedIndex {
			t.Fatal("FAIL-CLOSED VIOLADO: El sistema consideró sincronizado un índice que no contiene el RFC manual")
		}

		// El nuevo documento debe estar en el índice esperado pero ausente en el índice en disco
		if !strings.Contains(expectedIndex, "0002") || strings.Contains(string(actualDiskIndex), "0002") {
			t.Fatal("inconsistencia en la verificación del índice desincronizado")
		}
	})

	t.Run("ManualRFCDeleted_WithoutMakeDocs_FailClosed", func(t *testing.T) {
		_, docsDir := setupTempDocs(t)
		indexPath := filepath.Join(docsDir, "INDEX.md")

		rfc1 := `---
id: "0001"
title: RFC Uno
status: proposed
created: 2026-11-15
summary: Primer RFC
---
# RFC Uno
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-rfc-uno.md", rfc1)
		items, _ := CollectDocs(docsDir)
		writeDocFile(t, docsDir, "INDEX.md", RenderIndex(items))

		// Borrar el RFC en disco manualmente
		if err := os.Remove(filepath.Join(docsDir, "rfcs", "0001-rfc-uno.md")); err != nil {
			t.Fatal(err)
		}

		// Fail-Closed: Debe detectar la desincronización
		newItems, _ := CollectDocs(docsDir)
		expected := RenderIndex(newItems)
		actual, _ := os.ReadFile(indexPath)

		if string(actual) == expected {
			t.Fatal("FAIL-CLOSED VIOLADO: Se borró un archivo pero el índice no reportó desincronización")
		}
	})

	t.Run("TableInjection_PipesAndNewlinesEscaped", func(t *testing.T) {
		// Adversarial Red Team: Intentar romper la tabla Markdown con | y \n en title y summary
		items := []DocItem{
			{
				Kind:     "rfcs",
				Filename: "0001-inject.md",
				RelPath:  "rfcs/0001-inject.md",
				ID:       "0001",
				Title:    "Exploit | Header 1 | Header 2\nInyección de nueva fila",
				Status:   "proposed",
				Created:  "2026-11-15",
				Summary:  "Summary | Malicioso | Columna Extra\nSalto de línea malicioso",
			},
		}

		rendered := RenderIndex(items)

		// La tabla Markdown no debe contener saltos de línea crudos dentro de las celdas
		// ni barras verticales sin escapar que agreguen columnas adicionales
		lines := strings.Split(rendered, "\n")
		foundRow := false
		for _, l := range lines {
			if strings.HasPrefix(l, "| **0001** |") {
				foundRow = true
				// Contar columnas: una fila canónica tiene 4 celdas => 5 barras '|'
				// Si hay barras sin escapar, el split por '|' aumentará
				// Verificamos que las barras internas estén escapadas como \|
				if strings.Contains(l, "Exploit \\| Header 1 \\| Header 2") == false {
					t.Errorf("los pipes en el título no fueron debidamente escapados: %s", l)
				}
				if strings.Contains(l, "Summary \\| Malicioso \\| Columna Extra") == false {
					t.Errorf("los pipes en el resumen no fueron debidamente escapados: %s", l)
				}
				if strings.Contains(l, "\n") {
					t.Errorf("la fila contiene salto de línea crudo: %s", l)
				}
			}
		}
		if !foundRow {
			t.Fatal("no se encontró la fila del item adversarial en el índice renderizado")
		}
	})
}

// ---------------------------------------------------------------------------
// 5. Cálculo de --next-id ante Anomalías
// ---------------------------------------------------------------------------

func TestAdversarial_NextIDCalculation(t *testing.T) {
	t.Run("EmptyDocs_ReturnsDeterministic0001", func(t *testing.T) {
		var items []DocItem
		next := NextAvailableID(items)
		if next != 1 {
			t.Fatalf("se esperaba 1 en lista vacía, obtenido %d", next)
		}
		idStr := fmt.Sprintf("%04d", next)
		if idStr != "0001" {
			t.Fatalf("se esperaba formateo '0001', obtenido '%s'", idStr)
		}
	})

	t.Run("NumericGaps_ComputesMaxPlusOne", func(t *testing.T) {
		items := []DocItem{
			{ID: "0001"},
			{ID: "0005"},
			{ID: "0028"},
		}
		next := NextAvailableID(items)
		if next != 29 {
			t.Fatalf("se esperaba max(28)+1=29, obtenido %d", next)
		}
		idStr := fmt.Sprintf("%04d", next)
		if idStr != "0029" {
			t.Fatalf("se esperaba '0029', obtenido '%s'", idStr)
		}
	})

	t.Run("UnsortedItems_ComputesCorrectMax", func(t *testing.T) {
		items := []DocItem{
			{ID: "0028"},
			{ID: "0002"},
			{ID: "0099"},
			{ID: "0015"},
		}
		next := NextAvailableID(items)
		if next != 100 {
			t.Fatalf("se esperaba max(99)+1=100, obtenido %d", next)
		}
		idStr := fmt.Sprintf("%04d", next)
		if idStr != "0100" {
			t.Fatalf("se esperaba '0100', obtenido '%s'", idStr)
		}
	})

	t.Run("AnomalousAndInvalidIDs_IgnoredGracefully", func(t *testing.T) {
		items := []DocItem{
			{ID: "invalid"},
			{ID: "-005"},
			{ID: ""},
			{ID: "0007"},
		}
		next := NextAvailableID(items)
		if next != 8 {
			t.Fatalf("se esperaba que filtrara anomalías y calculara 7+1=8, obtenido %d", next)
		}
	})

	t.Run("HighIDs_NoOverflowOrFormattingError", func(t *testing.T) {
		items := []DocItem{
			{ID: "9999"},
		}
		next := NextAvailableID(items)
		if next != 10000 {
			t.Fatalf("se esperaba 10000, obtenido %d", next)
		}
		idStr := fmt.Sprintf("%04d", next)
		if idStr != "10000" {
			t.Fatalf("se esperaba '10000', obtenido '%s'", idStr)
		}
	})
}

// ---------------------------------------------------------------------------
// 6. Pruebas Subprocess End-to-End del CLI (Fail-Closed)
// ---------------------------------------------------------------------------

func TestAdversarial_CLIProcessExecution(t *testing.T) {
	rootDir, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot falló: %v", err)
	}

	t.Run("CLI_NextIDFlag", func(t *testing.T) {
		cmd := exec.Command("go", "run", "./cmd/docgen", "--next-id")
		cmd.Dir = rootDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("falló ejecución CLI --next-id: %v, salida: %s", err, string(out))
		}
		result := strings.TrimSpace(string(out))
		if len(result) != 4 {
			t.Fatalf("el resultado de --next-id debe tener exactamente 4 dígitos, obtenido: '%s'", result)
		}
	})

	t.Run("CLI_CheckFlag_PassesWhenInSync", func(t *testing.T) {
		cmd := exec.Command("go", "run", "./cmd/docgen", "--check")
		cmd.Dir = rootDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make docs-check debió pasar limpiamente en el repo real, pero falló: %v\nSalida: %s", err, string(out))
		}
	})

	t.Run("CLI_CheckFlag_FailsClosedInDesyncedTempRepo", func(t *testing.T) {
		tmp, docsDir := setupTempDocs(t)
		// Copiar el archivo main de docgen o invocarlo apuntando al repo de prueba
		// Crear un documento sin regenerar índice
		rfc := `---
id: "0001"
title: RFC Solitario
status: proposed
created: 2026-11-15
summary: Sin índice
---
# RFC
`
		writeDocFile(t, filepath.Join(docsDir, "rfcs"), "0001-rfc-solitario.md", rfc)
		// Crear un INDEX.md vacío o desactualizado
		writeDocFile(t, docsDir, "INDEX.md", "# Índice viejo y desincronizado\n")

		// Ejecutar docgen apuntando al directorio temporal mediante cmd
		cmd := exec.Command("go", "run", filepath.Join(rootDir, "cmd", "docgen"), "--check")
		cmd.Dir = tmp
		out, err := cmd.CombinedOutput()

		// DEBE salir con código de error != 0 (Fail-Closed)
		if err == nil {
			t.Fatalf("el CLI --check debió fallar (Fail-Closed) ante un repo desincronizado, pero salió con código 0.\nSalida: %s", string(out))
		}
	})
}
