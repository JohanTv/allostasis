package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocs_ConventionsAndMetadata(t *testing.T) {
	rootDir, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot falló: %v", err)
	}
	docsDir := filepath.Join(rootDir, "docs")

	items, docErrors := CollectDocs(docsDir)
	if len(docErrors) > 0 {
		for _, e := range docErrors {
			t.Errorf("Error de convención en docs: %s", e)
		}
	}

	if len(items) == 0 {
		t.Fatalf("No se encontraron documentos en %s", docsDir)
	}
}

func TestDocs_RelativeLinks(t *testing.T) {
	rootDir, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot falló: %v", err)
	}
	docsDir := filepath.Join(rootDir, "docs")

	linkErrors := CheckRelativeLinks(docsDir)
	if len(linkErrors) > 0 {
		for _, e := range linkErrors {
			t.Errorf("Enlace roto o inválido: %s", e)
		}
	}
}

func TestDocs_IndexIsUpToDate(t *testing.T) {
	rootDir, err := FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot falló: %v", err)
	}
	docsDir := filepath.Join(rootDir, "docs")
	indexPath := filepath.Join(docsDir, "INDEX.md")

	items, docErrors := CollectDocs(docsDir)
	if len(docErrors) > 0 {
		t.Fatalf("No se puede validar el índice debido a errores previos en documentos: %v", docErrors)
	}

	expected := RenderIndex(items)
	actualBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("Error leyendo %s: %v", indexPath, err)
	}

	if string(actualBytes) != expected {
		t.Errorf("docs/INDEX.md está desactualizado. Ejecuta 'make docs' o 'go run ./cmd/docgen' para regenerarlo")
	}
}

func TestDocgen_ParseFrontMatter(t *testing.T) {
	raw := `---
id: "0099"
title: "Título de prueba: con caracteres especiales"
status: proposed
created: 2026-11-15
summary: 'Resumen entre comillas simples'
---
# Contenido
`
	meta, err := ParseFrontMatter([]byte(raw))
	if err != nil {
		t.Fatalf("ParseFrontMatter falló: %v", err)
	}

	if meta["id"] != "0099" {
		t.Errorf("esperado id 0099, obtenido %s", meta["id"])
	}
	if meta["title"] != "Título de prueba: con caracteres especiales" {
		t.Errorf("esperado título con caracteres especiales, obtenido %s", meta["title"])
	}
	if meta["summary"] != "Resumen entre comillas simples" {
		t.Errorf("esperado resumen sin comillas simples, obtenido %s", meta["summary"])
	}
}

func TestDocgen_NextAvailableID(t *testing.T) {
	items := []DocItem{
		{ID: "0002"},
		{ID: "0005"},
		{ID: "0028"},
	}
	next := NextAvailableID(items)
	if next != 29 {
		t.Errorf("esperado nextID 29, obtenido %d", next)
	}
}
