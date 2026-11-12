package ui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// LinePrompter pose les questions en texte simple : listes numérotées et
// sélections saisies comme une expression. Il prend le relais quand les listes
// aux flèches sont impossibles — sortie redirigée, terminal réduit — mais qu'une
// personne reste au clavier.
type LinePrompter struct {
	console *Console
	reader  *bufio.Reader
	// lastPrefix retient la dernière demande de complétion : redemander le même
	// chemin revient à taper deux tabulations de suite, et liste donc.
	lastPrefix string
}

// NewLinePrompter construit un questionneur en mode ligne.
func NewLinePrompter(console *Console, input io.Reader) *LinePrompter {
	return &LinePrompter{console: console, reader: bufio.NewReader(input)}
}

// Interactive indique que des questions peuvent être posées.
func (p *LinePrompter) Interactive() bool { return true }

// readRaw lit une réponse telle quelle ; la fin du flux vaut interruption.
func (p *LinePrompter) readRaw() (string, error) {
	line, err := p.reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", ErrAborted
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readLine lit une réponse débarrassée de ses espaces.
func (p *LinePrompter) readLine() (string, error) {
	raw, err := p.readRaw()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

func (p *LinePrompter) prompt(text string) {
	fmt.Fprintf(p.console.Out, "  %s ", text)
}

// Ask demande une valeur libre et valide la saisie. En mode ligne, la touche
// de tabulation arrive dans la réponse : elle est traitée comme une demande de
// complétion, à la manière d'un shell.
func (p *LinePrompter) Ask(question Question) (string, error) {
	defaultValue := question.Default
	for {
		title := question.Title
		if defaultValue != "" {
			title += " [" + defaultValue + "]"
		}
		if question.Complete != complete.None {
			title += " (⇥ complète, ⇥⇥ liste)"
		}
		p.prompt(title + " :")
		raw, err := p.readRaw()
		if err != nil {
			return "", err
		}
		if question.Complete != complete.None && strings.Contains(raw, "\t") {
			defaultValue = p.completePath(raw, defaultValue, question.Complete)
			continue
		}
		answer := strings.TrimSpace(raw)
		if answer == "" {
			answer = defaultValue
		}
		if answer == "" {
			if question.AllowEmpty {
				return "", nil
			}
			p.console.Failure("Une valeur est attendue.")
			continue
		}
		if question.Validate == nil {
			return answer, nil
		}
		cleaned, err := question.Validate(answer)
		if err != nil {
			p.console.Failure("%v", err)
			continue
		}
		return cleaned, nil
	}
}

// completePath répond à une tabulation : il complète ce qui est certain, et
// liste les possibilités quand la tabulation est doublée — comme un shell. La
// valeur obtenue sert de départ à la question reposée.
func (p *LinePrompter) completePath(raw, fallback string, mode complete.Mode) string {
	prefix := strings.TrimSpace(strings.SplitN(raw, "\t", 2)[0])
	if prefix == "" {
		prefix = fallback
	}
	// Deux tabulations dans la même saisie, ou la même demande deux fois de
	// suite : dans les deux cas, les possibilités sont attendues. Ce qu'une
	// complétion vient de produire compte comme une demande, si bien qu'après
	// avoir atteint un dossier, une seule tabulation de plus liste son contenu.
	doubled := strings.Count(raw, "\t") > 1 || prefix == p.lastPrefix
	p.lastPrefix = prefix

	suggestions := complete.Suggest(prefix, mode)
	switch {
	case len(suggestions) == 0:
		p.console.Note("Aucune correspondance pour « %s ».", prefix)
		return prefix
	case len(suggestions) == 1:
		p.lastPrefix = suggestions[0]
		return suggestions[0]
	}

	common := commonPrefix(suggestions)
	if !doubled && len(common) > len(prefix) {
		// La partie commune est acquise ; la liste attendra la demande suivante.
		p.lastPrefix = common
		return common
	}
	if !doubled {
		p.console.Note("%d possibilités — une tabulation de plus pour les lister.", len(suggestions))
		return prefix
	}
	for index, suggestion := range suggestions {
		if index == 20 {
			p.console.Note("… et %s.", valid.Plural(len(suggestions)-20, "autre"))
			break
		}
		p.console.Print("    " + suggestion)
	}
	if len(common) > len(prefix) {
		p.lastPrefix = common
		return common
	}
	return prefix
}

// commonPrefix renvoie le plus long préfixe commun d'une liste de chemins.
func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	common := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, common) {
			common = common[:len(common)-1]
			if common == "" {
				return ""
			}
		}
	}
	return common
}

// Confirm pose une question fermée.
func (p *LinePrompter) Confirm(title string, defaultValue bool) (bool, error) {
	hint := "[o/N]"
	if defaultValue {
		hint = "[O/n]"
	}
	for {
		p.prompt(title + " " + hint)
		answer, err := p.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return defaultValue, nil
		case "o", "oui", "y", "yes":
			return true, nil
		case "n", "non", "no":
			return false, nil
		default:
			p.console.Failure("Répondez par « o » ou « n ».")
		}
	}
}

// Choose affiche une liste numérotée et accepte un numéro ou une valeur.
func (p *LinePrompter) Choose(title string, options []Option, defaultValue string) (string, error) {
	if len(options) == 0 {
		return defaultValue, nil
	}
	for {
		p.console.Blank()
		p.console.Print("  " + p.console.Bold(title))
		for index, option := range options {
			// Le chevron du mode flèches, et le mot qui va avec : l'étoile
			// d'usage marquait le défaut sans dire ce qu'elle marquait.
			if option.Value == defaultValue {
				p.console.Printf("   %s%2d  %s %s", cursorMark, index+1,
					p.console.Bold(option.Label), p.console.Dim("(défaut)"))
				continue
			}
			p.console.Printf("     %2d  %s", index+1, option.Label)
		}
		p.prompt("Votre choix (numéro) :")
		answer, err := p.readLine()
		if err != nil {
			return "", err
		}
		if answer == "" && defaultValue != "" {
			return defaultValue, nil
		}
		if number, err := strconv.Atoi(answer); err == nil && number >= 1 && number <= len(options) {
			return options[number-1].Value, nil
		}
		for _, option := range options {
			if strings.EqualFold(option.Value, answer) {
				return option.Value, nil
			}
		}
		p.console.Failure("« %s » ne correspond à aucune entrée (1 à %d).", answer, len(options))
	}
}

// MultiSelect affiche une liste numérotée et accepte une expression :
// « tous », « 1,3 », « 2-5 », un nom, ou un mélange.
func (p *LinePrompter) MultiSelect(title string, options []Option, selected []bool) ([]int, error) {
	if len(options) == 0 {
		return nil, nil
	}
	for {
		p.console.Blank()
		p.console.Print("  " + p.console.Bold(title))
		for index, option := range options {
			p.console.Printf("     %2d  %s", index+1, option.Label)
		}
		p.console.Note("« tous » (défaut), « 1,3 », « 2-5 », un nom, ou un mélange ; « - » pour ne rien choisir.")
		p.prompt("Votre sélection :")
		answer, err := p.readLine()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(answer) == "-" {
			return nil, nil
		}
		indices, err := groups.ParseSelection(answer, len(options), func(token string) int {
			for index, option := range options {
				if strings.EqualFold(option.Value, token) || strings.EqualFold(option.Label, token) {
					return index
				}
			}
			return -1
		})
		if err != nil {
			p.console.Failure("%v", err)
			continue
		}
		return indices, nil
	}
}
