package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

// Chaque chemin du terminal qui signe un dépôt verse sa marque au registre de
// l'organisation : c'est le seul endroit qui dira un jour à qui elle
// appartient. Le jeton étant tiré au hasard, une marque qui n'y monte pas le
// jour de la distribution est perdue pour de bon.

// marqueVersee est une ligne du registre des marques, telle qu'elle y est écrite.
type marqueVersee struct {
	Assignment string `json:"assignment"`
	Username   string `json:"username"`
	StudentID  string `json:"student_id"`
	Team       string `json:"team"`
	Token      string `json:"token"`
}

// marquesVersees relit le registre des marques de l'organisation.
func (h *harnais) marquesVersees() []marqueVersee {
	h.t.Helper()
	contenu, present := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.MarksFile]
	if !present {
		return nil
	}
	var lu struct {
		Issued []marqueVersee `json:"issued"`
	}
	if err := json.Unmarshal([]byte(contenu), &lu); err != nil {
		h.t.Fatalf("registre des marques illisible : %v\n%s", err, contenu)
	}
	return lu.Issued
}

// marqueDe rend la marque déposée dans le README d'un dépôt.
func (h *harnais) marqueDe(depot string) string {
	h.t.Helper()
	readme := h.State.Files("acme/"+depot, "main")[signature.ReadmeFile]
	token, signe := signature.First([]byte(readme))
	if !signe {
		h.t.Fatalf("« %s » n'est pas signé : %q", depot, readme)
	}
	return signature.Text(token)
}

// liste écrit une liste de classe telle quelle, en-tête compris.
func (h *harnais) liste(contenu string) string {
	h.t.Helper()
	chemin := filepath.Join(h.t.TempDir(), "liste.csv")
	if err := os.WriteFile(chemin, []byte(contenu), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return chemin
}

// « creer » garde l'identifiant d'un travail de la nomenclature, nomme ses
// dépôts par elle comme le navigateur, et verse leurs marques sous lui. Il en
// faisait « a26-5n6-01-tp1 », que le registre refusait : aucune marque n'y
// montait, compte ou pas, et l'avertissement doublait son point final.
func TestCreerVerseLesMarquesDUnTravailDeLaNomenclature(t *testing.T) {
	for _, cas := range []struct {
		nom, liste, depot string
		attendue          marqueVersee
	}{
		{"par compte", "nom_complet,github_username\nJean-Luc Picard,jlpicard\n",
			"a26.5n6.01.tp1.jean-luc-picard",
			marqueVersee{Assignment: "a26.5n6.01.tp1", Username: "jlpicard"}},
		{"par matricule", "nom_complet,matricule\nNadia Pépin,2100110\n",
			"a26.5n6.01.tp1.nadia-pepin",
			marqueVersee{Assignment: "a26.5n6.01.tp1", StudentID: "2100110"}},
	} {
		t.Run(cas.nom, func(t *testing.T) {
			h := nouveau(t, nil)
			h.Options.Roster = h.liste(cas.liste)
			h.Options.Assignment = "a26.5N6.01.tp1"
			h.Options.Yes = true

			if code := h.muet(); code != app.ExitOK {
				t.Fatalf("code = %d\n%s", code, h.texte())
			}
			if noms := h.depots(); strings.Join(noms, ",") != cas.depot {
				t.Fatalf("dépôts = %v\n%s", noms, h.texte())
			}
			marques := h.marquesVersees()
			cas.attendue.Token = h.marqueDe(cas.depot)
			if len(marques) != 1 || marques[0] != cas.attendue {
				t.Fatalf("registre des marques : %+v, attendu %+v\n%s",
					marques, cas.attendue, h.texte())
			}
			h.contient("1 dépôt signé d'une marque invisible")
			h.absent("Marques non enregistrées", "..")
		})
	}
}

// Un préfixe hérité garde son nom et ses dépôts, mais le registre ne range pas
// ses marques : cela se dit, en clair et sans double point. L'avertissement
// reprenait le refus du registre, « …n'est pas un travail de la
// nomenclature.. ».
func TestCreerUnPrefixeHeriteDitPourquoiSesMarquesNeMontentPas(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Jean-Luc Picard,jlpicard")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); strings.Join(noms, ",") != "tp1-jlpicard" {
		t.Fatalf("dépôts = %v", noms)
	}
	h.contient("Marques non enregistrées au registre", "« tp1 » n'en est pas un",
		"rien ne dira à qui appartient une marque")
	h.absent("..", "d'une marque invisible.")
	if marques := h.marquesVersees(); len(marques) != 0 {
		t.Fatalf("aucune marque ne devait monter : %+v", marques)
	}
}

