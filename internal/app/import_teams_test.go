package app_test

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// Reprendre au terminal un travail fait en équipe : ce qui suit le préfixe
// nomme une équipe, et les accès du dépôt disent qui en est.

// equipeOrg monte une organisation où un travail d'équipe a été fait sans
// l'outil.
func equipeOrg(t *testing.T) *fakegh.State {
	t.Helper()
	state := fakegh.NewState()
	state.Users["wasim"] = "Wasim Hakkani"
	for nom, membres := range map[string][]string{
		"projet-alpha": {"emilie-cote", "jlpicard"},
		"projet-beta":  {"aminata-d", "wasim"},
	} {
		state.AddRepo("acme", nom, true)
		for _, compte := range membres {
			state.AddCollaborator("acme/"+nom, compte, "push")
		}
		// L'enseignant a accès à tout : il n'est d'aucune équipe.
		state.AddCollaborator("acme/"+nom, "prof", "admin")
	}
	return state
}

// La reprise scriptée renomme les dépôts, compose les équipes, et leur partage
// le leur.
func TestImportEnEquipeEnLigneDeCommande(t *testing.T) {
	h := nouveau(t, equipeOrg(t))
	h.Options.ImportRequested = true
	h.Options.Import = "projet"
	h.Options.Teams = true
	h.Options.Into = "a26.5n6.01"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	noms := h.depots()
	sort.Strings(noms)
	attendus := []string{"a26.5n6.01.projet.alpha", "a26.5n6.01.projet.beta"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("dépôts repris inattendus : %v", noms)
	}

	// Les équipes portent la place du groupe, et leurs membres viennent des
	// accès des dépôts — l'enseignant excepté.
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.alpha"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres d'alpha = %v", membres)
	}
	if slices.Contains(membres, "prof") {
		t.Fatal("l'enseignant ne devrait être d'aucune équipe")
	}
	// Chaque équipe a reçu son dépôt.
	if depots := h.State.TeamRepoNames("acme", fakegh.TeamSlug("a26.5n6.01.beta")); len(depots) != 1 {
		t.Fatalf("beta devrait voir son dépôt : %v", depots)
	}
	h.contient("équipe alpha", "@emilie-cote")
}

// Une simulation ne renomme rien et ne crée aucune équipe.
func TestImportEnEquipeSimule(t *testing.T) {
	h := nouveau(t, equipeOrg(t))
	h.Options.ImportRequested = true
	h.Options.Import = "projet"
	h.Options.Teams = true
	h.Options.Into = "a26.5n6.01"
	h.Options.DryRun = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); !slices.Contains(noms, "projet-alpha") {
		t.Fatalf("rien ne devait être renommé : %v", noms)
	}
	for _, nom := range h.State.TeamNames("acme") {
		if strings.HasPrefix(nom, "a26.") {
			t.Fatalf("aucune équipe ne devait naître : %v", nom)
		}
	}
	h.contient("Reprise en équipe", "alpha", "Simulation")
}

// L'assistant ne pose la question qu'en la justifiant : plusieurs personnes sur
// un même dépôt, c'est le seul indice qu'un travail a été fait en équipe. La
// liste, elle, sert toujours : les équipes disent qui a fait le travail, pas
// comment ces gens s'appellent.
func TestAssistantDemandeSiLeTravailEstEnEquipe(t *testing.T) {
	h := nouveau(t, equipeOrg(t))
	h.Options.ImportRequested = true
	h.Options.Import = "projet"
	h.Options.Into = "a26.5n6.01"
	h.Options.Roster = listeOmnivox(t, t.TempDir(),
		`="1600105";="1010";="Côté";="Émilie";="COTE20059908";`,
		`="2100106";="1010";="Picard";="Jean-Luc";="PICJ68040412";`,
	)

	code, scripte := h.script(
		"tous",   // reprendre tous les dépôts du travail
		"equipe", // oui, c'est un travail d'équipe
		"",       // les compositions trouvées conviennent
		"oui",    // renommer, composer, partager
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if _, pose := scripte.MenuFor("fait en équipe"); !pose {
		t.Fatalf("la question n'a pas été posée :\n%s", h.texte())
	}
	if !slices.Contains(h.depots(), "a26.5n6.01.projet.alpha") {
		t.Fatalf("les dépôts n'ont pas été repris : %v", h.depots())
	}
	// Les comptes ont été nommés par la liste : c'est ce qui manquait.
	h.contient("Rapprochement des comptes", "Émilie Côté", "Jean-Luc Picard")
}

// Un travail individuel ne déclenche pas la question : chaque dépôt n'a qu'une
// personne, et ajouter une étape à tout le monde pour le cas d'un seul serait
// payer cher un doute qui n'existe pas.
func TestAssistantNeDemandeRienSurUnTravailIndividuel(t *testing.T) {
	state := fakegh.NewState()
	for nom, compte := range map[string]string{
		"tp1-emilie-cote": "emilie-cote",
		"tp1-jlpicard":    "jlpicard",
	} {
		state.AddRepo("acme", nom, true)
		state.AddCollaborator("acme/"+nom, compte, "push")
		state.AddCollaborator("acme/"+nom, "prof", "admin")
	}
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.01"

	h.Options.Roster = listeOmnivox(t, t.TempDir(),
		`="1600105";="1010";="Côté";="Émilie";="COTE20059908";`,
	)
	_, scripte := h.script(
		"tous", // reprendre tous les dépôts
		"non",  // rien à corriger
		"oui",  // renommer
	)
	if _, pose := scripte.MenuFor("fait en équipe"); pose {
		t.Fatalf("la question n'avait pas lieu d'être :\n%s", h.texte())
	}
}
