package users_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
)

// cohorte déclare le groupe qui sert de décor à ces tests : trois personnes,
// deux travaux, et des envois à des dates différentes.
func cohorte() (classroom.Classroom, []groups.RepoInfo) {
	cours := classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
			{FullName: "Émilie Côté", Username: "ecote"},
			{FullName: "Aminata Diallo", Username: "aminata-d"},
		},
		Defaults: classroom.DefaultsFrom(config.Default()),
	}
	inventaire := []groups.RepoInfo{
		{Name: "a26.5n6.01.tp1.jean-luc-picard", PushedAt: "2026-09-01T10:00:00Z"},
		{Name: "a26.5n6.01.tp2.jean-luc-picard", PushedAt: "2026-10-15T10:00:00Z"},
		{Name: "a26.5n6.01.tp1.emilie-cote", PushedAt: "2026-09-20T10:00:00Z"},
		// Aminata a un dépôt, mais n'y a jamais rien envoyé.
		{Name: "a26.5n6.01.tp1.aminata-diallo"},
	}
	return cours, inventaire
}

// build dresse les lignes sans aucune équipe : ces tests-ci ne portent que sur
// des travaux individuels.
func build(cours classroom.Classroom, repos []groups.RepoInfo) []users.Row {
	return users.Build(cours, repos, nil)
}

// envoi met une date rendue par GitHub sous la forme que portent les lignes :
// l'heure de la machine, à la minute.
func envoi(iso string) string {
	moment, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		panic(err)
	}
	return moment.Local().Format("2006-01-02 15:04")
}

// comptes rend les comptes d'une liste, dans l'ordre où elle les donne.
func comptes(lignes []users.Row) string {
	noms := make([]string, 0, len(lignes))
	for _, ligne := range lignes {
		noms = append(noms, ligne.Username)
	}
	return strings.Join(noms, ",")
}

func TestLigneRetientLePlusRecentEnvoi(t *testing.T) {
	lignes := build(cohorte())
	trouve := map[string]users.Row{}
	for _, ligne := range lignes {
		trouve[ligne.Username] = ligne
	}
	if picard := trouve["jlpicard"]; picard.PushedAt != envoi("2026-10-15T10:00:00Z") || len(picard.Repos) != 2 {
		t.Fatalf("Picard : %+v", picard)
	}
	// Un dépôt sans envoi ne donne pas de date : c'est ce qui distingue « muet »
	// de « n'a rien ».
	if aminata := trouve["aminata-d"]; aminata.PushedAt != "" || len(aminata.Repos) != 1 {
		t.Fatalf("Aminata : %+v", aminata)
	}
}

func TestRechercheIgnoreCasseEtAccents(t *testing.T) {
	lignes := build(cohorte())
	for _, cherche := range []string{"cote", "CÔTÉ", "Émilie"} {
		retenues := users.Apply(lignes, users.Filter{Text: cherche}, users.ByName, false)
		if comptes(retenues) != "ecote" {
			t.Fatalf("« %s » : %s", cherche, comptes(retenues))
		}
	}
	// Le compte se cherche à part du nom.
	retenues := users.Apply(lignes, users.Filter{Username: "picard"}, users.ByName, false)
	if comptes(retenues) != "jlpicard" {
		t.Fatalf("par compte : %s", comptes(retenues))
	}
	if comptes(users.Apply(lignes,
		users.Filter{Name: "picard"}, users.ByName, false)) != "jlpicard" {
		t.Fatal("« picard » est aussi dans le nom complet")
	}
}

func TestBornesDuDernierEnvoi(t *testing.T) {
	lignes := build(cohorte())

	apres := users.Apply(lignes,
		users.Filter{PushedAfter: "2026-10-01"}, users.ByName, false)
	if comptes(apres) != "jlpicard" {
		t.Fatalf("après le 1er octobre : %s", comptes(apres))
	}
	avant := users.Apply(lignes,
		users.Filter{PushedBefore: "2026-09-30"}, users.ByName, false)
	if comptes(avant) != "ecote" {
		t.Fatalf("avant le 30 septembre : %s", comptes(avant))
	}
	// Sans date connue, une personne n'est ni avant ni après : elle se retrouve
	// par « muet », pas par une borne.
	muets := users.Apply(lignes,
		users.Filter{Activity: users.Silent}, users.ByName, false)
	if comptes(muets) != "aminata-d" {
		t.Fatalf("muets : %s", comptes(muets))
	}
}

