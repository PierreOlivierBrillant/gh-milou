package groups_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
)

// prof écarte l'enseignant, comme le registre le ferait.
func prof(login string) bool { return login == "prof" }

func TestLaRemiseSeDateHorsDesCommitsEcartes(t *testing.T) {
	remise := groups.Handin{
		Commits: 3,
		Last:    "2026-10-03T16:00:00Z",
		LastBy: map[string]string{
			"ecote": "2026-09-30T20:45:00Z",
			"prof":  "2026-10-03T16:00:00Z",
		},
	}
	if dernier := remise.LastBut(prof); dernier != "2026-09-30T20:45:00Z" {
		t.Errorf("LastBut = %q", dernier)
	}
	// Sans personne à écarter, c'est le dernier commit tout court.
	if dernier := remise.LastBut(nil); dernier != "2026-10-03T16:00:00Z" {
		t.Errorf("LastBut(nil) = %q", dernier)
	}
}

// Les commits de qui enseigne — les fichiers de départ, une correction — ne
// sont pas le travail de l'étudiant : ils sortent du compte comme ils sortent
// de la date. Sans auteurs relevés, ou sans personne à écarter, le total reste.
func TestLesCommitsEcartesSortentDuCompte(t *testing.T) {
	remise := groups.Handin{
		Commits: 5,
		Authors: map[string]int{"ecote": 3, "prof": 2},
	}
	if nombre := remise.CommitsBut(prof); nombre != 3 {
		t.Errorf("CommitsBut = %d, attendu les 3 commits d'Émilie", nombre)
	}
	if nombre := remise.CommitsBut(nil); nombre != 5 {
		t.Errorf("CommitsBut(nil) = %d, attendu le total", nombre)
	}
	ancien := groups.Handin{Commits: 2}
	if nombre := ancien.CommitsBut(prof); nombre != 2 {
		t.Errorf("sans auteurs relevés, CommitsBut = %d, attendu le total", nombre)
	}
	// Les auteurs et le total viennent de deux lectures : un décalage ne doit
	// jamais rendre un compte négatif.
	decale := groups.Handin{Commits: 1, Authors: map[string]int{"prof": 2}}
	if nombre := decale.CommitsBut(prof); nombre != 0 {
		t.Errorf("CommitsBut = %d, attendu zéro", nombre)
	}
}

// Un dépôt où l'on n'a écarté que ce qui s'y trouvait n'a rien reçu : c'est une
// réponse, pas un manque d'information.
func TestUnDepotOuToutEstEcarteNaPasDeRemise(t *testing.T) {
	remise := groups.Handin{
		Commits: 1,
		Last:    "2026-10-03T16:00:00Z",
		LastBy:  map[string]string{"prof": "2026-10-03T16:00:00Z"},
	}
	if dernier := remise.LastBut(prof); dernier != "" {
		t.Errorf("LastBut = %q, attendu aucune remise", dernier)
	}
}

// Un relevé d'une forme antérieure ne porte pas ses auteurs. La date du dernier
// commit est alors tout ce qu'on sait, et mieux vaut une date trop tardive que
// pas de date du tout.
func TestUnReleveSansAuteursRendLaDateQuIlPorte(t *testing.T) {
	remise := groups.Handin{Commits: 2, Last: "2026-10-03T16:00:00Z"}
	if dernier := remise.LastBut(prof); dernier != "2026-10-03T16:00:00Z" {
		t.Errorf("LastBut = %q", dernier)
	}
}

// Un relevé ne couvre que ce qui a été poussé avant sa lecture — à une minute
// près, pour les horloges. Un relevé sans date, lu avant qu'on la retienne,
// ne couvre rien.
func TestUnReleveCouvreCeQuiLePrecede(t *testing.T) {
	remise := groups.Handin{Seen: "2026-09-30T12:00:00Z"}
	cas := map[string]bool{
		"":                     true,
		"2026-09-30T11:00:00Z": true,
		"2026-09-30T12:00:30Z": true,
		"2026-09-30T12:05:00Z": false,
		"illisible":            false,
	}
	for envoi, voulu := range cas {
		if obtenu := remise.Covers(envoi); obtenu != voulu {
			t.Errorf("Covers(%q) = %v, voulu %v", envoi, obtenu, voulu)
		}
	}
	if (groups.Handin{}).Covers("2026-09-30T11:00:00Z") {
		t.Error("un relevé sans date couvre un envoi")
	}
}

// Un compte qui a commis sans être attendu se nomme ; l'enseignant et les
// comptes attendus, quelle que soit leur casse, n'en sont pas.
func TestLesEtrangersSontCeuxQuOnNAttendaitPas(t *testing.T) {
	remise := groups.Handin{Authors: map[string]int{
		"ecote": 3, "vieux-compte": 2, "prof": 1, "ami": 1,
	}}
	prof := func(login string) bool { return login == "prof" }
	etrangers := remise.Strangers([]string{"ECote"}, prof)
	if len(etrangers) != 2 || etrangers[0] != "ami" || etrangers[1] != "vieux-compte" {
		t.Errorf("étrangers = %v", etrangers)
	}
	if sans := remise.Strangers([]string{"ecote", "vieux-compte", "ami"}, prof); len(sans) != 0 {
		t.Errorf("tout le monde est attendu, et pourtant : %v", sans)
	}
}

