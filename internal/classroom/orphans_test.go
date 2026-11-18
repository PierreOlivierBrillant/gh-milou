package classroom_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
)

// comptesGithub joue les profils publics : un compte y est, ou il n'y est pas.
type comptesGithub map[string]roster.Person

func (c comptesGithub) Account(login string) (roster.Person, bool, error) {
	person, existe := c[strings.ToLower(login)]
	return person, existe, nil
}

// enPanne joue un GitHub qui ne répond pas.
type enPanne struct{}

func (enPanne) Account(string) (roster.Person, bool, error) {
	return roster.Person{}, false, errors.New("délai dépassé")
}

// fragments rend les derniers niveaux relevés, dans l'ordre où ils viennent.
func fragments(orphelins []classroom.Orphan) string {
	noms := make([]string, 0, len(orphelins))
	for _, orphelin := range orphelins {
		noms = append(noms, orphelin.Fragment)
	}
	return strings.Join(noms, ",")
}

// Les dépôts que la liste ne réclame pas se rassemblent par personne, non par
// dépôt : une même personne en laisse un par travail, et les rattacher un à un
// ferait répondre plusieurs fois à la même question.
func TestOrphelinsRassemblesParDernierNiveau(t *testing.T) {
	inventaire := []groups.RepoInfo{
		{Name: "a26.5n6.01.tp1.emilie-cote", PushedAt: "2026-09-01T10:00:00Z"},
		{Name: "a26.5n6.01.tp1.wriker", PushedAt: "2026-09-15T10:00:00Z"},
		{Name: "a26.5n6.01.tp2.wriker", PushedAt: "2026-10-02T10:00:00Z"},
		{Name: "a26.5n6.01.tp1.william-riker"},
		// Un dépôt d'équipe appartient à son équipe, un autre groupe et un
		// dépôt hors nomenclature ne regardent pas celui-ci.
		{Name: "a26.5n6.01.projet.eq1"},
		{Name: "a26.5n6.02.tp1.quelqu-un"},
		{Name: "notes-du-cours"},
	}
	cours := groupe("a26", "5n6", "01", personnes("Émilie Côté", "ecote"))
	equipes := []teams.Team{{Short: "eq1", Session: "a26", Course: "5n6", Group: "01"}}

	orphelins := cours.Orphans(inventaire, equipes)
	if fragments(orphelins) != "william-riker,wriker" {
		t.Fatalf("orphelins = %s", fragments(orphelins))
	}
	riker := orphelins[1]
	if strings.Join(riker.Repos, " ") != "a26.5n6.01.tp1.wriker a26.5n6.01.tp2.wriker" ||
		strings.Join(riker.Assignments, " ") != "tp1 tp2" {
		t.Fatalf("dépôts de wriker = %+v", riker)
	}
	if !strings.HasPrefix(riker.PushedAt, "2026-10-02") {
		t.Errorf("dernier envoi = %q", riker.PushedAt)
	}
	// Qui n'a jamais rien reçu n'a pas de date, plutôt qu'une inventée.
	if orphelins[0].PushedAt != "" {
		t.Errorf("envoi de william-riker = %q", orphelins[0].PushedAt)
	}
}

// Un compte que le registre connaît déjà — même sans nom complet — n'est pas
// un orphelin : c'est une personne du groupe, et la liste la montre.
func TestUnCompteDejaConnuNEstPasOrphelin(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.wriker")
	connus := annuaire{"wriker": roster.Person{Username: "wriker"}}
	cours := groupe("a26", "5n6", "01", nil).Enrich(connus, inventaire)

	if orphelins := cours.Orphans(inventaire, nil); len(orphelins) != 0 {
		t.Fatalf("orphelins = %s", fragments(orphelins))
	}
}

