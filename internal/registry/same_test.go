package registry_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// deuxComptes est le cas de l'issue : une même personne, @Mr-Commeilfaut à une
// session, @commeilfautt à la suivante — chacun connu du registre sous son nom.
func deuxComptes(t *testing.T) *registry.Set {
	t.Helper()
	set, _, err := appliquer(t, registry.Empty(), registry.Learn(
		personne("Jean Commeilfaut", "Mr-Commeilfaut"),
		roster.Person{FullName: "Jean Comme-Tu-Veux", Username: "commeilfautt",
			StudentID: "2412345"}))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// Réunir deux comptes en fait une seule personne, désignée par celui qu'on a
// choisi de garder : son nom et son matricule l'emportent, et les slugs des
// deux mènent à elle.
func TestReunirDeuxComptesEnFaitUnePersonne(t *testing.T) {
	set, bouge, err := appliquer(t, deuxComptes(t),
		registry.Join("Mr-Commeilfaut", "commeilfautt"))
	if err != nil || !bouge {
		t.Fatalf("bouge = %v, err = %v", bouge, err)
	}

	if comptes := strings.Join(set.Accounts("mr-commeilfaut"), ","); comptes != "commeilfautt,Mr-Commeilfaut" {
		t.Fatalf("comptes = %s", comptes)
	}
	personne, _ := set.PersonOf("Mr-Commeilfaut")
	if personne.Username != "commeilfautt" || personne.FullName != "Jean Comme-Tu-Veux" ||
		personne.StudentID != "2412345" {
		t.Fatalf("personne = %+v", personne)
	}
	// Aucun dépôt ne se détache : ceux créés sous l'ancien nom mènent à elle.
	for _, slug := range []string{"jean-commeilfaut", "jean-comme-tu-veux", "mr-commeilfaut"} {
		trouvee, trouve := set.Resolve(slug)
		if !trouve || trouvee.Username != "commeilfautt" {
			t.Errorf("%s → %+v", slug, trouvee)
		}
	}
	// « classroom » apprend l'autre compte par là.
	lue, _ := set.Lookup("jean-commeilfaut")
	if lue.Username != "commeilfautt" || strings.Join(lue.Also, ",") != "Mr-Commeilfaut" {
		t.Errorf("lookup = %+v", lue)
	}
	// Rien n'est effacé : chaque fiche garde ce qu'elle portait.
	if fiche, _ := set.Find("Mr-Commeilfaut"); fiche.FullName != "Jean Commeilfaut" ||
		fiche.SameAs != "commeilfautt" {
		t.Errorf("fiche = %+v", fiche)
	}
}

// La fusion se défait, et chaque fiche retrouve exactement ce qu'elle portait.
func TestSeparerDefaitLaReunion(t *testing.T) {
	avant := deuxComptes(t)
	reunis, _, err := appliquer(t, avant, registry.Join("Mr-Commeilfaut", "commeilfautt"))
	if err != nil {
		t.Fatal(err)
	}
	// Séparer depuis l'un ou l'autre compte revient au même.
	for _, compte := range []string{"Mr-Commeilfaut", "commeilfautt"} {
		separes, bouge, err := appliquer(t, reunis, registry.Split(compte))
		if err != nil || !bouge {
			t.Fatalf("%s : bouge = %v, err = %v", compte, bouge, err)
		}
		attendu, _ := avant.Encode()
		obtenu, _ := separes.Encode()
		if string(attendu) != string(obtenu) {
			t.Errorf("%s :\n%s\nau lieu de\n%s", compte, obtenu, attendu)
		}
		if separes.Name("Mr-Commeilfaut") != "Jean Commeilfaut" {
			t.Errorf("%s : nom = %q", compte, separes.Name("Mr-Commeilfaut"))
		}
	}
}

// Redire une réunion qui existe, ou défaire une qui n'existe plus, n'écrit
// rien : c'est ce qui rend le rejeu sûr.
func TestReunirDeuxFoisNeBougePas(t *testing.T) {
	set, _, err := appliquer(t, deuxComptes(t), registry.Join("Mr-Commeilfaut", "commeilfautt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []registry.Change{
		registry.Join("Mr-Commeilfaut", "commeilfautt"),
		registry.Join("commeilfautt", "Mr-Commeilfaut"),
	} {
		if _, bouge, err := appliquer(t, set, change); err != nil || bouge {
			t.Errorf("%s : bouge = %v, err = %v", change.Reason, bouge, err)
		}
	}
	if _, bouge, err := appliquer(t, deuxComptes(t), registry.Split("commeilfautt")); err != nil || bouge {
		t.Errorf("séparer un compte seul : bouge = %v, err = %v", bouge, err)
	}
}

// Un compte que le registre ignore y entre, nu : c'est le cas de l'enseignant à
// deux comptes, qui ne figure sur aucune liste.
func TestReunirUnCompteInconnuLeFaitConnaitre(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Join("prof-perso", "prof"))
	if err != nil {
		t.Fatal(err)
	}
	if set.Len() != 2 || strings.Join(set.Accounts("prof-perso"), ",") != "prof,prof-perso" {
		t.Fatalf("comptes = %v", set.Accounts("prof-perso"))
	}
}

// Si l'un des comptes enseigne, la personne enseigne ; retirer le rôle
// l'atteint sous tous ses comptes.
func TestLeRoleEstCeluiDeLaPersonne(t *testing.T) {
	set, _, err := appliquer(t, deuxComptes(t), registry.SetRole("Mr-Commeilfaut", true))
	if err != nil {
		t.Fatal(err)
	}
	set, _, err = appliquer(t, set, registry.Join("Mr-Commeilfaut", "commeilfautt"))
	if err != nil {
		t.Fatal(err)
	}
	if !set.Teaches("commeilfautt") {
		t.Fatal("la personne enseigne : l'un de ses comptes le fait")
	}
	if enseignants := set.Teachers(); len(enseignants) != 1 || enseignants[0].Username != "commeilfautt" {
		t.Fatalf("enseignants = %+v", enseignants)
	}
	set, _, err = appliquer(t, set, registry.SetRole("commeilfautt", false))
	if err != nil {
		t.Fatal(err)
	}
	if set.Teaches("Mr-Commeilfaut") {
		t.Error("retirer le rôle doit atteindre tous ses comptes")
	}
}

// Deux matricules différents sont deux personnes pour le collège.
func TestDeuxMatriculesNeSeReunissentPas(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Learn(
		roster.Person{FullName: "Jean Tremblay", Username: "jtremblay", StudentID: "1"},
		roster.Person{FullName: "Jean Tremblay", Username: "jtremblay2", StudentID: "2"}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = appliquer(t, set, registry.Join("jtremblay2", "jtremblay"))
	if err == nil || !strings.Contains(err.Error(), "deux personnes") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := appliquer(t, set, registry.Join("jtremblay", "JTremblay")); err == nil {
		t.Error("un compte ne se réunit pas à lui-même")
	}
}

// Trois comptes : en séparer celui qui désigne laisse les deux autres ensemble.
func TestSeparerCeluiQuiDesigneGardeLesAutresEnsemble(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Change{Links: []registry.Link{
		{Username: "b", SameAs: "a"}, {Username: "c", SameAs: "a"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	set, _, err = appliquer(t, set, registry.Split("a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Accounts("a")) != 1 || strings.Join(set.Accounts("c"), ",") != "b,c" {
		t.Fatalf("a = %v, c = %v", set.Accounts("a"), set.Accounts("c"))
	}
	// Oublier le compte qui désigne ne sépare pas non plus les autres.
	set, _, err = appliquer(t, set, registry.Change{Forget: []string{"b"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(set.Accounts("c"), ",") != "c" {
		t.Fatalf("c = %v", set.Accounts("c"))
	}
}

// Le fichier se modifie à la main : un renvoi vers un inconnu se signale et
// s'ignore, une boucle désigne la même fiche d'où qu'on y entre, et la version
// n'avance que lorsqu'un renvoi l'exige.
func TestUnRenvoiEcritALaMainSeRelit(t *testing.T) {
	set, soucis := registry.Decode([]byte(`{"version": 3, "users": [
		{"username": "a", "full_name": "Anne A", "same_as": "b"},
		{"username": "b", "same_as": "a"},
		{"username": "c", "same_as": "fantome"},
		{"username": "d", "same_as": "pas un compte!"}
	]}`))
	if set.Len() != 4 {
		t.Fatalf("%d fiche(s)", set.Len())
	}
	if strings.Join(set.Accounts("b"), ",") != "a,b" || strings.Join(set.Accounts("a"), ",") != "a,b" {
		t.Errorf("a = %v, b = %v", set.Accounts("a"), set.Accounts("b"))
	}
	if len(set.Accounts("c")) != 1 || set.Name("b") != "Anne A" {
		t.Errorf("c = %v, nom de b = %q", set.Accounts("c"), set.Name("b"))
	}
	if len(soucis) != 2 || !strings.Contains(soucis[0], "pas un compte!") ||
		!strings.Contains(soucis[1], "@fantome") {
		t.Errorf("soucis = %v", soucis)
	}

	// Sans renvoi, on écrit toujours la version 2 : un poste resté en arrière
	// n'a pas à s'alarmer pour rien.
	seul, _ := deuxComptes(t).Encode()
	if !strings.Contains(string(seul), `"version": 2`) {
		t.Errorf("sans renvoi :\n%s", seul)
	}
	reunis, _, _ := appliquer(t, deuxComptes(t), registry.Join("Mr-Commeilfaut", "commeilfautt"))
	avec, _ := reunis.Encode()
	if !strings.Contains(string(avec), `"version": 3`) ||
		!strings.Contains(string(avec), `"same_as": "commeilfautt"`) {
		t.Errorf("avec renvoi :\n%s", avec)
	}
	relu, soucis := registry.Decode(avec)
	if len(soucis) != 0 || len(relu.Accounts("commeilfautt")) != 2 {
		t.Errorf("relu : %v, %v", relu.Accounts("commeilfautt"), soucis)
	}
}

// Une liste réimportée ne réunit ni ne sépare personne.
func TestApprendreNePoseAucunRenvoi(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Change{Learn: []registry.User{
		{Username: "x", SameAs: "y"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if fiche, _ := set.Find("x"); fiche.SameAs != "" {
		t.Errorf("fiche = %+v", fiche)
	}
	reunis, _, _ := appliquer(t, deuxComptes(t), registry.Join("Mr-Commeilfaut", "commeilfautt"))
	reappris, _, _ := appliquer(t, reunis, registry.Learn(personne("Jean Commeilfaut", "Mr-Commeilfaut")))
	if len(reappris.Accounts("Mr-Commeilfaut")) != 2 {
		t.Error("réapprendre quelqu'un ne doit pas défaire sa réunion")
	}
}
