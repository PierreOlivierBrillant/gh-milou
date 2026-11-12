package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// groupeAvecHistoriques monte un travail de deux dépôts : l'un remis après la
// date cible, l'autre où l'étudiant n'a rien écrit.
func groupeAvecHistoriques(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()

	tard := state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	tard.History = fakegh.Commits("2026-10-05T14:00:00Z", "2026-09-30T09:00:00Z")
	state.AddContributors("acme/a26.5n6.01.tp1.emilie-cote", "emilie-cote", "emilie-cote")

	muet := state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	muet.History = fakegh.Commits("2026-09-01T08:00:00Z")
	state.AddContributors("acme/a26.5n6.01.tp1.jean-luc-picard", "prof")

	h := nouveau(t, state)
	// Le silence se juge par rapport à quelqu'un : sans liste, un dépôt ne vise
	// personne et rien ne peut lui manquer.
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	})
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01.tp1"
	return h
}

func TestDrapeauDueRetientLaDateCible(t *testing.T) {
	h := groupeAvecHistoriques(t)
	h.Options.DueSet = true
	h.Options.Due = "2026-10-01"

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("est à remettre le 2026-10-01")
	// La date monte au registre de l'organisation, sous l'identifiant complet
	// du travail : c'est ce qui la rend vraie d'un poste à l'autre.
	contenu := travauxDuRegistre(h)
	if !strings.Contains(contenu, `"id": "a26.5n6.01.tp1"`) ||
		!strings.Contains(contenu, `"due": "2026-10-01"`) {
		t.Errorf("le registre ne porte pas l'échéance :\n%s", contenu)
	}
	// Elle n'est pas redite sur ce poste : deux exemplaires finiraient par
	// diverger, et c'est le registre qui fait foi.
	if local := h.groupesLocaux(); strings.Contains(local, "2026-10-01") {
		t.Errorf("le fichier local redit l'échéance :\n%s", local)
	}
}

// travauxDuRegistre rend le fichier des dates de remise de l'organisation.
func travauxDuRegistre(h *harnais) string {
	return string(h.State.Files("acme/"+registry.RepoName,
		registry.Branch)[registry.AssignmentsFile])
}

func TestDrapeauHandinsSignaleLeRetardEtLeSilence(t *testing.T) {
	h := groupeAvecHistoriques(t)
	h.Options.DueSet = true
	h.Options.Due = "2026-10-01"
	h.Options.Handins = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient(
		"Date cible de « tp1 » : 2026-10-01",
		"Commits", "Remise",
		"en retard",
		// Le dépôt que seul l'enseignant a garni nomme celui qui n'a rien remis.
		"rien de",
		// Et il nomme qui y a commis sans y être attendu : le registre ne
		// déclare personne enseignant, « prof » y est donc un inconnu.
		"commits de @prof",
	)
}

func TestDrapeauDueViderRetireLEcheance(t *testing.T) {
	h := groupeAvecHistoriques(t)
	h.Options.DueSet = true
	h.Options.Due = "2026-10-01"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	suivant := nouveauDansLeMemeDossier(t, h)
	suivant.Options.DueSet = true
	suivant.Options.Due = ""
	if code := suivant.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, suivant.texte())
	}
	suivant.contient("n'a plus de date cible")
	if contenu := travauxDuRegistre(suivant); strings.Contains(contenu, "2026-10-01") {
		t.Errorf("l'échéance survit à son retrait :\n%s", contenu)
	}
}

func TestUneDateCibleSurUnPrefixeSansPlaceEstRefusee(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-jlpicard", true)
	h := nouveau(t, state)
	h.Options.ManageRequested = true
	h.Options.Manage = "tp1"
	h.Options.DueSet = true
	h.Options.Due = "2026-10-01"

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d, attendu un refus\n%s", code, h.texte())
	}
	h.contient("ne dit pas à quel groupe il appartient")
}

func TestLaLigneDeCommandeRefuseUneDateQuiNEnEstPasUne(t *testing.T) {
	if _, err := app.Parse([]string{"--due", "le 1er octobre"}, nil); err == nil {
		t.Error("--due accepte « le 1er octobre »")
	}
	options, err := app.Parse([]string{"--due", "2026-10-01T23:59"}, nil)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	if !options.DueSet || options.Due != "2026-10-01T23:59" {
		t.Errorf("Due = %q, DueSet = %v", options.Due, options.DueSet)
	}
	// Le drapeau absent ne demande rien ; « --due "" » retire l'échéance.
	sans, err := app.Parse(nil, nil)
	if err != nil || sans.DueSet {
		t.Errorf("DueSet = %v sans drapeau", sans.DueSet)
	}
	vide, err := app.Parse([]string{"--due", ""}, nil)
	if err != nil || !vide.DueSet || vide.Due != "" {
		t.Errorf("« --due \"\" » : DueSet = %v, Due = %q", vide.DueSet, vide.Due)
	}
}

