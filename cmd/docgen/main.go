package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	filePattern  = regexp.MustCompile(`^(\d{4})-[a-z0-9]+(-[a-z0-9]+)*\.md$`)
	datePattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	linkPattern  = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	requiredKeys = []string{"id", "title", "status", "created", "summary"}

	validStatuses = map[string]map[string]bool{
		"decisions": {
			"accepted":    true,
			"aceptado":    true,
			"superseded":  true,
			"reemplazado": true,
			"deprecated":  true,
			"draft":       true,
			"borrador":    true,
		},
		"rfcs": {
			"draft":        true,
			"borrador":     true,
			"proposed":     true,
			"propuesto":    true,
			"accepted":     true,
			"aceptado":     true,
			"implemented":  true,
			"implementado": true,
			"rejected":     true,
			"rechazado":    true,
			"superseded":   true,
			"reemplazado":  true,
		},
	}
)

type DocItem struct {
	Kind     string // "decisions" or "rfcs"
	Filename string // e.g., "0002-worktrees-multi-repo-por-ticket.md"
	RelPath  string // e.g., "rfcs/0002-worktrees-multi-repo-por-ticket.md"
	ID       string // e.g., "0002"
	Title    string
	Status   string
	Created  string
	Summary  string
}

func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no se encontró la raíz del repositorio (go.mod no hallado)")
}

func ParseFrontMatter(content []byte) (map[string]string, error) {
	data := make(map[string]string)
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return data, fmt.Errorf("el archivo no comienza con el bloque '---' de front-matter")
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return data, fmt.Errorf("bloque front-matter '---' no cerrado")
	}

	for i := 1; i < endIdx; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
				v = v[1 : len(v)-1]
			}
			data[k] = v
		}
	}

	return data, nil
}

func CollectDocs(docsDir string) ([]DocItem, []string) {
	var items []DocItem
	var errs []string
	seenIDs := make(map[string]string)

	kinds := []string{"decisions", "rfcs"}
	for _, kind := range kinds {
		folder := filepath.Join(docsDir, kind)
		entries, err := os.ReadDir(folder)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Sprintf("error leyendo carpeta %s: %v", kind, err))
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			name := entry.Name()
			relPath := filepath.Join(kind, name)

			if !filePattern.MatchString(name) {
				errs = append(errs, fmt.Sprintf("%s: nombre no sigue convención NNNN-slug-en-minusculas.md", relPath))
				continue
			}

			fullPath := filepath.Join(folder, name)
			content, err := os.ReadFile(fullPath)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: error leyendo archivo: %v", relPath, err))
				continue
			}

			meta, err := ParseFrontMatter(content)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", relPath, err))
				continue
			}

			// Validar campos requeridos
			for _, req := range requiredKeys {
				if meta[req] == "" {
					errs = append(errs, fmt.Sprintf("%s: falta el campo obligatorio '%s' en front-matter", relPath, req))
				}
			}

			// Validar coincidencia de ID
			idPrefix := name[:4]
			if meta["id"] != idPrefix {
				errs = append(errs, fmt.Sprintf("%s: 'id' (%s) no coincide con prefijo del archivo (%s)", relPath, meta["id"], idPrefix))
			}

			// Validar fecha
			if !datePattern.MatchString(meta["created"]) {
				errs = append(errs, fmt.Sprintf("%s: 'created' (%s) no tiene formato YYYY-MM-DD", relPath, meta["created"]))
			} else if _, err := time.Parse("2006-01-02", meta["created"]); err != nil {
				errs = append(errs, fmt.Sprintf("%s: 'created' (%s) no es una fecha válida de calendario: %v", relPath, meta["created"], err))
			}

			// Validar estado
			validForKind := validStatuses[kind]
			if !validForKind[meta["status"]] {
				errs = append(errs, fmt.Sprintf("%s: estado '%s' no es válido para %s", relPath, meta["status"], kind))
			}

			// Validar unicidad de ID
			if prev, exists := seenIDs[idPrefix]; exists {
				errs = append(errs, fmt.Sprintf("ID duplicado '%s': en %s y %s", idPrefix, prev, relPath))
			} else {
				seenIDs[idPrefix] = relPath
			}

			items = append(items, DocItem{
				Kind:     kind,
				Filename: name,
				RelPath:  relPath,
				ID:       idPrefix,
				Title:    meta["title"],
				Status:   meta["status"],
				Created:  meta["created"],
				Summary:  meta["summary"],
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})

	return items, errs
}

