package web

import (
	"regexp"
	"strings"
	"testing"
)

// Ces tests tiennent le gabarit commun des écrans : ce qu'une page a le droit
// de porter, et les mots qu'elle emploie. Chaque règle a déjà été enfreinte
// une fois, écran par écran, et l'interface ne se ressemblait plus d'une page
// à l'autre.

// lireActif rend un fichier de l'interface embarquée.
func lireActif(t *testing.T, nom string) string {
	t.Helper()
	contenu, err := assets.ReadFile("assets/" + nom)
	if err != nil {
		t.Fatal(err)
	}
	return string(contenu)
}

// sansCommentaires retire les commentaires d'une page ou d'un script : ce
// qu'ils disent n'est pas montré, et ils citent parfois ce qui est interdit
// pour dire pourquoi.
func sansCommentaires(texte string) string {
	texte = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(texte, "")
	lignes := strings.Split(texte, "\n")
	gardees := lignes[:0]
	for _, ligne := range lignes {
		if strings.HasPrefix(strings.TrimSpace(ligne), "//") {
			continue
		}
		gardees = append(gardees, ligne)
	}
	return strings.Join(gardees, "\n")
}

// Une vue n'a pas de barre à elle : son titre est celui de la page, et ses
// actions sont dans l'en-tête, où chaque vue les déclare sous « data-pour ».
// Une déclaration qui ne nomme aucune vue ne s'afficherait jamais.
func TestLesActionsDUneVueSontDansLEntete(t *testing.T) {
	page := lireActif(t, "index.html")
	if strings.Contains(page, "barre-outils") {
		t.Error("une vue porte encore sa propre barre de titre et d'actions")
	}
	declarations := regexp.MustCompile(`data-pour="([^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(declarations) == 0 {
		t.Fatal("l'en-tête ne déclare rien pour aucune vue")
	}
	for _, declaration := range declarations {
		for _, vue := range strings.Fields(declaration[1]) {
			if !strings.Contains(page, `id="vue-`+vue+`"`) {
				t.Errorf("data-pour nomme « %s », qui n'est pas une vue", vue)
			}
		}
	}
}

// Le fil d'Ariane est le retour : un bouton « Retour » sur quelques écrans
// seulement faisait chercher, sur les autres, celui qui manquait.
func TestAucunEcranNaDeBoutonRetour(t *testing.T) {
	retour := regexp.MustCompile(`>\s*Retour\b|texte: 'Retour( au [^']*)?'`)
	for _, nom := range []string{"index.html", "app.js"} {
		if trouve := retour.FindString(sansCommentaires(lireActif(t, nom))); trouve != "" {
			t.Errorf("%s porte un bouton de retour : %q", nom, trouve)
		}
	}
}

// Un nombre de choses s'accorde, par « pluriel() » : « dépôt(s) » laissait au
// lecteur le soin de le faire, sur une page sur deux.
func TestLesNombresSAccordent(t *testing.T) {
	parentheses := regexp.MustCompile(`\p{L}\(s\)`)
	for _, nom := range []string{"index.html", "app.js"} {
		for numero, ligne := range strings.Split(sansCommentaires(lireActif(t, nom)), "\n") {
			if parentheses.MatchString(ligne) {
				t.Errorf("%s, ligne %d : « (s) » au lieu de pluriel() : %s",
					nom, numero+1, strings.TrimSpace(ligne))
			}
		}
	}
}

// Une idée, un mot. Enregistrer se disait de quatre façons, et voir avant
// d'écrire de sept : on cherchait le bouton sous un autre nom que la veille.
func TestChaqueGesteNaQuUnNom(t *testing.T) {
	anciens := []string{
		"Mémoriser", "Appliquer", "Écrire au registre",
		"Prévisualiser", "Voir ce qui", "Voir le renommage", "Simuler",
	}
	for _, nom := range []string{"index.html", "app.js"} {
		texte := sansCommentaires(lireActif(t, nom))
		for _, mot := range anciens {
			if strings.Contains(texte, mot) {
				t.Errorf("%s emploie encore « %s »", nom, mot)
			}
		}
	}
}

// Une date s'écrit d'une seule façon, par « dateLisible() ». Une date coupée
// à la main — « 2026-11-11 à 15:59 » — se lisait autrement que sa voisine.
func TestLesDatesPassentParUneSeuleFonction(t *testing.T) {
	coupee := regexp.MustCompile(`_at\b[^;\n]*\.slice\(0, ?1[06]\)|\.replace\('T', `)
	if trouve := coupee.FindString(sansCommentaires(lireActif(t, "app.js"))); trouve != "" {
		t.Errorf("une date est mise en forme à la main : %q", trouve)
	}
}
