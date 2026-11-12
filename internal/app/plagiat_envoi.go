package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/anonymize"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/corpus"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Envoyer des copies à un collègue.
//
// Deux fichiers sortent d'ici, et ils ne partent pas ensemble : l'archive, qui
// ne nomme personne, et la table de correspondance, qui reste. C'est la seule
// chose qui sépare « comparer du code » de « transmettre une liste de noms », et
// elle ne doit pas dépendre de la vigilance de qui envoie — l'outil ne met tout
// simplement pas la table dans l'archive, et il n'y a pas d'option pour l'y
// mettre.
//
// Ce qui n'a pas pu être effacé est montré avant d'écrire quoi que ce soit. Une
// anonymisation n'est jamais complète, et l'écrire au passé — « voici ce qui
// est parti » — arriverait trop tard.

// exporter anonymise les copies du travail géré et les écrit dans un ZIP.
func (m *manageSession) exporter(group *groups.Group, destination string) error {
	console := m.session.Console
	cours, nom, reconnu := m.travail(group)
	if !reconnu {
		return valid.Errorf(
			"« %s » ne dit pas à quel groupe il appartient : l'envoi anonymisé a "+
				"besoin d'un travail d'un groupe déclaré.", group.Prefix)
	}
	chemin, err := cheminDArchive(destination, nom)
	if err != nil {
		return err
	}

	portee, err := corpus.ParseReach(m.session.Options.Reach)
	if err != nil {
		return err
	}
	declarees, err := m.reglesDeComparaison()
	if err != nil {
		return err
	}
	cibles, err := m.plagiatCibles(cours, nom, portee, declarees)
	if err != nil {
		return err
	}
	reglage, err := m.session.Options.inspection()
	if err != nil {
		return err
	}
	requete := plagiarism.Request{
		Assignment: cours.AssignmentID(nom), Org: m.org, Targets: cibles,
		Inspection: reglage, Rules: declarees,
		Kgram: m.session.Options.Kgram, Window: m.session.Options.Window,
		Jobs: m.session.Options.Jobs,
	}

	console.Heading("Envoi anonymisé — " + nom)
	console.Note("Seuls les fichiers que l'inspection retient sont envoyés : ce " +
		"sont ceux qui seront comparés, et les seuls qu'on sache anonymiser.")

	progression := ui.NewProgress(console, "Copies", len(cibles))
	envoi, err := plagiarism.Export(m.session.Client, requete,
		anonymize.Options{Parts: m.session.Options.AnonymizeParts},
		func(done, _ int, id string) { progression.Update(done, id) })
	progression.Clear()
	if err != nil {
		return err
	}

	montrerEnvoi(console, envoi)
	if envoi.Copies == 0 {
		return valid.Errorf("Envoi anonymisé : aucune copie n'a pu être lue.")
	}
	if !m.session.Options.Yes {
		suite, err := m.session.Prompt.Confirm(
			"Écrire l'archive de "+valid.Plural(envoi.Copies, "copie")+" ?", true)
		if err != nil || !suite {
			return err
		}
	}
	return ecrireEnvoi(console, chemin, envoi)
}

// cheminDArchive arrête où écrire, et refuse d'écraser en silence.
func cheminDArchive(destination, travail string) (string, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", valid.Errorf("Envoi anonymisé : donnez le fichier à écrire.")
	}
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		destination = filepath.Join(destination, travail+"-anonymise.zip")
	}
	if filepath.Ext(destination) == "" {
		destination += ".zip"
	}
	return destination, nil
}