// Ce qu'un écran montre comme relevé est ce qu'aucun envoi n'a dépassé : l'âge
// du relevé n'y entre pas, et un dépôt absent de l'inventaire garde le sien.
func TestCompleteGardeCeQuAucunEnvoiNADepasse(t *testing.T) {
	ancien := groups.Handin{Commits: 3, Seen: "2026-09-01T08:00:00Z"}
	recent := groups.Handin{Commits: 5, Seen: "2026-09-30T12:00:00Z"}
	inventaire := []groups.RepoInfo{
		{Name: "tp1-ecote", PushedAt: "2026-08-31T18:00:00Z"},
		{Name: "TP1-JLPICARD", PushedAt: "2026-09-30T12:05:00Z"},
	}
	completes := groups.Complete(inventaire, map[string]groups.Handin{
		"tp1-ecote": ancien, "tp1-jlpicard": recent, "tp1-adopte": ancien,
	})
	if _, garde := completes["tp1-ecote"]; !garde {
		t.Error("un relevé d'un mois, que rien n'a dépassé, est écarté")
	}
	if _, garde := completes["tp1-jlpicard"]; garde {
		t.Error("un relevé dépassé par un envoi est gardé")
	}
	if _, garde := completes["tp1-adopte"]; !garde {
		t.Error("un dépôt absent de l'inventaire perd son relevé")
	}
}

// Un relevé dépassé par un envoi est à relire d'office : la mémoire seule le
// croirait encore bon. Ce qui n'a jamais été relevé, ou que rien n'a dépassé,
// ne se lit que s'il manque.
func TestUnReleveDepasseSeRelitDOffice(t *testing.T) {
	inventaire := []groups.RepoInfo{
		{Name: "tp1-ecote", PushedAt: "2026-09-30T18:00:00Z"},
		{Name: "tp1-jlpicard", PushedAt: "2026-09-30T11:00:00Z"},
		{Name: "tp1-nouveau", PushedAt: "2026-09-30T18:00:00Z"},
	}
	histoires := map[string]groups.Handin{
		"tp1-ecote":    {Seen: "2026-09-30T12:00:00Z"},
		"tp1-jlpicard": {Seen: "2026-09-30T12:00:00Z"},
	}
	depasses, autres := groups.Overtaken(
		[]string{"tp1-ecote", "tp1-jlpicard", "tp1-nouveau"}, inventaire, histoires)
	if strings.Join(depasses, " ") != "tp1-ecote" {
		t.Errorf("dépassés = %v, attendu le seul dépôt poussé depuis son relevé", depasses)
	}
	if strings.Join(autres, " ") != "tp1-jlpicard tp1-nouveau" {
		t.Errorf("autres = %v", autres)
	}
}

// Le dernier envoi d'un dépôt ignore ce que l'enseignant y a poussé, tant que
// l'historique relevé le permet ; sinon, il reste celui que GitHub donne.
func TestLActiviteIgnoreCeQueLEnseignantAPousse(t *testing.T) {
	historique := groups.Handin{
		Last:   "2026-09-20T16:00:00Z",
		LastBy: map[string]string{"prof": "2026-09-20T16:00:00Z", "ecote": "2026-09-05T10:00:00Z"},
		Seen:   "2026-09-21T08:00:00Z",
	}
	seulProf := groups.Handin{
		Last: "2026-09-20T16:00:00Z", LastBy: map[string]string{"prof": "2026-09-20T16:00:00Z"},
		Seen: "2026-09-21T08:00:00Z",
	}
	inventaire := []groups.RepoInfo{
		{Name: "releve", PushedAt: "2026-09-20T16:00:01Z"},
		{Name: "pousse-depuis", PushedAt: "2026-09-25T09:00:00Z"},
		{Name: "jamais-releve", PushedAt: "2026-09-20T16:00:01Z"},
		{Name: "rien-de-l-etudiant", PushedAt: "2026-09-20T16:00:01Z"},
	}
	remises := map[string]groups.Handin{
		"releve": historique, "pousse-depuis": historique, "rien-de-l-etudiant": seulProf,
	}

	corriges := groups.Activity(inventaire, remises, prof)
	voulus := []string{"2026-09-05T10:00:00Z", "2026-09-25T09:00:00Z", "2026-09-20T16:00:01Z", ""}
	for index, voulu := range voulus {
		if corriges[index].PushedAt != voulu {
			t.Errorf("%s : %q, voulu %q", corriges[index].Name, corriges[index].PushedAt, voulu)
		}
	}
	// L'inventaire reçu reste tel que GitHub l'a rendu : il est mis en cache.
	if inventaire[0].PushedAt != "2026-09-20T16:00:01Z" {
		t.Error("l'inventaire d'origine a été modifié")
	}
	// Sans savoir qui enseigne, rien ne se corrige.
	if groups.Activity(inventaire, remises, nil)[0].PushedAt != "2026-09-20T16:00:01Z" {
		t.Error("corrigé sans savoir qui enseigne")
	}
}

func TestDatedReporteLActiviteSurLeGroupe(t *testing.T) {
	groupe := groups.Build("tp1", []groups.RepoInfo{
		{Name: "tp1-ecote", PushedAt: "2026-09-20T16:00:00Z"},
		{Name: "tp1-jlpicard", PushedAt: "2026-09-20T16:00:00Z"},
	})
	date := groupe.Dated([]groups.RepoInfo{{Name: "TP1-ECOTE", PushedAt: ""}})
	if date.Repos[0].PushedAt != "" || date.Repos[1].PushedAt != groupe.Repos[1].PushedAt {
		t.Errorf("groupe daté = %+v", date.Repos)
	}
	if groupe.Repos[0].PushedAt == "" {
		t.Error("le groupe d'origine a été modifié")
	}
}