// Une correction déposée après l'échéance est l'œuvre de qui enseigne : la
// compter mettrait l'étudiante en retard pour ce qu'elle n'a pas fait.
func TestUneCorrectionDEnseignantNeMetPersonneEnRetardAuTerminal(t *testing.T) {
	state := fakegh.NewState()
	depot := state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	depot.History = []fakegh.HistoryEntry{
		{At: "2026-10-03T16:00:00Z", Login: "prof"},
		{At: "2026-09-30T20:45:00Z", Login: "emilie-cote"},
	}
	state.AddContributors("acme/a26.5n6.01.tp1.emilie-cote", "emilie-cote", "prof")

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	// Le registre est le seul à dire qui enseigne : rien dans un nom de dépôt
	// ne le dirait.
	h.Options.User = "prof"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("cooptation : code = %d\n%s", code, h.texte())
	}

	suite := nouveauDansLeMemeDossier(t, h)
	suite.Options.ManageRequested = true
	suite.Options.Manage = "a26.5n6.01.tp1"
	suite.Options.DueSet, suite.Options.Due = true, "2026-10-01"
	suite.Options.Handins = true
	if code := suite.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, suite.texte())
	}
	if strings.Contains(suite.texte(), "en retard") {
		t.Errorf("le commit de l'enseignant met l'étudiante en retard :\n%s", suite.texte())
	}
	suite.contient("remis")
}

// Un dépôt qui n'a rien reçu porte déjà le nom de son étudiante dans la colonne
// d'à côté : le redire à la place du verdict n'apprendrait rien.
func TestUnDepotSansRemiseLeDitSansNommerPersonne(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.Handins = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("non remis")
	if strings.Contains(h.texte(), "rien de") {
		t.Errorf("le verdict nomme l'étudiante que la ligne porte déjà :\n%s", h.texte())
	}
}

// groupeAvecInvitation monte un travail de deux dépôts : l'un remis, l'autre
// dont l'étudiant n'a pas encore accepté son invitation.
func groupeAvecInvitation(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()

	remis := state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	remis.History = []fakegh.HistoryEntry{{At: "2026-09-30T20:45:00Z", Login: "emilie-cote"}}
	state.AddContributors("acme/a26.5n6.01.tp1.emilie-cote", "emilie-cote")
	state.AddCollaborator("acme/a26.5n6.01.tp1.emilie-cote", "emilie-cote", "push")

	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	state.Invite("acme/a26.5n6.01.tp1.jean-luc-picard", "jlpicard", "push")

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	})
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01.tp1"
	return h
}

// Une invitation qu'on n'a pas acceptée n'est pas un silence : la personne n'a
// pas pu remettre, et le terminal le dit du même mot que le navigateur.
func TestUneInvitationEnAttenteSeDitAuTerminal(t *testing.T) {
	h := groupeAvecInvitation(t)
	h.Options.Handins = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("non accepté", "remis")
}

// Le filtre par état se pose aussi au drapeau : les trois interfaces offrent le
// même choix, et « non accepté » y veut dire la même chose.
func TestDrapeauHandinNeGardeQuUnEtat(t *testing.T) {
	h := groupeAvecInvitation(t)
	h.Options.Handins = true
	h.Options.Handin = classroom.Unaccepted

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("a26.5n6.01.tp1.jean-luc-picard", "1 affiché")
	if strings.Contains(h.texte(), "a26.5n6.01.tp1.emilie-cote  ") {
		t.Errorf("le dépôt remis n'a pas été écarté :\n%s", h.texte())
	}
}

// Un état inconnu arrête la ligne de commande plutôt que de se perdre.
func TestDrapeauHandinRefuseUnEtatInconnu(t *testing.T) {
	if _, err := app.Parse([]string{"--handin", "presque remis"}, &bytes.Buffer{}); err == nil {
		t.Error("« presque remis » est accepté")
	}
}

// ligneDe rend la ligne du tableau qui porte ce dépôt.
func ligneDe(h *harnais, depot string) string {
	h.t.Helper()
	for _, ligne := range strings.Split(h.texte(), "\n") {
		if strings.Contains(ligne, depot+" ") {
			return ligne
		}
	}
	h.t.Fatalf("aucune ligne pour « %s » :\n%s", depot, h.texte())
	return ""
}