// Le travail compte « hors liste » exactement les dépôts qu'il y a à rattacher.
// La marque de doublon de GitHub n'en fait pas un de plus : « jlpicard-1 » est
// le dépôt de @jlpicard, et sa ligne le nomme.
func TestLeDecompteDuTravailEstCeluiDesOrphelins(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.jlpicard-1", "a26.5n6.01.tp1.wriker", "a26.5n6.01.tp2.wriker")
	cours := groupe("a26", "5n6", "01", personnes("Jean-Luc Picard", "jlpicard"))

	horsListe := 0
	for _, travail := range cours.Assignments(inventaire, nil) {
		horsListe += travail.Others
	}
	aRattacher := 0
	for _, orphelin := range cours.Orphans(inventaire, nil) {
		aRattacher += len(orphelin.Repos)
	}
	if horsListe != 2 || aRattacher != horsListe {
		t.Fatalf("hors liste = %d, à rattacher = %d", horsListe, aRattacher)
	}
}

// D'un groupe à l'autre, un même dernier niveau ne se relève qu'une fois : le
// registre est celui de l'organisation, et rattacher là rattache partout.
func TestUnMemeFragmentDeDeuxGroupesNeSeReleveQuUneFois(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.wriker", "h26.4w6.02.tp1.wriker")
	premier := groupe("a26", "5n6", "01", nil).Orphans(inventaire, nil)
	second := groupe("h26", "4w6", "02", nil).Orphans(inventaire, nil)

	reunis := classroom.MergeOrphans(premier, second)
	if len(reunis) != 1 || len(reunis[0].Repos) != 2 {
		t.Fatalf("réunis = %+v", reunis)
	}
	// Deux « tp1 » de deux groupes ne font qu'un nom de travail.
	if strings.Join(reunis[0].Assignments, ",") != "tp1" {
		t.Errorf("travaux = %v", reunis[0].Assignments)
	}
}

// Le rattachement ne devine rien : il demande à GitHub. Ce qui est un compte
// devient une personne ; le reste attend une main.
func TestLeRattachementSepareCeQueGithubReconnait(t *testing.T) {
	orphelins := []classroom.Orphan{
		{Fragment: "dtroi-1", Repos: []string{"a26.5n6.01.tp1.dtroi-1"}},
		{Fragment: "william-riker", Repos: []string{"a26.5n6.01.tp1.william-riker"}},
		{Fragment: "WRiker", Repos: []string{"a26.5n6.01.tp1.WRiker", "a26.5n6.01.tp2.WRiker"}},
	}
	github := comptesGithub{
		"wriker": {Username: "wriker", FullName: "William Riker"},
		// Un profil sans nom : le compte suffit à rattacher le dépôt.
		"dtroi": {Username: "dtroi"},
	}

	plan, err := classroom.PlanAdoption(orphelins, github)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready() || len(plan.Found) != 2 || plan.Repos() != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	// La marque de doublon est retirée, et la casse est celle de GitHub.
	comptes := []string{}
	for _, personne := range plan.People() {
		comptes = append(comptes, personne.Username+"="+personne.FullName)
	}
	if strings.Join(comptes, ",") != "dtroi=,wriker=William Riker" {
		t.Errorf("personnes = %v", comptes)
	}
	if fragments(plan.Unknown) != "william-riker" {
		t.Errorf("sans compte = %s", fragments(plan.Unknown))
	}
}

// Un GitHub qui ne répond pas ne dit pas qu'un compte manque : le conclure
// ferait tenir un compte réel pour un slug, et l'écarter du rattachement.
func TestUnePanneNEstPasUneAbsence(t *testing.T) {
	_, err := classroom.PlanAdoption([]classroom.Orphan{{Fragment: "wriker"}}, enPanne{})
	if err == nil {
		t.Fatal("une panne doit arrêter le rattachement")
	}
}

