package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// envoi met une date rendue par GitHub sous la forme que l'annuaire porte :
// l'heure de la machine, à la minute.
func envoi(iso string) string {
	moment, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		panic(err)
	}
	return moment.Local().Format("2006-01-02 15:04")
}

// annuaireLigne est une personne de l'annuaire, telle que l'API la rend.
type annuaireLigne struct {
	FullName    string                `json:"full_name"`
	Username    string                `json:"username"`
	StudentID   string                `json:"student_id"`
	IsTeacher   bool                  `json:"is_teacher"`
	Role        string                `json:"role"`
	Repos       int                   `json:"repos"`
	PushedAt    string                `json:"pushed_at"`
	Enrollments []annuaireInscription `json:"enrollments"`
}

// annuaireInscription est un cours qu'elle a suivi, ou donné.
type annuaireInscription struct {
	Scope       string `json:"scope"`
	Session     string `json:"session"`
	SessionName string `json:"session_name"`
	Course      string `json:"course"`
	Role        string `json:"role"`
	Assignments []struct {
		Name string `json:"name"`
	} `json:"assignments"`
}

// annuaire est ce que l'API rend pour l'organisation entière.
type annuaireRendu struct {
	Users    []annuaireLigne `json:"users"`
	Sessions []struct {
		Short string `json:"short"`
		Name  string `json:"name"`
	} `json:"sessions"`
	Courses   []string `json:"courses"`
	Total     int      `json:"total"`
	Shown     int      `json:"shown"`
	Unmatched int      `json:"unmatched"`
}

// college monte deux sessions et deux cours autour des mêmes personnes.
func college(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	envois := map[string]string{
		"a26.5n6.01.tp1.jean-luc-picard":    "2026-09-01T10:00:00Z",
		"a26.5n6.01.tp1.emilie-cote":        "2026-09-20T10:00:00Z",
		"a26.4w6.01.projet.emilie-cote":     "2026-11-05T10:00:00Z",
		"h27.5n6.02.tp1.emilie-cote":        "2027-02-10T10:00:00Z",
		"h27.5n6.02.tp1.inconnu-quelconque": "2027-02-11T10:00:00Z",
	}
	for nom, envoi := range envois {
		state.AddRepo("acme", nom, true).PushedAt = envoi
	}
	h := nouveau(t, state)
	h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")
	h.groupe("a26", "4w6", "01", "Émilie Côté", "emilie-cote")
	h.groupe("h27", "5n6", "02", "Émilie Côté", "emilie-cote", "Aminata Diallo", "aminata-d")
	return h
}

func (h *harnais) annuaire(requete string) annuaireRendu {
	h.t.Helper()
	var rendu annuaireRendu
	h.json(http.MethodGet, "/api/users"+requete, nil, &rendu)
	return rendu
}

// comptesDe rend les comptes de l'annuaire, dans l'ordre où il les donne.
func comptesDe(rendu annuaireRendu) string {
	noms := make([]string, 0, len(rendu.Users))
	for _, etudiant := range rendu.Users {
		noms = append(noms, etudiant.Username)
	}
	return strings.Join(noms, ",")
}

// Une personne n'a qu'une ligne, quels que soient les cours qu'elle a suivis :
// c'est ce que la liste d'un groupe ne peut pas montrer.
func TestAnnuaireRassembleLesGroupesDeLOrganisation(t *testing.T) {
	rendu := college(t).annuaire("")
	if comptesDe(rendu) != "aminata-d,emilie-cote,jlpicard" {
		t.Fatalf("annuaire : %s", comptesDe(rendu))
	}
	if rendu.Total != 3 || rendu.Shown != 3 {
		t.Fatalf("%d affiché(s) sur %d", rendu.Shown, rendu.Total)
	}
	// Le dépôt dont le dernier niveau ne désigne personne n'ajoute pas
	// quelqu'un, mais il est signalé.
	if rendu.Unmatched != 1 {
		t.Fatalf("dépôts non rattachés : %d", rendu.Unmatched)
	}

	var emilie = rendu.Users[1]
	if emilie.Username != "emilie-cote" {
		t.Fatalf("deuxième ligne : %s", emilie.Username)
	}
	if len(emilie.Enrollments) != 3 || emilie.Repos != 3 {
		t.Fatalf("Émilie : %d inscription(s), %d dépôt(s)",
			len(emilie.Enrollments), emilie.Repos)
	}
	if emilie.PushedAt != envoi("2027-02-10T10:00:00Z") {
		t.Fatalf("dernier envoi d'Émilie : %q", emilie.PushedAt)
	}
	// Chaque inscription porte ses propres dépôts : deux groupes ont chacun
	// leur « tp1 ».
	parPlace := map[string]int{}
	for _, inscription := range emilie.Enrollments {
		parPlace[inscription.Scope] = len(inscription.Assignments)
	}
	if parPlace["a26.5n6.01"] != 1 || parPlace["h27.5n6.02"] != 1 ||
		parPlace["a26.4w6.01"] != 1 {
		t.Fatalf("dépôts par place : %v", parPlace)
	}

	// Les sessions arrivent nommées et rangées, comme partout ailleurs.
	if len(rendu.Sessions) != 2 ||
		rendu.Sessions[0].Short != "h27" || rendu.Sessions[0].Name != "Hiver 2027" ||
		rendu.Sessions[1].Short != "a26" {
		t.Fatalf("sessions : %+v", rendu.Sessions)
	}
	if strings.Join(rendu.Courses, ",") != "4w6,5n6" {
		t.Fatalf("cours : %v", rendu.Courses)
	}
}