func TestActiviteEtTravail(t *testing.T) {
	cours, inventaire := cohorte()
	cours.Students = append(cours.Students,
		roster.Person{FullName: "Zoé Tremblay", Username: "ztremblay"})
	lignes := build(cours, inventaire)

	sans := users.Apply(lignes,
		users.Filter{Activity: users.WithoutRepos}, users.ByName, false)
	if comptes(sans) != "ztremblay" {
		t.Fatalf("sans dépôt : %s", comptes(sans))
	}
	avec := users.Apply(lignes,
		users.Filter{Activity: users.WithRepos}, users.ByUsername, false)
	if comptes(avec) != "aminata-d,ecote,jlpicard" {
		t.Fatalf("avec dépôt : %s", comptes(avec))
	}
	tp2 := users.Apply(lignes,
		users.Filter{Assignment: "tp2"}, users.ByName, false)
	if comptes(tp2) != "jlpicard" {
		t.Fatalf("tp2 : %s", comptes(tp2))
	}
}

func TestTriParNomCompteEtEnvoi(t *testing.T) {
	lignes := build(cohorte())

	// Les accents ne dispersent pas l'ordre : « Émilie » se range avec les E.
	if ordre := comptes(users.Apply(lignes, users.Filter{},
		users.ByName, false)); ordre != "aminata-d,ecote,jlpicard" {
		t.Fatalf("par nom : %s", ordre)
	}
	if ordre := comptes(users.Apply(lignes, users.Filter{},
		users.ByUsername, true)); ordre != "jlpicard,ecote,aminata-d" {
		t.Fatalf("par compte, décroissant : %s", ordre)
	}
	// Sans date, Aminata se range avec les plus anciens : en queue d'un tri
	// décroissant, en tête d'un tri croissant.
	if ordre := comptes(users.Apply(lignes, users.Filter{},
		users.ByPushed, true)); ordre != "jlpicard,ecote,aminata-d" {
		t.Fatalf("par envoi, décroissant : %s", ordre)
	}
	if ordre := comptes(users.Apply(lignes, users.Filter{},
		users.ByPushed, false)); ordre != "aminata-d,ecote,jlpicard" {
		t.Fatalf("par envoi, croissant : %s", ordre)
	}
}

func TestFiltreRefuseCeQuIlNePeutPasAppliquer(t *testing.T) {
	if _, err := (users.Filter{PushedAfter: "1er octobre"}).Validate(); err == nil {
		t.Fatal("une date qui n'en est pas une doit être refusée")
	}
	if _, err := (users.Filter{
		PushedAfter: "2026-10-01", PushedBefore: "2026-09-01"}).Validate(); err == nil {
		t.Fatal("des bornes inversées doivent être refusées")
	}
	if _, err := (users.Filter{Activity: "bavard"}).Validate(); err == nil {
		t.Fatal("une activité inconnue doit être refusée")
	}
	if _, err := users.ParseKey("popularite"); err == nil {
		t.Fatal("un tri inconnu doit être refusé")
	}
	// Ce qui est accepté revient normalisé.
	filtre, err := (users.Filter{
		PushedAfter: "2026-10-01", Activity: "AVEC", Text: "  Côté "}).Validate()
	if err != nil || filtre.Activity != users.WithRepos || filtre.Text != "Côté" {
		t.Fatalf("normalisation : %+v (%v)", filtre, err)
	}
}

