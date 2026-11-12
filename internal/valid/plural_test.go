package valid_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Le français met le singulier jusqu'à un, zéro compris : « 0 dépôt »,
// « 1 dépôt », « 2 dépôts ». C'est la règle de « pluriel() » dans la page.
func TestUnNombreSAccordeCommeEnFrancais(t *testing.T) {
	cas := []struct {
		nombre  int
		attendu string
	}{
		{0, "0 dépôt"}, {1, "1 dépôt"}, {2, "2 dépôts"}, {19, "19 dépôts"},
	}
	for _, c := range cas {
		if obtenu := valid.Plural(c.nombre, "dépôt"); obtenu != c.attendu {
			t.Errorf("Plural(%d) = %q, attendu %q", c.nombre, obtenu, c.attendu)
		}
	}
	// Une forme donnée l'emporte : un mot invariable, une phrase entière.
	if obtenu := valid.Plural(3, "index", "index"); obtenu != "3 index" {
		t.Errorf("Plural(3, index) = %q", obtenu)
	}
	if obtenu := valid.Plural(2, "dépôt n'a pas pu être lu", "dépôts n'ont pas pu être lus"); obtenu != "2 dépôts n'ont pas pu être lus" {
		t.Errorf("Plural(2, phrase) = %q", obtenu)
	}
	if obtenu := valid.Agree(1, "renommé"); obtenu != "renommé" {
		t.Errorf("Agree(1) = %q", obtenu)
	}
	if obtenu := valid.Agree(4, "a", "ont"); obtenu != "ont" {
		t.Errorf("Agree(4) = %q", obtenu)
	}
}

// Aucun texte du programme ne laisse l'accord au lecteur : un « (s) » dans une
// chaîne se remplace par Plural ou Agree. Les commentaires n'en sont pas : ils
// ne s'affichent pas.
func TestAucunTexteNeLaisseLAccordAuLecteur(t *testing.T) {
	racine := filepath.Join("..", "..")
	fichiers := token.NewFileSet()
	err := filepath.WalkDir(racine, func(chemin string, entree fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entree.IsDir() {
			// Les dossiers cachés — « .git », « .codegraph » — ne sont pas du
			// programme.
			if strings.HasPrefix(entree.Name(), ".") && chemin != racine {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(chemin, ".go") || strings.HasSuffix(chemin, "_test.go") {
			return nil
		}
		arbre, err := parser.ParseFile(fichiers, chemin, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(arbre, func(noeud ast.Node) bool {
			if texte, ok := noeud.(*ast.BasicLit); ok && texte.Kind == token.STRING &&
				strings.Contains(texte.Value, "(s)") {
				t.Errorf("%s : %s laisse l'accord au lecteur",
					fichiers.Position(texte.Pos()), texte.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
