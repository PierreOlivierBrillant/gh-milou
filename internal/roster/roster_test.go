package roster_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

func TestParseEnTetesFrancais(t *testing.T) {
	liste := roster.Parse("nom_complet,github_username\nÉmilie Côté,emilie-cote\nJean-Luc Picard,jlpicard\n")
	if len(liste.People) != 2 || len(liste.Issues) != 0 {
		t.Fatalf("liste = %+v", liste)
	}
	if liste.People[0].FullName != "Émilie Côté" || liste.People[0].Username != "emilie-cote" {
		t.Errorf("première personne = %+v", liste.People[0])
	}
	if !liste.IsValid() {
		t.Error("la liste devrait être valide")
	}
}

func TestParseEnTetesAnglais(t *testing.T) {
	liste := roster.Parse("login;name\njlpicard;Jean-Luc Picard\n")
	if len(liste.People) != 1 {
		t.Fatalf("liste = %+v", liste)
	}
	if liste.People[0].FullName != "Jean-Luc Picard" || liste.People[0].Username != "jlpicard" {
		t.Errorf("colonnes inversées mal détectées : %+v", liste.People[0])
	}
}

func TestParseSansEnTete(t *testing.T) {
	liste := roster.Parse("Émilie Côté,emilie-cote\nJean-Luc Picard,jlpicard\n")
	if len(liste.People) != 2 || len(liste.Issues) != 0 {
		t.Fatalf("liste = %+v", liste)
	}
}

func TestParseSeparateurs(t *testing.T) {
	cas := map[string]string{
		"virgule":    "nom,github\nÉmilie Côté,emilie-cote\n",
		"pointvirg":  "nom;github\nÉmilie Côté;emilie-cote\n",
		"tabulation": "nom\tgithub\nÉmilie Côté\temilie-cote\n",
	}
	for nom, contenu := range cas {
		liste := roster.Parse(contenu)
		if len(liste.People) != 1 || liste.People[0].Username != "emilie-cote" {
			t.Errorf("%s : %+v", nom, liste)
		}
	}
}

func TestParseIgnoreVidesEtCommentaires(t *testing.T) {
	liste := roster.Parse("nom,github\n\n# une remarque,ignorée\nÉmilie Côté,emilie-cote\n\n")
	if len(liste.People) != 1 || len(liste.Issues) != 0 {
		t.Fatalf("liste = %+v", liste)
	}
}

func TestParseRejetsAvecNumeroDeLigne(t *testing.T) {
	liste := roster.Parse(strings.Join([]string{
		"nom_complet,github_username",
		"Émilie Côté,emilie-cote",
		"Sans compte,",
		"Compte invalide,-mauvais-",
		"Doublon,EMILIE-COTE",
		",orphelin",
		"Une seule colonne",
	}, "\n"))

	if len(liste.People) != 1 {
		t.Fatalf("%d personne(s) valide(s) : %+v", len(liste.People), liste.People)
	}
	if len(liste.Issues) != 5 {
		t.Fatalf("%d rejet(s) : %+v", len(liste.Issues), liste.Issues)
	}
	lignes := []int{3, 4, 5, 6, 7}
	for index, issue := range liste.Issues {
		if issue.Line != lignes[index] {
			t.Errorf("rejet %d à la ligne %d, attendu %d (%s)", index, issue.Line, lignes[index], issue.Message)
		}
		if issue.Message == "" {
			t.Errorf("rejet %d sans motif", index)
		}
	}
	if !strings.Contains(liste.Issues[2].Message, "déjà présent") {
		t.Errorf("le doublon doit être signalé comme tel : %s", liste.Issues[2].Message)
	}
	if liste.IsValid() {
		t.Error("une liste avec des rejets n'est pas valide")
	}
}

func TestParseVide(t *testing.T) {
	liste := roster.Parse("   \n\n")
	if len(liste.People) != 0 || len(liste.Issues) != 1 {
		t.Fatalf("liste = %+v", liste)
	}
}