// L'identifiant gardé tel quel rend aussi à « creer » ce que la même
// slugification lui ôtait : une date cible, refusée parce que le travail « ne
// dit pas à quel groupe il appartient », et l'équipe enseignante d'un groupe
// cloisonné, qui ne recevait pas les dépôts qu'on venait de créer.
func TestCreerUnTravailDeLaNomenclatureGardeSaDateEtSonCloisonnement(t *testing.T) {
	state := fakegh.NewState()
	state.AddTeam("acme", "a26.5n6.01.enseignants", "prof")
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Jean-Luc Picard,jlpicard")
	h.Options.Assignment = "a26.5n6.01.tp1"
	h.Options.DueSet, h.Options.Due = true, "2026-10-01"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if contenu := travauxDuRegistre(h); !strings.Contains(contenu, `"id": "a26.5n6.01.tp1"`) ||
		!strings.Contains(contenu, `"due": "2026-10-01"`) {
		t.Errorf("le registre ne porte pas l'échéance :\n%s", contenu)
	}
	partages := h.State.TeamRepos["acme/"+fakegh.TeamSlug("a26.5n6.01.enseignants")]
	if _, servie := partages["acme/a26.5n6.01.tp1.jean-luc-picard"]; !servie {
		t.Errorf("l'équipe enseignante n'a pas reçu le dépôt : %v", partages)
	}
}

// Un gabarit passé en drapeau ne vaut que pour un préfixe hérité : un travail
// de la nomenclature nomme ses dépôts par elle, et le dit plutôt que de
// laisser croire que le drapeau a servi.
func TestCreerUnTravailDeLaNomenclatureNeSuitPasLeGabaritDonne(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Jean-Luc Picard,jlpicard")
	h.Options.Assignment = "a26.5n6.01.tp1"
	h.Options.Pattern = "{assignment}-{username}"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); strings.Join(noms, ",") != "a26.5n6.01.tp1.jean-luc-picard" {
		t.Fatalf("dépôts = %v", noms)
	}
	h.contient("--pattern ne vaut que pour un préfixe hérité")

	// Le gabarit retenu reste celui des préfixes hérités : ce travail-ci n'a
	// pas à changer le nom du prochain.
	contenu, err := os.ReadFile(h.Reglages)
	if err != nil {
		t.Fatalf("réglages : %v", err)
	}
	if !strings.Contains(string(contenu), `"name_pattern": "{assignment}-{username}"`) {
		t.Errorf("réglages = %s", contenu)
	}
}

// Ajouter des dépôts à un groupe, depuis « gerer », les signe comme les autres,
// et leurs marques montent au même registre. Le runner les signait sans que
// rien ne les y verse.
func TestAjouterDesDepotsAuGroupeVerseLeursMarques(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := gestion(t, state, "a26.5n6.01.tp1")
	liste := h.liste("nom_complet,matricule\nNadia Pépin,2100110\n")

	code, _ := h.script(
		"depots", "ajouter",
		"", // aucun dossier de fichiers de départ
		"fichier", liste,
		"oui", // confirmer la création
		"quitter",
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	marques := h.marquesVersees()
	attendue := marqueVersee{
		Assignment: "a26.5n6.01.tp1", StudentID: "2100110",
		Token: h.marqueDe("a26.5n6.01.tp1.nadia-pepin"),
	}
	if len(marques) != 1 || marques[0] != attendue {
		t.Fatalf("registre des marques : %+v, attendu %+v\n%s", marques, attendue, h.texte())
	}
	h.contient("1 dépôt signé d'une marque invisible")
}

// Les dépôts d'équipe sont signés comme ceux des personnes, et leurs marques
// montent sous le nom de l'équipe : ni compte ni matricule ne la désigne, et
// rien ne les versait.
func TestDistribuerAuxEquipesVerseLeursMarques(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "jlpicard")
	h.composer("a26.5n6.01", "eq2", "emilie-cote")

	h.Options.Assignment = "a26.5n6.01.projet"
	h.Options.Teams = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("distribution : code %d\n%s", code, h.texte())
	}

	marques := h.marquesVersees()
	sort.Slice(marques, func(i, j int) bool { return marques[i].Team < marques[j].Team })
	attendues := []marqueVersee{
		{Assignment: "a26.5n6.01.projet", Team: "eq1",
			Token: h.marqueDe("a26.5n6.01.projet.eq1")},
		{Assignment: "a26.5n6.01.projet", Team: "eq2",
			Token: h.marqueDe("a26.5n6.01.projet.eq2")},
	}
	if len(marques) != 2 || marques[0] != attendues[0] || marques[1] != attendues[1] {
		t.Fatalf("registre des marques : %+v, attendu %+v\n%s", marques, attendues, h.texte())
	}
	h.contient("2 dépôts signés d'une marque invisible")
}
