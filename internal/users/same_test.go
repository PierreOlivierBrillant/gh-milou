package users_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
)

// deuxSessions est le cas de l'issue : @Mr-Commeilfaut a suivi un cours à
// l'automne, @commeilfautt un autre à l'hiver, et c'est la même personne. Une
// homonyme sans rapport suit le cours d'hiver.
func deuxSessions() ([]classroom.Classroom, []groups.RepoInfo) {
	defauts := classroom.DefaultsFrom(config.Default())
	return []classroom.Classroom{
		{
			Org: "acme", Session: "a26", Course: "5n6", Group: "01", Defaults: defauts,
			Students: []roster.Person{{FullName: "Jean Commeilfaut", Username: "Mr-Commeilfaut"}},
		},
		{
			Org: "acme", Session: "h27", Course: "4w6", Group: "01", Defaults: defauts,
			Students: []roster.Person{
				{FullName: "Jean Comme-Tu-Veux", Username: "commeilfautt", StudentID: "2412345"},
				{FullName: "Jean Commeilfaut", Username: "autre-jean"},
			},
		},
	}, []groups.RepoInfo{
		{Name: "a26.5n6.01.tp1.jean-commeilfaut", PushedAt: "2026-09-20T10:00:00Z"},
		{Name: "h27.4w6.01.tp1.jean-comme-tu-veux", PushedAt: "2027-02-10T10:00:00Z"},
	}
}

// registreDe rend un registre qui a appris des personnes, puis appliqué des
// changements.
func registreDe(t *testing.T, changes ...registry.Change) *registry.Set {
	t.Helper()
	set := registry.Empty()
	for _, change := range changes {
		var err error
		if set, _, err = set.With(change, "2026-10-07"); err != nil {
			t.Fatal(err)
		}
	}
	return set
}

// Un compte qui a commis dans les dépôts d'une personne sans être le sien est
// proposé d'abord : c'est le plus souvent elle-même, sous un autre compte. Et
// la réunion l'accepte même s'il n'est d'aucun groupe — il a commis ici, ce
// n'est pas une faute de frappe. Un compte vu nulle part, lui, l'est encore.
func TestUnCompteVuDansSesDepotsSeProposeEtSeReunit(t *testing.T) {
	cours, inventaire := deuxSessions()
	set := registreDe(t)
	lignes := users.Directory(cours, inventaire, nil, nil, set)
	remises := map[string]groups.Handin{
		"a26.5n6.01.tp1.jean-commeilfaut": {Authors: map[string]int{"mr-commeilfaut": 2, "vieux-jean": 5}},
	}
	if vus := users.Seen(lignes, set, remises, "Mr-Commeilfaut"); strings.Join(vus, " ") != "vieux-jean" {
		t.Fatalf("vus = %v", vus)
	}
	if _, err := users.PlanJoin(lignes, set, "prof", "acme", "vieux-jean", "Mr-Commeilfaut"); err == nil {
		t.Fatal("un compte inconnu de l'annuaire devait être refusé sans témoin")
	}

	temoins := users.Witness(lignes, remises, "vieux-jean", "Mr-Commeilfaut")
	if len(temoins) != len(lignes)+1 {
		t.Fatalf("%d ligne(s) après témoignage, attendu %d", len(temoins), len(lignes)+1)
	}
	reunion, err := users.PlanJoin(temoins, set, "prof", "acme", "vieux-jean", "Mr-Commeilfaut")
	if err != nil {
		t.Fatalf("réunion refusée : %v", err)
	}
	if reunion.Principal != "Mr-Commeilfaut" ||
		strings.Join(reunion.Accounts, ",") != "Mr-Commeilfaut,vieux-jean" {
		t.Errorf("réunion = %+v", reunion)
	}
	if sans := users.Witness(lignes, remises, "inconnu"); len(sans) != len(lignes) {
		t.Error("un compte vu nulle part a été ajouté à l'annuaire")
	}
}

