package web

import (
	"regexp"
	"strings"
	"testing"
)

// Le script de la page est global : deux fonctions du même nom ne s'ajoutent
// pas, la dernière déclarée remplace l'autre partout, sans erreur ni
// avertissement. Une fonction qui lisait un fichier déposé a ainsi été
// remplacée par celle qui écrit une taille, et le fichier partait sous la
// forme « 0 o ».
func TestLeScriptNeDeclareAucuneFonctionDeuxFois(t *testing.T) {
	script, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	declaration := regexp.MustCompile(`^(?:async\s+)?function\s+([\p{L}\p{N}_$]+)\s*\(`)
	premieres := map[string]int{}
	for numero, ligne := range strings.Split(string(script), "\n") {
		trouve := declaration.FindStringSubmatch(ligne)
		if trouve == nil {
			continue
		}
		if avant, deja := premieres[trouve[1]]; deja {
			t.Errorf("app.js:%d redéclare %s(), déjà déclarée ligne %d : l'une des deux disparaît",
				numero+1, trouve[1], avant)
			continue
		}
		premieres[trouve[1]] = numero + 1
	}
}

// Une zone de dépôt se clique par un vrai bouton. Une zone qui se ferait passer
// pour un bouton ne le serait pas aux yeux du navigateur : posée dans un
// <label>, son clic partirait aussi vers le premier bouton du label.
func TestChaqueZoneDeDepotPorteSonBouton(t *testing.T) {
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	zones := strings.Split(html, "data-depot")[1:]
	if len(zones) == 0 {
		t.Fatal("aucune zone de dépôt dans la page")
	}
	for rang, suite := range zones {
		zone := suite[:strings.Index(suite, "</div>")]
		if !strings.Contains(zone, `class="depot-choisir"`) {
			t.Errorf("la zone de dépôt n° %d n'a pas de bouton « depot-choisir »", rang+1)
		}
		if strings.Contains(zone, `role="button"`) {
			t.Errorf("la zone de dépôt n° %d se fait passer pour un bouton", rang+1)
		}
	}
}
