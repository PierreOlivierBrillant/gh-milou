package app_test

import (
	"os"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
)

// « gerer » → « Dépôts » → « ajouter » crée les dépôts de qui n'est pas encore
// servi. Ils doivent naître comme ceux de « creer » et du navigateur : sous le
// même nom, à la même équipe enseignante, et en apprenant au registre à qui ils
// sont.
//
// Aucun dépôt de ces travaux ne vient d'un modèle : la question des fichiers de
// départ est posée, et laissée vide.

// travailDeLaNomenclature prépare « a26.5n6.01.tp1 », où Émilie a déjà son dépôt.
func travailDeLaNomenclature() *fakegh.State {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	return state
}

// Un travail de la nomenclature nomme ses dépôts par elle, au navigateur comme
// dans « creer ». « ajouter » proposait le gabarit retenu pour les préfixes
// hérités, et Jean-Luc recevait « a26.5n6.01.tp1-jlpicard » à côté de
// « a26.5n6.01.tp1.emilie-cote ». Le gabarit retenu, lui, ne bouge pas : il
// reste celui du prochain préfixe hérité.
func TestAjouterNommeLesDepotsParLaNomenclature(t *testing.T) {
	h := gestion(t, travailDeLaNomenclature(), "a26.5n6.01.tp1")
	liste := h.cohorteCSV("Jean-Luc Picard,jlpicard")

	code, scripte := h.script(
		"depots", "ajouter",
		"", // aucun dossier de fichiers de départ
		"fichier", liste,
		"oui", // vérifier le compte
		"oui", // confirmer la création
		"quitter",
	)
	if question, posee := scripte.AskedFor("Gabarit de nom"); posee {
		t.Errorf("le gabarit est demandé, « %s » proposé", question.Default)
	}
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := strings.Join(h.depots(), ","); noms !=
		"a26.5n6.01.tp1.emilie-cote,a26.5n6.01.tp1.jean-luc-picard" {
		t.Fatalf("dépôts = %s\n%s", noms, h.texte())
	}
	h.contient("Gabarit de nom : " + classroom.NamePattern)

	contenu, err := os.ReadFile(h.Reglages)
	if err != nil {
		t.Fatalf("réglages : %v", err)
	}
	if !strings.Contains(string(contenu), `"name_pattern": "{assignment}-{username}"`) {
		t.Errorf("le gabarit retenu a changé : %s", contenu)
	}
}

// Dans un groupe cloisonné, les dépôts ajoutés vont d'emblée à l'équipe
// enseignante, comme ceux que « creer » distribue. Sans elle, ajouter un
// retardataire décloisonnait le groupe sans que personne ne s'en aperçoive.
func TestAjouterAUnGroupeCloisonneDonneLesDepotsAuxEnseignants(t *testing.T) {
	state := travailDeLaNomenclature()
	state.AddTeam("acme", "a26.5n6.01.enseignants", "prof")
	h := gestion(t, state, "a26.5n6.01.tp1")
	liste := h.cohorteCSV("Jean-Luc Picard,jlpicard")

	code, _ := h.script("depots", "ajouter", "", "fichier", liste, "oui", "oui", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	partages := state.TeamRepos["acme/"+fakegh.TeamSlug("a26.5n6.01.enseignants")]
	if _, servie := partages["acme/a26.5n6.01.tp1.jean-luc-picard"]; !servie {
		t.Errorf("l'équipe enseignante n'a pas reçu le dépôt ajouté : %v\n%s",
			partages, h.texte())
	}
}

// Les noms des personnes ajoutées montent au registre, comme ceux d'une
// distribution : c'est lui qui dira, depuis n'importe quel poste, à qui est
// chaque dépôt. Rien n'y monte tant que la création n'est pas confirmée.
func TestAjouterInscritLesNomsAuRegistreUneFoisConfirme(t *testing.T) {
	state := travailDeLaNomenclature()
	nomsDuRegistre := func() string {
		return state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	}

	h := gestion(t, state, "a26.5n6.01.tp1")
	liste := h.cohorteCSV("Jean-Luc Picard,jlpicard")
	if code, _ := h.script("depots", "ajouter", "", "fichier", liste,
		"oui", "non", "quitter"); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if contenu := nomsDuRegistre(); strings.Contains(contenu, "jlpicard") {
		t.Fatalf("une création refusée a écrit au registre :\n%s", contenu)
	}

	h = gestion(t, state, "a26.5n6.01.tp1")
	if code, _ := h.script("depots", "ajouter", "", "fichier", liste,
		"oui", "oui", "quitter"); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	contenu := nomsDuRegistre()
	for _, attendu := range []string{"jlpicard", "Jean-Luc Picard"} {
		if !strings.Contains(contenu, attendu) {
			t.Errorf("« %s » manque au registre :\n%s", attendu, contenu)
		}
	}
}

// Qui a déjà son dépôt du travail est reconnu sous la nomenclature aussi : le
// dernier niveau de ce dépôt est son nom, « jean-luc-picard », et non son
// compte. Le tri le comparait à « jlpicard », tenait Jean-Luc pour neuf et lui
// préparait un second dépôt. Une inscrite sans compte se dit par son matricule,
// pas par un « @ » qui ne désignerait personne.
func TestAjouterReconnaitQuiADejaSonDepotDuTravail(t *testing.T) {
	for _, cas := range []struct {
		nom, depot, liste, designee string
		reponses                    []string
	}{
		{"par son compte", "a26.5n6.01.tp1.jean-luc-picard",
			"nom_complet,github_username\nJean-Luc Picard,jlpicard\n",
			"Jean-Luc Picard (@jlpicard)", []string{"oui"}}, // vérifier le compte
		{"par son matricule", "a26.5n6.01.tp1.naomi-chery",
			"nom_complet,matricule\nNaomi Chéry,2100456\n",
			"Naomi Chéry (2100456)", nil},
	} {
		t.Run(cas.nom, func(t *testing.T) {
			state := fakegh.NewState()
			state.AddRepo("acme", cas.depot, true)
			h := gestion(t, state, "a26.5n6.01.tp1")
			reponses := append([]string{"depots", "ajouter", "", "fichier", h.liste(cas.liste)},
				cas.reponses...)

			code, _ := h.script(append(reponses, "quitter")...)
			if code != app.ExitOK {
				t.Fatalf("code = %d\n%s", code, h.texte())
			}
			h.contient("1 personne déjà présente", cas.designee, "Rien à ajouter")
			h.absent("(@)")
			if noms := strings.Join(h.depots(), ","); noms != cas.depot {
				t.Errorf("dépôts = %s", noms)
			}
		})
	}
}