func FindDocsRoot(docsDir string) string {
	absDocs, err := filepath.Abs(docsDir)
	if err != nil {
		absDocs = docsDir
	}
	curr := absDocs
	for {
		if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	if filepath.Base(absDocs) == "docs" {
		return filepath.Dir(absDocs)
	}
	return absDocs
}

func CheckRelativeLinks(docsDir string) []string {
	var errs []string
	repoRoot := FindDocsRoot(docsDir)

	err := filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: error leyendo archivo: %v", path, err))
			return nil
		}

		relFile, _ := filepath.Rel(docsDir, path)
		matches := linkPattern.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			if len(m) < 3 {
				continue
			}
			rawTarget := strings.TrimSpace(m[2])

			// 1. Enlace vacío
			if rawTarget == "" {
				errs = append(errs, fmt.Sprintf("%s: enlace vacío '[]()'", relFile))
				continue
			}

			// Soporte para delimitadores <url>
			if strings.HasPrefix(rawTarget, "<") && strings.HasSuffix(rawTarget, ">") {
				rawTarget = strings.TrimPrefix(strings.TrimSuffix(rawTarget, ">"), "<")
			}

			// Soporte para títulos opcionales en Markdown: [text](url "title")
			if idx := strings.IndexAny(rawTarget, " \t"); idx != -1 {
				rest := strings.TrimSpace(rawTarget[idx:])
				if (strings.HasPrefix(rest, `"`) && strings.HasSuffix(rest, `"`)) ||
					(strings.HasPrefix(rest, `'`) && strings.HasSuffix(rest, `'`)) {
					rawTarget = strings.TrimSpace(rawTarget[:idx])
				}
			}

			// Enlace con espacios en blanco no permitidos
			if strings.ContainsAny(rawTarget, " \t\r\n") {
				errs = append(errs, fmt.Sprintf("%s: enlace con espacios en blanco no permitidos: '%s'", relFile, rawTarget))
				continue
			}

			// Ignorar esquemas web
			if strings.HasPrefix(rawTarget, "http://") || strings.HasPrefix(rawTarget, "https://") ||
				strings.HasPrefix(rawTarget, "mailto:") {
				continue
			}

			// Prohibir file:/// ya que viola la portabilidad (ADR-0007)
			if strings.HasPrefix(rawTarget, "file://") {
				errs = append(errs, fmt.Sprintf("%s: enlace absoluto 'file://' no portable (usa rutas relativas): %s", relFile, rawTarget))
				continue
			}

			// Prohibir enlaces absolutos con '/'
			if strings.HasPrefix(rawTarget, "/") {
				errs = append(errs, fmt.Sprintf("%s: enlace absoluto con '/' no permitido (usa rutas relativas): %s", relFile, rawTarget))
				continue
			}

			// Validación de anclas y fragmentos
			targetFile := rawTarget
			if strings.Contains(rawTarget, "#") {
				parts := strings.Split(rawTarget, "#")
				if len(parts) > 2 {
					errs = append(errs, fmt.Sprintf("%s: ancla fragmentada o malformada (múltiples '#'): '%s'", relFile, rawTarget))
					continue
				}
				targetFile = parts[0]
				anchor := parts[1]
				if anchor == "" {
					errs = append(errs, fmt.Sprintf("%s: ancla vacía en enlace: '%s'", relFile, rawTarget))
					continue
				}
				if strings.ContainsAny(anchor, " \t\r\n<>\"'\\^`{}|") {
					errs = append(errs, fmt.Sprintf("%s: ancla con caracteres inválidos: '%s'", relFile, rawTarget))
					continue
				}
				if targetFile == "" {
					// Ancla intra-documento válida (ej. [Context](#context))
					continue
				}
			}

			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), targetFile))

			// Detección de Path Traversal fuera del repositorio
			relToRoot, err := filepath.Rel(repoRoot, resolved)
			if err != nil || strings.HasPrefix(relToRoot, "..") {
				errs = append(errs, fmt.Sprintf("%s: enlace hacia el exterior del repositorio no permitido (path traversal): %s", relFile, targetFile))
				continue
			}

			if _, err := os.Stat(resolved); err != nil {
				errs = append(errs, fmt.Sprintf("%s: enlace roto a '%s' (no existe: %s)", relFile, targetFile, resolved))
			}
		}

		return nil
	})

	if err != nil {
		errs = append(errs, fmt.Sprintf("error caminando directorio docs: %v", err))
	}

	return errs
}

