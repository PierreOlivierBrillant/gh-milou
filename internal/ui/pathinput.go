package ui

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Bornes d'affichage de la liste des possibilités.
const (
	listedMax     = 60
	defaultWidth  = 80
	columnPadding = 2
)

// pathModel est le champ de saisie des questions attendant un chemin. Il est
// écrit à part de huh pour que la tabulation se comporte comme dans un shell :
// une fois pour compléter, deux fois pour lister les possibilités.
type pathModel struct {
	console  *Console
	question Question
	input    textinput.Model

	suggestions []string
	listed      bool   // la liste des possibilités est affichée
	tabs        int    // tabulations consécutives
	message     string // motif d'un refus
	hint        string // précision discrète sur ce qu'il reste à faire
	width       int

	value   string
	done    bool
	aborted bool
}

func newPathModel(console *Console, question Question) *pathModel {
	field := textinput.New()
	field.Prompt = "> "
	field.SetValue(question.Default)
	field.CursorEnd()
	field.Focus()
	return &pathModel{console: console, question: question, input: field, width: defaultWidth}
}

// Init démarre le clignotement du curseur.
func (m *pathModel) Init() tea.Cmd { return textinput.Blink }

// Update traite les frappes : tabulation, validation, annulation, édition.
func (m *pathModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tea.WindowSizeMsg:
		if typed.Width > 0 {
			m.width = typed.Width
		}
		return m, nil
	case tea.KeyMsg:
		switch typed.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.aborted, m.done = true, true
			return m, tea.Quit
		case tea.KeyEnter:
			return m, m.submit()
		case tea.KeyTab:
			m.completePath()
			return m, nil
		}
		// Toute autre frappe reprend la saisie : la liste n'a plus lieu d'être.
		m.tabs, m.listed, m.message, m.hint = 0, false, "", ""
	}

	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

// submit valide la saisie ; une valeur refusée laisse la question ouverte.
func (m *pathModel) submit() tea.Cmd {
	answer := strings.TrimSpace(m.input.Value())
	if answer == "" {
		if m.question.AllowEmpty {
			m.value, m.done = "", true
			return tea.Quit
		}
		m.message = "Une valeur est attendue."
		return nil
	}
	if m.question.Validate == nil {
		m.value, m.done = answer, true
		return tea.Quit
	}
	cleaned, err := m.question.Validate(answer)
	if err != nil {
		m.message = err.Error()
		return nil
	}
	m.value, m.done = cleaned, true
	return tea.Quit
}

// completePath complète la saisie, puis liste les possibilités à la tabulation
// suivante — le comportement d'un shell. Le compteur de tabulations n'est
// jamais remis à zéro par une complétion : après avoir atteint un dossier, une
// seule tabulation de plus suffit donc à voir ce qu'il contient.
func (m *pathModel) completePath() {
	m.tabs++
	m.message, m.hint = "", ""
	current := m.input.Value()
	m.suggestions = complete.Suggest(current, m.question.Complete)

	if len(m.suggestions) == 0 {
		m.listed = false
		m.message = "Aucune correspondance."
		return
	}
	if len(m.suggestions) == 1 {
		m.listed = false
		if m.suggestions[0] == current {
			m.hint = "Aucune autre possibilité."
			return
		}
		m.setValue(m.suggestions[0])
		return
	}

	// La partie commune à toutes les possibilités est acquise d'emblée ; s'il
	// n'y a plus rien à ajouter, la tabulation suivante les affiche.
	common := commonPrefix(m.suggestions)
	switch {
	case len(common) > len(current):
		m.setValue(common)
		m.listed = false
	case m.tabs >= 2:
		m.listed = true
	default:
		// Sans ce mot, la tabulation semblerait sans effet.
		m.hint = itoa(len(m.suggestions)) + " possibilités — ⇥ pour les lister."
	}
}

func (m *pathModel) setValue(value string) {
	m.input.SetValue(value)
	m.input.CursorEnd()
}

// View dessine le champ, l'aide, un éventuel message et la liste demandée.
func (m *pathModel) View() string {
	if m.done {
		// Une fois répondu, seule une trace compacte reste à l'écran.
		if m.aborted {
			return ""
		}
		return "  " + m.console.Dim(m.question.Title+" :") + " " + m.value + "\n"
	}

	var view strings.Builder
	view.WriteString("  " + m.console.Bold(m.title()) + "\n")
	view.WriteString("  " + m.console.Dim("⇥ complète · ⇥⇥ liste · ↵ valide · échap annule") + "\n")
	view.WriteString(m.input.View() + "\n")
	if m.message != "" {
		view.WriteString("  " + m.console.Err(m.message) + "\n")
	}
	if m.hint != "" {
		view.WriteString("  " + m.console.Dim(m.hint) + "\n")
	}
	if m.listed {
		view.WriteString(m.columns())
	}
	return view.String()
}

func (m *pathModel) title() string {
	if m.question.Default != "" {
		return m.question.Title + " (défaut : " + m.question.Default + ")"
	}
	return m.question.Title
}

// columns dispose les possibilités en colonnes, en n'affichant que le dernier
// segment de chaque chemin — ce qui reste à choisir.
func (m *pathModel) columns() string {
	shown := m.suggestions
	hidden := 0
	if len(shown) > listedMax {
		hidden = len(shown) - listedMax
		shown = shown[:listedMax]
	}

	labels := make([]string, 0, len(shown))
	widest := 0
	for _, suggestion := range shown {
		label := lastSegment(suggestion)
		labels = append(labels, label)
		if width := VisibleWidth(label); width > widest {
			widest = width
		}
	}

	columns := (m.width - 2) / (widest + columnPadding)
	if columns < 1 {
		columns = 1
	}
	var view strings.Builder
	for index, label := range labels {
		if index%columns == 0 {
			view.WriteString("  ")
		}
		padding := widest + columnPadding - VisibleWidth(label)
		if index%columns == columns-1 || index == len(labels)-1 {
			view.WriteString(m.console.Dim(label) + "\n")
			continue
		}
		view.WriteString(m.console.Dim(label) + strings.Repeat(" ", maxInt(1, padding)))
	}
	if hidden > 0 {
		view.WriteString("  " + m.console.Dim("… et "+valid.Plural(hidden, "autre")) + "\n")
	}
	return view.String()
}

// lastSegment isole ce qui distingue une possibilité des autres. Le séparateur
// qui marque les dossiers est conservé tel que la suggestion le porte.
func lastSegment(path string) string {
	trimmed := path
	for len(trimmed) > 0 && os.IsPathSeparator(trimmed[len(trimmed)-1]) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "" {
		return path
	}
	return filepath.Base(trimmed) + path[len(trimmed):]
}

// askPath pose une question de chemin dans son propre champ de saisie.
func (p *HuhPrompter) askPath(question Question) (string, error) {
	model := newPathModel(p.console, question)
	options := []tea.ProgramOption{tea.WithOutput(outputOr(p.output))}
	if p.input != nil {
		options = append(options, tea.WithInput(p.input))
	}
	final, err := tea.NewProgram(model, options...).Run()
	if err != nil {
		return "", err
	}
	answered, ok := final.(*pathModel)
	if !ok || answered.aborted {
		return "", ErrAborted
	}
	return answered.value, nil
}

func outputOr(writer io.Writer) io.Writer {
	if writer != nil {
		return writer
	}
	// Comme huh : les questions vont sur la sortie d'erreur, jamais dans un
	// flux que l'on redirigerait pour en garder la trace.
	return os.Stderr
}

// itoa évite un import pour un simple nombre.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