func TestParseQueDesCommentaires(t *testing.T) {
	liste := roster.Parse("# rien ici\n")
	if len(liste.People) != 0 || len(liste.Issues) != 1 {
		t.Fatalf("liste = %+v", liste)
	}
}

func TestLoadEtWrite(t *testing.T) {
	dossier := t.TempDir()
	chemin := filepath.Join(dossier, "cohorte.csv")
	gens := []roster.Person{
		{FullName: "Émilie Côté", Username: "emilie-cote"},
		{FullName: "Jean-Luc Picard", Username: "jlpicard"},
	}
	if _, err := roster.Write(chemin, gens); err != nil {
		t.Fatalf("Write : %v", err)
	}
	liste, err := roster.Load(chemin)
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if len(liste.People) != 2 || liste.People[1].Username != "jlpicard" {
		t.Fatalf("relecture = %+v", liste.People)
	}
}

func TestLoadFichierAbsent(t *testing.T) {
	if _, err := roster.Load(filepath.Join(t.TempDir(), "absent.csv")); err == nil {
		t.Error("un fichier absent doit produire une erreur")
	}
}

func TestLoadAvecBOM(t *testing.T) {
	dossier := t.TempDir()
	chemin := filepath.Join(dossier, "bom.csv")
	contenu := "\ufeffnom_complet,github_username\nÉmilie Côté,emilie-cote\n"
	if err := os.WriteFile(chemin, []byte(contenu), 0o644); err != nil {
		t.Fatal(err)
	}
	liste, err := roster.Load(chemin)
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if len(liste.People) != 1 {
		t.Fatalf("le BOM doit être ignoré : %+v", liste)
	}
}

// Une liste écrite sous Windows arrive en Windows-1252, et la refuser
// obligerait à la convertir à la main avant de s'en servir.
func TestLoadAccepteWindows1252(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "latin1.csv")
	// « Émilie Côté » en Windows-1252 : un octet par lettre accentuée.
	contenu := []byte("nom,github\r\n\xc9milie C\xf4t\xe9,emilie-cote\r\n")
	if err := os.WriteFile(chemin, contenu, 0o644); err != nil {
		t.Fatal(err)
	}
	liste, err := roster.Load(chemin)
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if len(liste.People) != 1 || liste.People[0].FullName != "Émilie Côté" {
		t.Fatalf("liste = %+v", liste.People)
	}
}

func TestMarqueDeDoublonRetiree(t *testing.T) {
	cas := []struct {
		compte string
		base   string
		marque bool
	}{
		{"aleksilesaj-1", "aleksilesaj", true},
		{"emilie-cote-12", "emilie-cote", true},
		// Un compte peut légitimement finir par un nombre : rien dans le nom
		// seul ne dit lequel des deux on lit.
		{"CO-7", "CO", true},
		{"jlpicard", "jlpicard", false},
		// Rien à retirer : il ne resterait rien.
		{"2100104", "2100104", false},
		{"-1", "-1", false},
	}
	for _, essai := range cas {
		base, marque := roster.WithoutDuplicateMarker(essai.compte)
		if base != essai.base || marque != essai.marque {
			t.Errorf("%q → %q (%v), attendu %q (%v)",
				essai.compte, base, marque, essai.base, essai.marque)
		}
	}
}

func TestDeuxNomsCompletsPeuventEtreLaMemePersonne(t *testing.T) {
	if !roster.SameName("Alexis Lesage", "alexis lesage") {
		t.Error("la casse ne fait pas deux personnes")
	}
	if !roster.SameName("", "Alexis Lesage") || !roster.SameName("Alexis Lesage", "  ") {
		t.Error("un nom encore inconnu ne contredit rien")
	}
	if roster.SameName("Alexis Lesage", "Émilie Côté") {
		t.Error("deux noms différents sont deux personnes")
	}
}
