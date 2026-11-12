package runner_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Tout chemin du programme qui demande à l'exécuteur de signer lui donne aussi
// le registre de l'organisation. C'est l'oubli qu'ont fait, chacun à son tour,
// l'ajout de dépôts au terminal et l'ajout d'une personne au navigateur : des
// dépôts signés dont la marque ne désignait personne. L'exécuteur dit déjà
// qu'aucun registre ne la reçoit ; ce test refuse qu'on en arrive là.
func TestToutCheminQuiSigneDonneLeRegistre(t *testing.T) {
	racine := filepath.Join("..", "..")
	fichiers := token.NewFileSet()
	signants := 0
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
			litteral, ok := noeud.(*ast.CompositeLit)
			if !ok || !estOptionsDuRunner(litteral.Type) {
				return true
			}
			champs := map[string]bool{}
			for _, element := range litteral.Elts {
				if paire, ok := element.(*ast.KeyValueExpr); ok {
					if cle, ok := paire.Key.(*ast.Ident); ok {
						champs[cle.Name] = true
					}
				}
			}
			if !champs["Sign"] {
				return true
			}
			signants++
			if !champs["Registry"] {
				t.Errorf("%s : l'exécuteur y signe sans recevoir le registre — rien ne "+
					"dirait à qui ses marques appartiennent", fichiers.Position(litteral.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Un test qui ne trouverait aucun chemin ne veillerait sur rien.
	if signants == 0 {
		t.Fatal("aucun chemin ne demande de signer : la recherche ne voit plus rien")
	}
}

// estOptionsDuRunner reconnaît « runner.Options ». Le paquet lui-même n'en
// compose aucune : ce sont ses appelants qui décident de signer.
func estOptionsDuRunner(expression ast.Expr) bool {
	selection, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	paquet, ok := selection.X.(*ast.Ident)
	return ok && paquet.Name == "runner" && selection.Sel.Name == "Options"
}
