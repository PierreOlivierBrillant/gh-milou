package app

import (
	"os"
	"path/filepath"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// starterLoad isole le chargement d'un dossier de départ, pour la lisibilité.
func starterLoad(path string) (*starter.Bundle, error) { return starter.Load(path) }

// location décrit un fichier géré par l'outil.
type location struct {
	Label string
	Path  string
	State string
}

// locations renvoie l'emplacement et l'état des fichiers gérés par l'outil.
func (s *Session) locations() []location {
	reportDir, err := roster.ExpandPath(s.Options.ReportDir)
	if err != nil {
		reportDir = s.Options.ReportDir
	}
	reports := "aucun bilan"
	if matches, err := filepath.Glob(filepath.Join(reportDir, "*.json")); err == nil && len(matches) > 0 {
		reports = valid.Plural(len(matches), "bilan")
	}
	settingsState := "absent"
	if info, err := os.Stat(s.ConfigFile); err == nil && !info.IsDir() {
		settingsState = "présent"
	}
	return []location{
		{"Réglages", s.ConfigFile, settingsState},
		{"Cache", s.Cache.Path(), s.Cache.Describe()},
		{"Bilans", reportDir, reports},
	}
}

// advancedMenu ouvre les options avancées, accessibles sans authentification.
func (s *Session) advancedMenu() error {
	for {
		s.Console.Heading("Options avancées")
		for _, item := range s.locations() {
			s.Console.Printf("  %s %s  %s", s.Console.Dim(pad(item.Label, 10)), item.Path,
				s.Console.Dim("("+item.State+")"))
		}

		action, err := s.Prompt.Choose("Que faire ?", ui.Options(
			"vider", "Vider le cache local",
			"emplacements", "Afficher les emplacements des fichiers",
			"portees", "Portées du jeton GitHub",
			"equipe", "Donner à une équipe accès au registre des étudiants",
			"registre", "Effacer l'historique du registre des étudiants",
			"reglages", "Oublier les réglages mémorisés",
			"revenir", "Revenir au menu principal",
		), "revenir")
		if err != nil {
			return err
		}
		switch action {
		case "revenir":
			return nil
		case "vider":
			if err := s.clearCache(); err != nil {
				return err
			}
		case "emplacements":
			s.showLocations()
		case "portees":
			if err := s.manageScopes(); err != nil {
				return err
			}
		case "equipe":
			if err := s.grantTeamFromMenu(); err != nil {
				return err
			}
		case "registre":
			if err := s.forgetHistoryFromMenu(); err != nil {
				return err
			}
		case "reglages":
			if err := s.forgetSettings(); err != nil {
				return err
			}
		}
	}
}

func (s *Session) clearCache() error {
	count, _ := s.Cache.Stats()
	if count == 0 {
		s.Console.Note("Le cache est déjà vide.")
		return nil
	}
	confirmed, err := s.Prompt.Confirm(valid.Agree(count, "Supprimer l'entrée du cache ?",
		"Supprimer les "+itoa(count)+" entrées du cache ?"), true)
	if err != nil {
		return err
	}
	if !confirmed {
		s.Console.Warning("Annulé.")
		return nil
	}
	removed := s.Cache.Clear()
	// Le cache mémoire de la session doit disparaître avec le cache disque.
	s.invalidateCaches()
	s.Console.Success("%s.", valid.Plural(removed, "entrée supprimée", "entrées supprimées"))
	return nil
}

func (s *Session) showLocations() {
	s.Console.Blank()
	for _, item := range s.locations() {
		s.Console.Print("  " + s.Console.Bold(item.Label))
		s.Console.Print("    " + item.Path)
		s.Console.Print("    " + s.Console.Dim(item.State))
	}
}

func (s *Session) forgetSettings() error {
	if info, err := os.Stat(s.ConfigFile); err != nil || info.IsDir() {
		s.Console.Note("Aucun réglage mémorisé.")
		return nil
	}
	s.Console.Note("%s", s.ConfigFile)
	confirmed, err := s.Prompt.Confirm("Oublier les réglages mémorisés ?", false)
	if err != nil {
		return err
	}
	if !confirmed {
		s.Console.Warning("Annulé.")
		return nil
	}
	if err := os.Remove(s.ConfigFile); err != nil {
		s.Console.Failure("Suppression impossible : %v", err)
		return nil
	}
	s.resetSettings()
	s.Console.Success("Réglages oubliés.")
	return nil
}