// groupeDeDeuxTravaux monte un groupe où Émilie a remis tp1 et tp2 le 20 octobre,
// fixe à chaque travail l'échéance donnée — aucune à celui qui n'y figure pas —,
// et rend la session qui relève le groupe entier.
func groupeDeDeuxTravaux(t *testing.T, echeances map[string]string) *harnais {
	t.Helper()
	state := fakegh.NewState()
	for _, travail := range []string{"tp1", "tp2"} {
		depot := state.AddRepo("acme", "a26.5n6.01."+travail+".emilie-cote", true)
		depot.History = []fakegh.HistoryEntry{{At: "2026-10-20T14:00:00Z", Login: "emilie-cote"}}
		state.AddContributors("acme/a26.5n6.01."+travail+".emilie-cote", "emilie-cote")
	}
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	h.Options.ManageRequested = true
	for _, travail := range []string{"tp1", "tp2"} {
		due, fixee := echeances[travail]
		if !fixee {
			continue
		}
		fixer := nouveauDansLeMemeDossier(t, h)
		fixer.Options.Manage = "a26.5n6.01." + travail
		fixer.Options.DueSet, fixer.Options.Due = true, due
		if code := fixer.muet(); code != app.ExitOK {
			t.Fatalf("échéance de %s : code = %d\n%s", travail, code, fixer.texte())
		}
	}
	groupe := nouveauDansLeMemeDossier(t, h)
	groupe.Options.Manage = "a26.5n6.01"
	groupe.Options.Handins = true
	return groupe
}

// Un groupe entier mêle ses travaux : chaque dépôt s'y juge sur la date cible
// du sien, comme la page du travail le fait au navigateur. Sans cela, gérer le
// groupe plutôt que le travail disait « remis » d'une copie en retard.
func TestUnGroupeEntierJugeChaqueDepotSurLaDateDeSonTravail(t *testing.T) {
	groupe := groupeDeDeuxTravaux(t, map[string]string{"tp1": "2026-10-01", "tp2": "2026-11-01"})
	if code := groupe.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, groupe.texte())
	}
	if ligne := ligneDe(groupe, "a26.5n6.01.tp1.emilie-cote"); !strings.Contains(ligne, "en retard") {
		t.Errorf("tp1, remis après son échéance, n'est pas en retard :\n%s", ligne)
	}
	if ligne := ligneDe(groupe, "a26.5n6.01.tp2.emilie-cote"); strings.Contains(ligne, "en retard") ||
		!strings.Contains(ligne, "remis") {
		t.Errorf("tp2, remis avant son échéance, est mal jugé :\n%s", ligne)
	}
	// Le groupe dit sa place : le relevé n'a pas à prétendre le contraire, et
	// chaque travail ayant sa date, il n'y a rien à en dire.
	groupe.absent("ne dit pas à quel groupe il appartient", "Sans date cible")
}

// Un travail sans date cible n'a jamais de retard : une colonne qui n'en
// montre aucun doit dire pourquoi, sans quoi « remis » se lirait « à l'heure ».
func TestUnGroupeEntierNommeSesTravauxSansDateCible(t *testing.T) {
	groupe := groupeDeDeuxTravaux(t, map[string]string{"tp1": "2026-10-01"})
	if code := groupe.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, groupe.texte())
	}
	groupe.contient("Sans date cible, rien n'est dit en retard dans : tp2.")
	if ligne := ligneDe(groupe, "a26.5n6.01.tp1.emilie-cote"); !strings.Contains(ligne, "en retard") {
		t.Errorf("tp1 garde son échéance :\n%s", ligne)
	}
}

// Le nom complet d'un dépôt vient de la liste du groupe, comme au navigateur et
// dans --students. Le dernier niveau d'un nom de dépôt est un slug : lu comme un
// compte GitHub, il nommerait l'inconnu qui porte ce compte-là.
func TestLeNomCompletVientDeLaListeDuGroupe(t *testing.T) {
	h := groupeAvecHistoriques(t)
	h.State.Users["jean-luc-picard"] = "Un Homonyme"
	if code, _ := h.script("quitter"); code != app.ExitOK {
		t.Fatalf("travail : code = %d\n%s", code, h.texte())
	}
	groupe := nouveauDansLeMemeDossier(t, h)
	groupe.Options.Manage = "a26.5n6.01"
	if code, _ := groupe.script("quitter"); code != app.ExitOK {
		t.Fatalf("groupe : code = %d\n%s", code, groupe.texte())
	}
	for _, session := range []*harnais{h, groupe} {
		if ligne := ligneDe(session, "a26.5n6.01.tp1.jean-luc-picard"); !strings.Contains(ligne, "Jean-Luc Picard") {
			t.Errorf("« %s » ne nomme pas Jean-Luc Picard :\n%s", session.Options.Manage, ligne)
		}
		if ligne := ligneDe(session, "a26.5n6.01.tp1.emilie-cote"); !strings.Contains(ligne, "Émilie Côté") {
			t.Errorf("« %s » ne nomme pas Émilie Côté :\n%s", session.Options.Manage, ligne)
		}
		session.absent("Un Homonyme")
	}
	if appels := h.State.CallCount("/users/jean-luc-picard"); appels != 0 {
		t.Errorf("le profil du compte « jean-luc-picard » a été lu %d fois", appels)
	}
}