func RenderIndex(items []DocItem) string {
	var buf bytes.Buffer

	buf.WriteString("# Development Documentation Index\n\n")
	buf.WriteString("> Canonical index of Architecture Decision Records (ADRs) and Requests for Comments (RFCs) for Allostasis (`alo`).  \n")
	buf.WriteString("> **Notice:** This document is automatically generated by `cmd/docgen`; do not edit manually.  \n")
	buf.WriteString("> All documents follow the architecture specification in [ARCHITECTURE.md](ARCHITECTURE.md) and backlog in [BACKLOG.md](BACKLOG.md).\n\n")
	buf.WriteString("---\n\n")

	// 1. ADRs
	buf.WriteString("## 1. Architecture Decision Records (ADRs)\n\n")
	buf.WriteString("| ID | Title | Status | Summary |\n")
	buf.WriteString("| :--- | :--- | :---: | :--- |\n")
	for _, item := range items {
		if item.Kind == "decisions" {
			title := strings.ReplaceAll(item.Title, "|", "\\|")
			title = strings.ReplaceAll(title, "\n", " ")
			summary := strings.ReplaceAll(item.Summary, "|", "\\|")
			summary = strings.ReplaceAll(summary, "\n", " ")
			buf.WriteString(fmt.Sprintf("| **%s** | [%s](%s) | %s | %s |\n",
				item.ID, title, item.RelPath, item.Status, summary))
		}
	}

	buf.WriteString("\n---\n\n")

	// 2. RFCs
	buf.WriteString("## 2. Requests for Comments (RFCs)\n\n")
	buf.WriteString("| ID | Title | Status | Summary |\n")
	buf.WriteString("| :--- | :--- | :---: | :--- |\n")
	for _, item := range items {
		if item.Kind == "rfcs" {
			title := strings.ReplaceAll(item.Title, "|", "\\|")
			title = strings.ReplaceAll(title, "\n", " ")
			summary := strings.ReplaceAll(item.Summary, "|", "\\|")
			summary = strings.ReplaceAll(summary, "\n", " ")
			buf.WriteString(fmt.Sprintf("| **%s** | [%s](%s) | %s | %s |\n",
				item.ID, title, item.RelPath, item.Status, summary))
		}
	}

	buf.WriteString("\n---\n\n")

	// 3. Principles & References
	buf.WriteString("## 3. Principles & Operational References\n\n")
	buf.WriteString("- 🧭 [**Design Principles of Allostasis**](principles.md): Fundamentos ontológicos y principios rectores del producto (P1 a P5, Ley de Little, Medida de Lebesgue de Klee).\n")
	buf.WriteString("- 📚 [**Suite de Referencias y Gobernanza Operativa**](references/README.md):\n")
	buf.WriteString("  - [Protocolo de Living Tickets y Fases del SDLC](references/PROTOCOLO_LIVING_TICKETS_Y_SDLC.md)\n")
	buf.WriteString("  - [Protocolo de Relevo Asíncrono de Tareas (Task Handoff)](references/PROTOCOLO_RELEVO_ASINCRONO_HANDOFF.md)\n")
	buf.WriteString("  - [Matriz de Taxonomía y Protocolo de Tickets Paraguas](references/MATRIZ_TAXONOMIA_Y_TICKETS_PARAGUAS.md)\n")
	buf.WriteString("  - [Estrategia Integral de Gestión TI y Work Items](references/ESTRATEGIA_GESTION_TI_Y_WORKITEMS.md)\n")
	buf.WriteString("  - [Gobernanza y Principios de Diseño del CLI](references/CLI_DESIGN_GUARD.md)\n")

	return buf.String()
}

func NextAvailableID(items []DocItem) int {
	maxID := 0
	for _, item := range items {
		if val, err := strconv.Atoi(item.ID); err == nil && val > maxID {
			maxID = val
		}
	}
	return maxID + 1
}

func CreateDocScaffold(docsDir, kind, slug, title string) (string, error) {
	if kind != "decisions" && kind != "rfcs" {
		return "", fmt.Errorf("tipo de documento inválido '%s': debe ser 'decisions' o 'rfcs'", kind)
	}

	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		return "", fmt.Errorf("el título no puede estar vacío")
	}

	cleanSlug := strings.ToLower(strings.TrimSpace(slug))
	cleanSlug = strings.ReplaceAll(cleanSlug, " ", "-")
	if cleanSlug == "" {
		return "", fmt.Errorf("el slug no puede estar vacío")
	}

	items, docErrs := CollectDocs(docsDir)
	if len(docErrs) > 0 {
		return "", fmt.Errorf("no se puede crear scaffold porque existen documentos con errores: %s", strings.Join(docErrs, "; "))
	}

	nextID := NextAvailableID(items)
	idStr := fmt.Sprintf("%04d", nextID)

	filename := fmt.Sprintf("%s-%s.md", idStr, cleanSlug)
	if !filePattern.MatchString(filename) {
		return "", fmt.Errorf("el slug '%s' genera un nombre de archivo inválido '%s' (debe cumplir %s)", slug, filename, filePattern.String())
	}

	targetDir := filepath.Join(docsDir, kind)
	filePath := filepath.Join(targetDir, filename)

	if _, err := os.Stat(filePath); err == nil {
		return "", fmt.Errorf("el archivo ya existe: %s", filePath)
	}

	today := strings.TrimSpace(os.Getenv("DOC_DATE"))
	if today == "" {
		today = "2026-11-15"
	}

	content := fmt.Sprintf(`---
id: "%s"
title: %s
status: %s
created: %s
summary: Resumen breve de la propuesta o decisión técnica.
---

# %s · %s

Related: [BACKLOG.md](../BACKLOG.md), [principles.md](../principles.md).

## Context

Describe la necesidad u origen técnico de esta propuesta.

## Decision / Proposal

Detalla los cambios arquitecturales, interfaces y contratos.

## Consequences

- Consecuencias positivas o simplificaciones operativas.
- Riesgos mitigados o compensaciones técnicas.
`, idStr, trimmedTitle, map[string]string{"decisions": "accepted", "rfcs": "proposed"}[kind], today, idStr, trimmedTitle)

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", err
	}

	return filePath, nil
}