func TestAnnuaireSeFiltreParSessionEtParCours(t *testing.T) {
	h := college(t)
	cas := []struct{ requete, attendus string }{
		{"?session=a26", "emilie-cote,jlpicard"},
		{"?session=h27", "aminata-d,emilie-cote"},
		{"?course=4w6", "emilie-cote"},
		// Les deux critères portent sur la même inscription : Picard a fait
		// 5N6, mais pas à l'hiver.
		{"?session=h27&course=5n6", "aminata-d,emilie-cote"},
		{"?session=h27&course=4w6", ""},
		{"?q=cote", "emilie-cote"},
		{"?sort=envoi&desc=1", "emilie-cote,jlpicard,aminata-d"},
		{"?activity=sans", "aminata-d"},
		{"?after=2026-10-01", "emilie-cote"},
	}
	for _, essai := range cas {
		rendu := h.annuaire(essai.requete)
		if comptesDe(rendu) != essai.attendus {
			t.Fatalf("%s : %s (attendu %s)", essai.requete, comptesDe(rendu), essai.attendus)
		}
		if rendu.Shown != len(rendu.Users) || rendu.Total != 3 {
			t.Fatalf("%s : %d affiché(s) sur %d", essai.requete, rendu.Shown, rendu.Total)
		}
	}

	// Une session choisie ne propose que les cours qu'elle a portés.
	if cours := h.annuaire("?session=h27").Courses; strings.Join(cours, ",") != "5n6" {
		t.Fatalf("cours de l'hiver : %v", cours)
	}

	// Un critère que le serveur ne peut pas appliquer est refusé, plutôt que
	// silencieusement ignoré.
	reponse, contenu := h.requete(http.MethodGet, "/api/users?after=hier", nil)
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
}

// Un groupe qu'on n'a pas déclaré n'a pas de liste : ses dépôts ne désignent
// personne de connu, et l'annuaire le dit plutôt que d'inventer des étudiants.
func TestAnnuaireSansGroupeDeclare(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)

	rendu := h.annuaire("")
	if rendu.Total != 0 || len(rendu.Users) != 0 {
		t.Fatalf("annuaire : %+v", rendu.Users)
	}
	if rendu.Unmatched != 1 {
		t.Fatalf("dépôts non rattachés : %d", rendu.Unmatched)
	}
}

// Qui n'a pas encore de compte figure à l'annuaire avec ses dépôts, et sa ligne
// porte son matricule : c'est lui qui la désigne au navigateur. Sous un compte
// vide, partagé par tous les inscrits sans compte, déplier l'une les aurait
// toutes dépliées.
func TestLAnnuaireDesigneParSonMatriculeQuiNAPasDeCompte(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.nadia-pepin", true)
	state.AddRepo("acme", "a26.5n6.01.tp1.claire-otis", true)
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	h.poserListe(place, inscritParCompte("Jean-Luc Picard", "jlpicard"),
		inscritParMatricule("Nadia Pépin", "2100110"),
		inscritParMatricule("Claire Otis", "2100789"))

	trouvees := map[string]annuaireLigne{}
	for _, ligne := range h.annuaire("").Users {
		trouvees[ligne.FullName] = ligne
	}
	for nom, matricule := range map[string]string{
		"Nadia Pépin": "2100110", "Claire Otis": "2100789",
	} {
		if ligne := trouvees[nom]; ligne.StudentID != matricule || ligne.Repos != 1 {
			t.Errorf("%s : %+v", nom, ligne)
		}
	}
}