// Un compte qui porte le slug d'un nom n'est pas forcément l'étudiant : ce
// qu'on écarte après avoir lu le profil ne s'écrit pas.
func TestSeulCeQuiEstRetenuSeRattache(t *testing.T) {
	plan, _ := classroom.PlanAdoption(
		[]classroom.Orphan{{Fragment: "dtroi"}, {Fragment: "wriker"}},
		comptesGithub{"dtroi": {Username: "dtroi"}, "wriker": {Username: "wriker"}})

	retenu := plan.Only([]string{"WRIKER"})
	if len(retenu.Found) != 1 || retenu.Found[0].Person.Username != "wriker" {
		t.Fatalf("retenu = %+v", retenu)
	}
	if rien := plan.Only(nil); rien.Ready() {
		t.Error("ne rien retenir ne doit rien écrire")
	}
}

// À la main, on dit à qui est un dépôt que GitHub ne reconnaît pas. Le compte
// donné est vérifié, et le nom vient de son profil à défaut d'être donné.
func TestRattacherALaMain(t *testing.T) {
	orphelins := []classroom.Orphan{{Fragment: "william-riker"}, {Fragment: "wriker"}}
	github := comptesGithub{"wriker": {Username: "wriker", FullName: "Will Riker"}}

	trouve, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "william-riker", Login: "wriker"}, nil, github)
	if err != nil {
		t.Fatal(err)
	}
	if trouve.Orphan.Fragment != "william-riker" || trouve.Person.FullName != "Will Riker" {
		t.Fatalf("rattachement = %+v", trouve)
	}
	// Le nom donné l'emporte sur celui du profil.
	trouve, err = classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "william-riker", Login: "wriker", FullName: "  William   Riker "}, nil, github)
	if err != nil || trouve.Person.FullName != "William Riker" {
		t.Fatalf("nom donné = %+v, %v", trouve.Person, err)
	}
	// Sans compte, c'est le dernier niveau lui-même qu'on rattache.
	if trouve, err = classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "wriker"}, nil, github); err != nil || trouve.Person.Username != "wriker" {
		t.Fatalf("sans compte = %+v, %v", trouve, err)
	}
}

// Ce qui n'a pas été relevé, et un compte qui n'existe pas, sont refusés : l'un
// écrirait au registre sur la foi d'un drapeau, l'autre y laisserait une fiche
// que rien ne désigne.
func TestRattacherALaMainRefuseCeQuiNeDesignePersonne(t *testing.T) {
	orphelins := []classroom.Orphan{{Fragment: "william-riker"}}
	github := comptesGithub{"wriker": {Username: "wriker"}}

	if _, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "dtroi", Login: "wriker"}, nil, github); err == nil {
		t.Error("un dernier niveau qu'aucun dépôt sans étudiant ne porte doit être refusé")
	}
	if _, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "william-riker"}, nil, github); err == nil {
		t.Error("un slug qui n'est pas un compte, sans compte donné, doit être refusé")
	}
	if _, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "william-riker", Login: "wriker"}, nil, enPanne{}); err == nil {
		t.Error("une panne ne doit pas laisser passer un compte non vérifié")
	}
}

// Un compte que le registre connaît se rattache sous le nom qu'il y porte,
// sans rien demander à GitHub : c'est souvent un ancien slug d'une personne
// connue, et le profil n'en sait pas davantage que le registre.
func TestRattacherAQuelquUnDeConnu(t *testing.T) {
	orphelins := []classroom.Orphan{{Fragment: "william-riker"}}
	connus := annuaire{
		"wriker": {Username: "wriker", FullName: "William T. Riker"},
		// Un slug du registre ne vaut pas compte : seul « wriker » en est un.
		"riker": {Username: "wriker", FullName: "William T. Riker"},
	}

	trouve, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "william-riker", Login: "wriker"}, connus, enPanne{})
	if err != nil || trouve.Person.FullName != "William T. Riker" {
		t.Fatalf("connu = %+v, %v", trouve, err)
	}
	if _, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: "william-riker", Login: "riker"}, connus, comptesGithub{}); err == nil {
		t.Error("un slug du registre ne doit pas tenir lieu de compte")
	}
}