// Une fois les deux comptes réunis au registre, l'annuaire n'a plus qu'une
// ligne pour la personne : ses deux cours, ses deux dépôts, sous le compte
// qu'on a choisi de garder.
func TestLAnnuaireReunitDeuxComptesDUneMemePersonne(t *testing.T) {
	cours, inventaire := deuxSessions()
	if lignes := users.Directory(cours, inventaire, nil, nil, registreDe(t)); len(lignes) != 3 {
		t.Fatalf("avant : %s", comptes(lignes))
	}

	set := registreDe(t, registry.Join("Mr-Commeilfaut", "commeilfautt"))
	lignes := users.Directory(cours, inventaire, nil, nil, set)
	if comptes(lignes) != "commeilfautt,autre-jean" {
		t.Fatalf("après : %s", comptes(lignes))
	}
	for _, ligne := range lignes {
		if ligne.Username != "commeilfautt" {
			continue
		}
		if strings.Join(ligne.Accounts, ",") != "commeilfautt,Mr-Commeilfaut" ||
			len(ligne.Enrollments) != 2 || len(ligne.Repos) != 2 {
			t.Fatalf("ligne = %+v", ligne)
		}
		// Ce que dit le compte qui désigne l'emporte.
		if ligne.FullName != "Jean Comme-Tu-Veux" || ligne.StudentID != "2412345" {
			t.Errorf("nom = %q, matricule = %q", ligne.FullName, ligne.StudentID)
		}
	}

	// La fiche, ouverte depuis l'un ou l'autre compte, montre l'ensemble.
	for _, compte := range []string{"Mr-Commeilfaut", "commeilfautt"} {
		fiche := users.ProfileOf(cours, inventaire, nil, nil, set, compte)
		if fiche.Courses != 2 || len(fiche.Joined) != 2 || fiche.Username != "commeilfautt" {
			t.Errorf("%s : fiche = %+v", compte, fiche)
		}
	}
}

// Un enseignant à deux comptes n'a qu'une ligne, avec les cours donnés sous
// l'un et sous l'autre.
func TestUnEnseignantADeuxComptesNAQuUneLigne(t *testing.T) {
	cours, inventaire := deuxSessions()
	infos := []teams.Info{
		{Slug: "a26-5n6-01-enseignants", Name: "a26.5n6.01.enseignants", Members: []string{"prof-perso"}},
		{Slug: "h27-4w6-01-enseignants", Name: "h27.4w6.01.enseignants", Members: []string{"prof"}},
	}
	set := registreDe(t,
		registry.Change{Learn: []registry.User{{Username: "prof", FullName: "Kathryn Janeway"}}},
		registry.SetRole("prof", true),
		registry.Join("prof-perso", "prof"))

	var profs []users.Row
	for _, ligne := range users.Directory(cours, inventaire, nil, infos, set) {
		if ligne.IsTeacher {
			profs = append(profs, ligne)
		}
	}
	if len(profs) != 1 || profs[0].Username != "prof" || len(profs[0].Enrollments) != 2 ||
		profs[0].FullName != "Kathryn Janeway" {
		t.Fatalf("enseignants = %+v", profs)
	}
}

// Réunir se prépare dans le domaine : ce qui l'emporte est dit avant d'écrire.
func TestPreparerUneReunionDitCeQuiLEmporte(t *testing.T) {
	cours, inventaire := deuxSessions()
	set := registreDe(t)
	lignes := users.Directory(cours, inventaire, nil, nil, set)

	reunion, err := users.PlanJoin(lignes, set, "prof", "acme", "Mr-Commeilfaut", "commeilfautt")
	if err != nil {
		t.Fatal(err)
	}
	if reunion.FullName != "Jean Comme-Tu-Veux" || reunion.StudentID != "2412345" ||
		reunion.Role != users.AsStudent ||
		strings.Join(reunion.Accounts, ",") != "commeilfautt,Mr-Commeilfaut" {
		t.Fatalf("réunion = %+v", reunion)
	}
	// Dans l'autre sens, c'est l'autre nom qui l'emporte ; le matricule reste,
	// faute d'un autre.
	inverse, err := users.PlanJoin(lignes, set, "prof", "acme", "commeilfautt", "Mr-Commeilfaut")
	if err != nil {
		t.Fatal(err)
	}
	if inverse.FullName != "Jean Commeilfaut" || inverse.StudentID != "2412345" {
		t.Errorf("inverse = %+v", inverse)
	}
}

