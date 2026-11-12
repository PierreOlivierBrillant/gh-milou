package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/anonymize"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Envoyer des copies à un collègue d'une autre organisation.
//
// L'écran écrit deux fichiers sur cette machine : l'archive, qui ne nomme
// personne, et la table de correspondance, qui reste ici. Écrire n'est pas le
// geste risqué — c'est envoyer qui l'est, et c'est une personne qui le fera,
// hors de l'outil. Ce que l'écran doit donc faire, c'est montrer ce qui a
// résisté à l'anonymisation avant que cette personne n'attache le fichier à un
// courriel.

// envoiInput est ce que la page demande.
type envoiInput struct {
	plagiatInput
	// Destination est le fichier ZIP à écrire, sur cette machine.
	Destination string `json:"destination"`
	// Parts anonymise aussi les fragments de noms pris isolément.
	Parts bool `json:"parts"`
}

// handlePlagiarismExport anonymise les copies d'un travail et les écrit.
func (s *Server) handlePlagiarismExport(writer http.ResponseWriter, request *http.Request) {
	cours, id, repos, err := s.assignmentOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body envoiInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	destination, err := destinationDArchive(body.Destination, cours.ShortName(id))
	if err != nil {
		fail(writer, err)
		return
	}

	declarees := s.rulesOf(cours.Org)
	cibles, err := s.plagiatTargets(cours, id, repos, body.plagiatInput, declarees)
	if err != nil {
		fail(writer, err)
		return
	}
	// Les identités viennent d'ici : c'est ce que l'anonymisation doit effacer,
	// et seul ce poste les connaît toutes.
	requete := plagiarism.Request{
		Assignment: id, Org: cours.Org, Targets: cibles,
		Inspection: body.settings(), Rules: declarees,
		Kgram: body.Kgram, Window: body.Window, Jobs: s.deps.Jobs,
	}
	client := s.deps.Client
	job := s.jobs.Start("envoi",
		valid.Plural(len(cibles), "copie anonymisée", "copies anonymisées")+" de « "+cours.ShortName(id)+" »",
		func(job *Job) (any, error) {
			envoi, err := plagiarism.Export(client, requete,
				anonymize.Options{Parts: body.Parts},
				func(done, total int, nom string) { job.Progress(done, total, nom) })
			if err != nil {
				return nil, err
			}
			for _, souci := range envoi.Problems {
				job.Warn(souci.Repo + " : " + souci.Reason)
			}
			if envoi.Copies == 0 {
				return nil, valid.Errorf("Envoi anonymisé : aucune copie n'a pu être lue.")
			}
			return ecrireEnvoi(destination, envoi)
		})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// ecrireEnvoi pose l'archive et la table, et dit où elles sont.
//
// Les deux fichiers sont écrits côte à côte mais nommés pour qu'on ne les
// confonde pas : « envoi.zip » et « envoi-correspondance.csv ». Les permissions
// de la table sont celles d'un fichier de noms d'étudiants.
func ecrireEnvoi(destination string, envoi *plagiarism.Exported) (map[string]any, error) {
	if err := os.WriteFile(destination, envoi.Bundle.Zip, 0o600); err != nil {
		return nil, valid.Errorf("Archive « %s » : %v.", destination, err)
	}
	base := strings.TrimSuffix(destination, filepath.Ext(destination))

	csv, err := envoi.Bundle.Table.CSV()
	if err != nil {
		return nil, err
	}
	cheminCSV := base + "-correspondance.csv"
	if err := os.WriteFile(cheminCSV, csv, 0o600); err != nil {
		return nil, valid.Errorf("Table « %s » : %v.", cheminCSV, err)
	}
	payload, err := json.MarshalIndent(envoi.Bundle.Table, "", "  ")
	if err != nil {
		return nil, err
	}
	cheminJSON := base + "-correspondance.json"
	if err := os.WriteFile(cheminJSON, append(payload, '\n'), 0o600); err != nil {
		return nil, valid.Errorf("Table « %s » : %v.", cheminJSON, err)
	}

	return map[string]any{
		"path": destination, "table_csv": cheminCSV, "table_json": cheminJSON,
		"copies": envoi.Copies, "hits": envoi.Bundle.Hits,
		"residues": envoi.Bundle.Residues,
		"files":    anonymize.Files(envoi.Bundle.Residues),
		"summary":  anonymize.Summary(envoi.Bundle.Residues),
		"problems": envoi.Problems,
	}, nil
}

// destinationDArchive arrête où écrire.
func destinationDArchive(destination, travail string) (string, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", valid.Errorf("Envoi anonymisé : choisissez le fichier à écrire.")
	}
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		destination = filepath.Join(destination, travail+"-anonymise.zip")
	}
	if filepath.Ext(destination) == "" {
		destination += ".zip"
	}
	return destination, nil
}
