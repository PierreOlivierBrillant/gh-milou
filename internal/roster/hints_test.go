package roster_test

import (
	"slices"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// Le nom du fichier d'Omnivox porte le cours et le groupe.
func TestLeNomDuFichierDitLeCoursEtLeGroupe(t *testing.T) {
	indices := roster.HintsFrom("ListeEtudiants_cours4203N5EM_gr1040.csv", nil)
	if indices.Course != "3N5" {
		t.Fatalf("cours = %q", indices.Course)
	}
	if indices.Group != "1040" {
		t.Fatalf("groupe = %q", indices.Group)
	}
}

// Un chemin complet se lit comme un nom seul : un fichier déposé dans la page
// n'a que son nom, un fichier choisi a tout son chemin.
func TestUnCheminCompletSeLitCommeUnNom(t *testing.T) {
	for _, chemin := range []string{
		"/home/prof/Téléchargements/ListeEtudiants_cours4204N6EM_gr1030.csv",
		`C:\Users\prof\Downloads\ListeEtudiants_cours4204N6EM_gr1030.csv`,
	} {
		if indices := roster.HintsFrom(chemin, nil); indices.Course != "4N6" {
			t.Fatalf("%s → cours = %q", chemin, indices.Course)
		}
	}
}

// La colonne « Groupe » passe avant le nom du fichier : elle suit chaque
// étudiant, là où le nom a pu être changé.
func TestLaColonneGroupePasseAvantLeNomDuFichier(t *testing.T) {
	entrees := []roster.Entry{
		{FullName: "Édouard Loiselle", Group: "1030"},
		{FullName: "Félix Boisvert", Group: "1030"},
	}
	indices := roster.HintsFrom("ListeEtudiants_cours4203N5EM_gr9999.csv", entrees)
	if indices.Group != "1030" || len(indices.Groups) != 0 {
		t.Fatalf("indices = %+v", indices)
	}
}

// Une liste qui mêle deux groupes n'en désigne aucun : les nommer laisse le
// choix à qui saura trancher.
func TestUneListeQuiMeleDeuxGroupesNEnChoisitAucun(t *testing.T) {
	entrees := []roster.Entry{
		{FullName: "Édouard Loiselle", Group: "1040"},
		{FullName: "Félix Boisvert", Group: "1030"},
		{FullName: "Mei Chen", Group: "1040"},
	}
	indices := roster.HintsFrom("ListeEtudiants.csv", entrees)
	if indices.Group != "" {
		t.Fatalf("un groupe a été choisi malgré le mélange : %+v", indices)
	}
	if !slices.Equal(indices.Groups, []string{"1030", "1040"}) {
		t.Fatalf("groupes = %v", indices.Groups)
	}
}

// Un nom de fichier quelconque ne dit rien, et se taire vaut mieux qu'inventer.
func TestUnNomQuelconqueNeDitRien(t *testing.T) {
	indices := roster.HintsFrom("cohorte.csv", nil)
	if indices.Course != "" || indices.Group != "" || len(indices.Groups) != 0 {
		t.Fatalf("indices inventés : %+v", indices)
	}
}