// Ce que le domaine refuse, et pourquoi : rien qui se déduirait d'une faute de
// frappe, d'un geste déjà fait, ou d'un clic d'étudiant.
func TestCeQuUneReunionRefuse(t *testing.T) {
	cours, inventaire := deuxSessions()
	reunis := registreDe(t, registry.Join("Mr-Commeilfaut", "commeilfautt"))
	cooptes := registreDe(t,
		registry.Change{Learn: []registry.User{{Username: "prof"}}},
		registry.SetRole("prof", true))

	cas := []struct {
		nom               string
		set               *registry.Set
		viewer, a, b, mot string
	}{
		{"même compte", registreDe(t), "prof", "autre-jean", "Autre-Jean", "même compte"},
		{"déjà réunis", reunis, "prof", "commeilfautt", "Mr-Commeilfaut", "déjà réunis"},
		{"faute de frappe", registreDe(t), "prof", "autre-jean", "comettuveux", "nulle part"},
		{"pas enseignant", cooptes, "autre-jean", "autre-jean", "commeilfautt", "Seul un enseignant"},
	}
	for _, c := range cas {
		lignes := users.Directory(cours, inventaire, nil, nil, c.set)
		_, err := users.PlanJoin(lignes, c.set, c.viewer, "acme", c.a, c.b)
		if err == nil || !strings.Contains(err.Error(), c.mot) {
			t.Errorf("%s : err = %v", c.nom, err)
		}
	}

	// Deux matricules différents sont deux personnes.
	cours[0].Students[0].StudentID = "2400001"
	lignes := users.Directory(cours, inventaire, nil, nil, registreDe(t))
	if _, err := users.PlanJoin(lignes, registreDe(t), "prof", "acme",
		"Mr-Commeilfaut", "commeilfautt"); err == nil || !strings.Contains(err.Error(), "deux personnes") {
		t.Errorf("matricules : err = %v", err)
	}
}

// Séparer n'est offert qu'à ce que le registre a réuni ; une liste de groupe
// qui déclare deux comptes se corrige depuis ce groupe, et le domaine le dit.
func TestSeparerNeDefaitQueCeQueLeRegistreAReuni(t *testing.T) {
	cours, inventaire := deuxSessions()
	set := registreDe(t, registry.Join("Mr-Commeilfaut", "commeilfautt"))
	lignes := users.Directory(cours, inventaire, nil, nil, set)
	separation, err := users.PlanSplit(lignes, set, "prof", "acme", "Mr-Commeilfaut")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(separation.Remaining, ",") != "commeilfautt" {
		t.Errorf("séparation = %+v", separation)
	}

	cours[1].Students[1].Also = []string{"jean-perso"}
	lignes = users.Directory(cours, inventaire, nil, nil, registreDe(t))
	_, err = users.PlanSplit(lignes, registreDe(t), "prof", "acme", "autre-jean")
	if err == nil || !strings.Contains(err.Error(), "la liste d'un groupe") {
		t.Errorf("err = %v", err)
	}
}

// Les homonymes sont proposés d'abord : une suggestion, jamais une décision.
func TestLesCandidatsDeMemeNomPassentDAbord(t *testing.T) {
	cours, inventaire := deuxSessions()
	candidats := users.Candidates(users.Directory(cours, inventaire, nil, nil, registreDe(t)),
		"Mr-Commeilfaut")
	if len(candidats) != 2 || candidats[0].Username != "autre-jean" || !candidats[0].SameName ||
		candidats[1].SameName {
		t.Fatalf("candidats = %+v", candidats)
	}
}