func TestLignesDUnGroupeLuParPrefixe(t *testing.T) {
	group := groups.Build("tp1", []groups.RepoInfo{
		{Name: "tp1-jlpicard", PushedAt: "2026-09-01T10:00:00Z"},
		{Name: "tp1-ecote"},
	})
	lignes := users.FromGroup(group, map[string]string{"tp1-jlpicard": "Jean-Luc Picard"})
	if ordre := comptes(users.Apply(lignes, users.Filter{},
		users.ByPushed, true)); ordre != "jlpicard,ecote" {
		t.Fatalf("par envoi : %s", ordre)
	}
	// Le nom complet vient d'ailleurs que du dépôt ; sans lui, seul le suffixe
	// permet de retrouver la personne.
	if retenues := users.Apply(lignes, users.Filter{Text: "picard"},
		users.ByName, false); comptes(retenues) != "jlpicard" {
		t.Fatalf("recherche : %s", comptes(retenues))
	}
}

// Le dépôt d'une équipe est celui de chacun de ses membres : sans cela, un
// travail fait en équipe laisserait tout le monde à « aucun dépôt », alors que
// tout le monde en a un.
func TestUnDepotDEquipeCompteChezChacunDeSesMembres(t *testing.T) {
	cours, inventaire := cohorte()
	inventaire = append(inventaire, groups.RepoInfo{
		Name: "a26.5n6.01.projet.eq1", PushedAt: "2026-10-02T08:00:00Z",
	})
	equipes := cours.Teams([]teams.Info{{
		Slug: "a26-5n6-01-eq1", Name: "a26.5n6.01.eq1",
		Members: []string{"ecote", "aminata-d"},
	}})

	lignes := users.Build(cours, inventaire, equipes)
	trouve := map[string]users.Row{}
	for _, ligne := range lignes {
		trouve[ligne.Username] = ligne
	}
	for _, compte := range []string{"ecote", "aminata-d"} {
		partage := false
		for _, depot := range trouve[compte].Repos {
			if depot.Name != "a26.5n6.01.projet.eq1" {
				continue
			}
			partage = true
			if depot.Team != "eq1" {
				t.Fatalf("le dépôt devrait nommer son équipe : %+v", depot)
			}
		}
		if !partage {
			t.Fatalf("@%s devrait avoir le dépôt de son équipe : %+v",
				compte, trouve[compte].Repos)
		}
	}
	// Aminata n'avait jamais rien envoyé : l'envoi de son équipe devient le sien.
	if trouve := trouve["aminata-d"].PushedAt; trouve != envoi("2026-10-02T08:00:00Z") {
		t.Fatalf("dernier envoi d'Aminata attendu du dépôt d'équipe, trouvé %q", trouve)
	}
	// Jean-Luc n'est dans aucune équipe : ses deux dépôts restent les siens.
	if depots := trouve["jlpicard"].Repos; len(depots) != 2 {
		t.Fatalf("@jlpicard ne devrait rien recevoir d'une équipe : %+v", depots)
	}
}

// Les dépôts de qui n'a pas encore de compte sont les siens : c'est son nom qui
// les nomme, et son matricule qui la désigne. Rangés sous un compte vide, ils
// n'étaient à personne — la liste disait « aucun dépôt », et le déplacement
// annonçait n'en renommer aucun avant de les renommer.
func TestLesDepotsDeQuiNAPasDeCompteSontLesSiens(t *testing.T) {
	cours := classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Nadia Pépin", StudentID: "2100110"},
			{FullName: "Claire Otis", StudentID: "2100789"},
		},
		Defaults: classroom.DefaultsFrom(config.Default()),
	}
	inventaire := []groups.RepoInfo{
		{Name: "a26.5n6.01.tp1.nadia-pepin", PushedAt: "2026-09-12T08:00:00Z"},
		{Name: "a26.5n6.01.tp2.claire-otis"},
	}

	trouve := map[string]users.Row{}
	for _, ligne := range build(cours, inventaire) {
		trouve[ligne.FullName] = ligne
	}
	nadia, claire := trouve["Nadia Pépin"], trouve["Claire Otis"]
	if len(nadia.Repos) != 1 || nadia.Repos[0].Name != "a26.5n6.01.tp1.nadia-pepin" ||
		nadia.PushedAt != envoi("2026-09-12T08:00:00Z") {
		t.Errorf("Nadia : %+v", nadia)
	}
	if len(claire.Repos) != 1 || claire.Repos[0].Name != "a26.5n6.01.tp2.claire-otis" {
		t.Errorf("Claire : %+v", claire)
	}
}