func main() {
	checkFlag := flag.Bool("check", false, "Verifica la consistencia documental sin modificar docs/INDEX.md")
	nextIDFlag := flag.Bool("next-id", false, "Calcula e imprime el próximo ID secuencial de 4 dígitos")
	newKind := flag.String("new", "", "Crea un nuevo documento de desarrollo: 'rfc' o 'decision'")
	titleFlag := flag.String("title", "", "Título del nuevo documento al usar --new")

	flag.Parse()

	rootDir, err := FindRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	docsDir := filepath.Join(rootDir, "docs")
	indexPath := filepath.Join(docsDir, "INDEX.md")

	items, docErrors := CollectDocs(docsDir)
	linkErrors := CheckRelativeLinks(docsDir)

	if *nextIDFlag {
		fmt.Printf("%04d\n", NextAvailableID(items))
		return
	}

	if *newKind != "" {
		kind := strings.ToLower(*newKind)
		folder := ""
		if kind == "rfc" || kind == "rfcs" {
			folder = "rfcs"
		} else if kind == "decision" || kind == "decisions" || kind == "adr" {
			folder = "decisions"
		} else {
			fmt.Fprintf(os.Stderr, "Error: --new requiere 'rfc' o 'decision'\n")
			os.Exit(1)
		}

		args := flag.Args()
		if len(args) < 1 {
			fmt.Fprintf(os.Stderr, "Error: se requiere el slug del documento. Ej: go run ./cmd/docgen --new rfc tui-navigator --title \"TUI Navigator\"\n")
			os.Exit(1)
		}
		slug := args[0]
		title := *titleFlag
		if title == "" {
			title = strings.Title(strings.ReplaceAll(slug, "-", " "))
		}

		createdPath, err := CreateDocScaffold(docsDir, folder, slug, title)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creando documento: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✔ Documento creado: %s\n", createdPath)

		// Regenerar índice automáticamente
		newItems, _ := CollectDocs(docsDir)
		rendered := RenderIndex(newItems)
		_ = os.WriteFile(indexPath, []byte(rendered), 0644)
		fmt.Println("✔ docs/INDEX.md actualizado.")
		return
	}

	hasErrors := false
	if len(docErrors) > 0 {
		hasErrors = true
		fmt.Fprintln(os.Stderr, "❌ Errores en metadatos o convenciones de documentos:")
		for _, e := range docErrors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
	}

	if len(linkErrors) > 0 {
		hasErrors = true
		fmt.Fprintln(os.Stderr, "❌ Enlaces rotos o no portables detectados en docs/:")
		for _, e := range linkErrors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
	}

	expectedIndex := RenderIndex(items)

	if *checkFlag {
		currentIndex, err := os.ReadFile(indexPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Falta docs/INDEX.md o no se pudo leer: %v\n", err)
			os.Exit(1)
		}
		if string(currentIndex) != expectedIndex {
			hasErrors = true
			fmt.Fprintln(os.Stderr, "❌ docs/INDEX.md está desactualizado respecto a docs/decisions y docs/rfcs.")
			fmt.Fprintln(os.Stderr, "   Ejecuta 'make docs' o 'go run ./cmd/docgen' para regenerarlo.")
		}

		if hasErrors {
			os.Exit(1)
		}
		fmt.Println("✔ Todos los documentos, enlaces e índice están en perfecto estado de consistencia.")
		return
	}

	if hasErrors {
		fmt.Fprintln(os.Stderr, "\nNo se puede generar docs/INDEX.md debido a los errores anteriores.")
		os.Exit(1)
	}

	if err := os.WriteFile(indexPath, []byte(expectedIndex), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error escribiendo %s: %v\n", indexPath, err)
		os.Exit(1)
	}

	fmt.Printf("✔ docs/INDEX.md regenerado exitosamente (%d documentos indexados).\n", len(items))
}
