// Package ui rassemble l'affichage et les questions posées au terminal.
// Hors terminal — sortie redirigée, script, journal — rien ne s'affiche en
// couleur ni en animation, et aucun retour chariot n'est émis.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"

	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Console écrit les messages de l'outil.
type Console struct {
	Out   io.Writer
	Color bool // styles actifs
	TTY   bool // animations et retours chariot permis
	Width int
}

// NewConsole construit la console de la sortie standard.
func NewConsole() *Console {
	return NewConsoleFor(os.Stdout)
}

// NewConsoleFor construit une console pour une sortie donnée.
func NewConsoleFor(out io.Writer) *Console {
	tty := false
	if file, ok := out.(*os.File); ok {
		tty = isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
	}
	color := tty && os.Getenv("NO_COLOR") == ""
	if os.Getenv("MILOU_FORCE_COLOR") != "" {
		color = true
	}
	return &Console{Out: out, Color: color, TTY: tty, Width: 100}
}

// Printf écrit une ligne formatée.
func (c *Console) Printf(format string, args ...any) {
	fmt.Fprintf(c.Out, format+"\n", args...)
}

// Print écrit une ligne telle quelle.
func (c *Console) Print(text string) {
	fmt.Fprintln(c.Out, text)
}

// Blank saute une ligne.
func (c *Console) Blank() { fmt.Fprintln(c.Out) }

func (c *Console) style(style lipgloss.Style, text string) string {
	if !c.Color {
		return text
	}
	return style.Render(text)
}

// Bold met en valeur un fragment.
func (c *Console) Bold(text string) string { return c.style(boldStyle, text) }

// Dim atténue un fragment.
func (c *Console) Dim(text string) string { return c.style(dimStyle, text) }

// OK colore un succès.
func (c *Console) OK(text string) string { return c.style(okStyle, text) }

// Warn colore un avertissement.
func (c *Console) Warn(text string) string { return c.style(warnStyle, text) }

// Err colore une erreur.
func (c *Console) Err(text string) string { return c.style(errStyle, text) }

// Info colore une information.
func (c *Console) Info(text string) string { return c.style(infoStyle, text) }

// Heading imprime un titre de section.
func (c *Console) Heading(title string) {
	c.Blank()
	c.Print(c.style(titleStyle, "── "+title+" "+strings.Repeat("─", maxInt(3, 60-len([]rune(title))))))
}

// Banner imprime l'en-tête de l'outil.
func (c *Console) Banner(title, subtitle string) {
	c.Blank()
	c.Print("  " + c.Bold(title))
	if subtitle != "" {
		c.Print("  " + c.Dim(subtitle))
	}
}

// Success, Warning, Failure et Note impriment un message préfixé.
func (c *Console) Success(format string, args ...any) {
	c.Printf("  %s %s", c.OK("✓"), fmt.Sprintf(format, args...))
}

// Warning signale un problème sans interrompre.
func (c *Console) Warning(format string, args ...any) {
	c.Printf("  %s %s", c.Warn("⚠"), fmt.Sprintf(format, args...))
}

// Failure signale un échec.
func (c *Console) Failure(format string, args ...any) {
	c.Printf("  %s %s", c.Err("✗"), fmt.Sprintf(format, args...))
}

// Note écrit une précision discrète.
func (c *Console) Note(format string, args ...any) {
	c.Printf("  %s", c.Dim(fmt.Sprintf(format, args...)))
}

// Table imprime un tableau aligné ; limit borne le nombre de lignes affichées.
func (c *Console) Table(headers []string, rows [][]string, limit int) {
	if len(rows) == 0 {
		return
	}
	shown := rows
	hidden := 0
	if limit > 0 && len(rows) > limit {
		shown = rows[:limit]
		hidden = len(rows) - limit
	}

	widths := make([]int, len(headers))
	for index, header := range headers {
		widths[index] = visibleWidth(header)
	}
	for _, row := range shown {
		for index, cell := range row {
			if index < len(widths) && visibleWidth(cell) > widths[index] {
				widths[index] = visibleWidth(cell)
			}
		}
	}

	c.Blank()
	c.Print("  " + c.Dim(joinCells(headers, widths)))
	separators := make([]string, len(headers))
	for index := range headers {
		separators[index] = strings.Repeat("─", widths[index])
	}
	c.Print("  " + c.Dim(joinCells(separators, widths)))
	for _, row := range shown {
		c.Print("  " + joinCells(row, widths))
	}
	if hidden > 0 {
		c.Note("… et %s de plus.", valid.Plural(hidden, "ligne"))
	}
}

func joinCells(cells []string, widths []int) string {
	parts := make([]string, 0, len(cells))
	for index, cell := range cells {
		width := 0
		if index < len(widths) {
			width = widths[index]
		}
		padding := width - visibleWidth(cell)
		if padding < 0 {
			padding = 0
		}
		parts = append(parts, cell+strings.Repeat(" ", padding))
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

// visibleWidth mesure la largeur affichée, séquences de style exclues.
func visibleWidth(text string) int {
	return lipgloss.Width(text)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
