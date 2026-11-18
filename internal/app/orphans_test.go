package app_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// cohorteTrouee monte l'organisation telle qu'elle se présente vraiment : des
// dépôts dont le dernier niveau est un compte GitHub que rien ne déclare, l'un
// d'eux sous la marque de doublon de GitHub, et un autre qui n'est le compte
// de personne.
func cohorteTrouee(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	state.Users["wriker"] = "William Riker"
	state.Users["dtroi"] = "" // un compte réel, sans nom de profil
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.tp1.wriker",
		"a26.5n6.01.tp2.wriker",
		"a26.5n6.01.tp1.dtroi-1",
		"a26.5n6.01.tp1.parti-sans-compte",
	} {
		state.AddRepo("acme", nom, true).PushedAt = "2026-10-02T10:00:00Z"
	}
	h := nouveau(t, state)
	h.Options.StudentsRequested = true
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	return h
}

// registreEcrit rend le fichier des utilisateurs du registre, tel qu'il a été
// poussé.
func (h *harnais) registreEcrit() string {
	return h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
}

// L'annuaire dit combien de dépôts ne se rattachent à personne, et par où les
// rattacher : sans cela, le compte ne mène nulle part.
func TestAnnuaireDitCommentRattacherLesDepotsSansEtudiant(t *testing.T) {
	h := cohorteTrouee(t)
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("4 dépôts ne se rattachent à aucun étudiant connu", "--adopt-orphans")
}

// « --adopt-orphans -y » rattache ce que GitHub reconnaît, marque de doublon
// comprise, et nomme ce qu'il ne reconnaît pas avec la façon d'y remédier.
func TestAdoptOrphansRattacheLesComptesQueGithubReconnait(t *testing.T) {
	h := cohorteTrouee(t)
	h.Options.AdoptOrphans, h.Options.Yes = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("@wriker", "William Riker", "@dtroi", "profil sans nom",
		"3 dépôts rattachés à leur étudiant, sous 2 comptes",
		"parti-sans-compte", "aucun compte", "--attach")
	ecrit := h.registreEcrit()
	for _, compte := range []string{`"wriker"`, `"dtroi"`} {
		if !strings.Contains(ecrit, compte) {
			t.Errorf("le registre n'a pas appris %s :\n%s", compte, ecrit)
		}
	}
	if strings.Contains(ecrit, "parti-sans-compte") {
		t.Errorf("un slug que GitHub ne reconnaît pas ne doit rien écrire :\n%s", ecrit)
	}
}

// En script, rien ne s'écrit sans « --yes », et la simulation montre sans
// écrire : c'est la règle de tout ce qui écrit dans cet outil.
func TestAdoptOrphansNEcritRienSansConsentement(t *testing.T) {
	h := cohorteTrouee(t)
	h.Options.AdoptOrphans = true
	if code := h.muet(); code == app.ExitOK {
		t.Fatalf("sans --yes, le script doit s'arrêter\n%s", h.texte())
	}
	h.contient("--yes")

	simule := nouveauDansLeMemeDossier(t, h)
	simule.Options.StudentsRequested, simule.Options.AdoptOrphans = true, true
	simule.Options.DryRun = true
	if code := simule.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, simule.texte())
	}
	simule.contient("@wriker", "Simulation : rien n'a été écrit.")
	if strings.Contains(h.registreEcrit(), "wriker") {
		t.Errorf("rien ne devait s'écrire :\n%s", h.registreEcrit())
	}
}

// Dans l'assistant, on écarte ce qui ne doit pas se rattacher — un compte qui
// porte le slug d'un nom sans être l'étudiant —, puis on rattache à la main ce
// que GitHub ne reconnaît pas.
func TestLAssistantLaisseChoisirPuisRattacherALaMain(t *testing.T) {
	h := cohorteTrouee(t)
	h.State.Users["jlpicard"] = "Jean-Luc Picard"
	code, _ := h.script("rattacher", "wriker", "oui", "jlpicard", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("2 dépôts rattachés à leur étudiant, sous 1 compte",
		"1 dépôt rattaché à Jean-Luc Picard (@jlpicard)")
	ecrit := h.registreEcrit()
	if strings.Contains(ecrit, `"dtroi"`) {
		t.Errorf("le compte décoché ne devait pas s'écrire :\n%s", ecrit)
	}
	if !strings.Contains(ecrit, "parti-sans-compte") {
		t.Errorf("le dernier niveau rattaché à la main doit rester au registre :\n%s", ecrit)
	}
}

// « --attach » rattache à la main ce que GitHub ne reconnaît pas. Le nom vient
// du profil : le redemander ferait saisir deux fois la même chose.
func TestAttachRattacheUnDernierNiveauALaMain(t *testing.T) {
	h := cohorteTrouee(t)
	h.State.Users["jlpicard"] = "Jean-Luc Picard"
	h.Options.Attach = "parti-sans-compte=jlpicard"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("1 dépôt rattaché à Jean-Luc Picard (@jlpicard)")
	if !strings.Contains(h.registreEcrit(), "parti-sans-compte") {
		t.Errorf("le registre doit retenir le dernier niveau :\n%s", h.registreEcrit())
	}
}

// Un drapeau mal écrit, un dernier niveau qui n'est pas sans étudiant, et un
// compte qui n'existe pas sont refusés : chacun écrirait au registre quelque
// chose qu'on n'a pas vu.
func TestAttachRefuseCeQuiNeDesignePersonne(t *testing.T) {
	for _, cas := range []struct{ valeur, attendu string }{
		{"parti-sans-compte", "dernier-niveau=compte"},
		{"emilie-cote=wriker", "« emilie-cote » n'est le dernier niveau d'aucun dépôt"},
		{"parti-sans-compte=personne-de-tel", "n'existe pas sur GitHub"},
	} {
		h := cohorteTrouee(t)
		h.Options.Attach = cas.valeur
		if code := h.muet(); code != app.ExitValidation {
			t.Errorf("%s : code = %d\n%s", cas.valeur, code, h.texte())
		}
		h.contient(cas.attendu)
		if strings.Contains(h.registreEcrit(), "parti-sans-compte") {
			t.Errorf("%s : rien ne devait s'écrire", cas.valeur)
		}
	}
}