// montrerEnvoi dit ce qui a été remplacé, et surtout ce qui a résisté.
func montrerEnvoi(console *ui.Console, envoi *plagiarism.Exported) {
	bundle := envoi.Bundle
	console.Printf("  %s.", valid.Plural(envoi.Copies, "copie préparée", "copies préparées"))
	for _, souci := range envoi.Problems {
		console.Warning("%s : %s", souci.Repo, souci.Reason)
	}

	if len(bundle.Hits) > 0 {
		total := 0
		natures := map[string]int{}
		for _, hit := range bundle.Hits {
			total += hit.Count
			natures[hit.What] += hit.Count
		}
		console.Printf("  %s : %s.",
			valid.Plural(total, "occurrence remplacée", "occurrences remplacées"), enMots(natures))
	}

	if len(bundle.Residues) == 0 {
		console.Note("Le contrôle n'a rien relevé. Cela ne prouve pas que rien " +
			"n'a survécu : une capture d'écran ou un nom écrit autrement lui " +
			"échappent.")
		return
	}
	console.Warning("%s encore à quelqu'un, dans %s. %s avant d'envoyer.",
		valid.Plural(len(bundle.Residues), "chose ressemble", "choses ressemblent"),
		valid.Plural(len(anonymize.Files(bundle.Residues)), "fichier"),
		valid.Agree(len(bundle.Residues), "Regardez-la", "Regardez-les"))
	lignes := make([][]string, 0, len(bundle.Residues))
	for _, residue := range bundle.Residues {
		lignes = append(lignes, []string{
			residue.Path, fmt.Sprintf("%d", residue.Line), residue.Kind, residue.Text,
		})
	}
	console.Table([]string{"Fichier", "Ligne", "Nature", "Vu"}, lignes, 20)
}

// enMots met un décompte par nature en une phrase.
func enMots(natures map[string]int) string {
	parties := make([]string, 0, len(natures))
	for _, nature := range sortedKeysOf(natures) {
		parties = append(parties, fmt.Sprintf("%d %s", natures[nature], nature))
	}
	return strings.Join(parties, ", ")
}

func sortedKeysOf(counts map[string]int) []string {
	noms := make([]string, 0, len(counts))
	for nom := range counts {
		noms = append(noms, nom)
	}
	sort.Strings(noms)
	return noms
}

// ecrireEnvoi pose l'archive et la table, et dit où elles sont.
func ecrireEnvoi(console *ui.Console, chemin string, envoi *plagiarism.Exported) error {
	if err := os.WriteFile(chemin, envoi.Bundle.Zip, 0o600); err != nil {
		return valid.Errorf("Archive « %s » : %v.", chemin, err)
	}

	// La table va à côté, jamais dedans. Son nom le dit, et ses permissions
	// aussi : elle porte des noms d'étudiants.
	base := strings.TrimSuffix(chemin, filepath.Ext(chemin))
	csv, err := envoi.Bundle.Table.CSV()
	if err != nil {
		return err
	}
	cheminCSV := base + "-correspondance.csv"
	if err := os.WriteFile(cheminCSV, csv, 0o600); err != nil {
		return valid.Errorf("Table « %s » : %v.", cheminCSV, err)
	}
	payload, err := json.MarshalIndent(envoi.Bundle.Table, "", "  ")
	if err != nil {
		return err
	}
	cheminJSON := base + "-correspondance.json"
	if err := os.WriteFile(cheminJSON, append(payload, '\n'), 0o600); err != nil {
		return valid.Errorf("Table « %s » : %v.", cheminJSON, err)
	}

	console.Success("Archive écrite : %s", chemin)
	console.Note("Table de correspondance : %s et %s.", cheminCSV, cheminJSON)
	console.Note("La table n'est pas dans l'archive, et ne doit pas l'accompagner : " +
		"c'est elle seule qui dit qui se cache derrière un jeton.")
	return nil
}

// demanderEnvoi demande où écrire l'archive, puis l'écrit.
func (m *manageSession) demanderEnvoi(group *groups.Group) error {
	_, nom, reconnu := m.travail(group)
	if !reconnu {
		nom = group.Prefix
	}
	destination, err := m.session.Prompt.Ask(ui.Question{
		Title:    "Fichier ZIP à écrire",
		Default:  nom + "-anonymise.zip",
		Complete: complete.Path,
	})
	if err != nil {
		return err
	}
	return m.exporter(group, destination)
}
