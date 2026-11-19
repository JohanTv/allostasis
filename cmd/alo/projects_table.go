package main

import (
	"fmt"
	"strings"

	"github.com/allostasis/alo/internal/domain/issue"
)

// printProjectsTable imprime el listado de proyectos con su pertenencia (compartido por `alo projects` y `alo ticket projects`).
// MIEMBRO: "sí" / "no" (visible en el workspace pero sin pertenecer: el sync lo omite) / "?" (Plane no lo informa).
func printProjectsTable(projects []issue.ProjectRecord) {
	fmt.Printf("%-12s %-36s %-8s %s\n", "PROYECTO", "ID", "MIEMBRO", "NOMBRE")
	fmt.Println(strings.Repeat("─", 80))
	for _, p := range projects {
		member := "?"
		if p.IsMember != nil {
			member = "no"
			if *p.IsMember {
				member = "sí"
			}
		}
		fmt.Printf("%-12s %-36s %-8s %s\n", p.Identifier, p.ProjectID, member, p.Name)
	}
}
