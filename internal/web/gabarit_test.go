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

// Ce qui s'enregistre se défait aussi : sans « Annuler », une modification
// regrettée ne se défaisait qu'en rechargeant la page, et rien ne disait
// qu'elle attendait encore d'être enregistrée. Il se tient juste à gauche
// d'« Enregistrer » — le geste qui détruit, s'il y en a un, reste à l'autre
// bout de la boîte —, éteint tant que rien n'a bougé, et suivreModifications
// l'allume : un « Annuler » que rien ne suit resterait éteint pour toujours.
func TestChaqueEnregistrerASonAnnuler(t *testing.T) {
	page := sansCommentaires(lireActif(t, "index.html"))
	script := sansCommentaires(lireActif(t, "app.js"))
	enregistrer := regexp.MustCompile(`<button[^>]*>Enregistrer</button>`)
	annulerAGauche := regexp.MustCompile(`<button([^>]*)>Annuler</button>\s*$`)
	eteint := regexp.MustCompile(`\sdisabled\b`)
	identifiant := regexp.MustCompile(`\bid="([^"]+)"`)

	positions := enregistrer.FindAllStringIndex(page, -1)
	if len(positions) == 0 {
		t.Fatal("la page n'a aucun « Enregistrer »")
	}
	for _, position := range positions {
		bouton := page[position[0]:position[1]]
		voisin := annulerAGauche.FindStringSubmatch(page[:position[0]])
		if voisin == nil {
			t.Errorf("%s n'a pas d'« Annuler » juste à sa gauche", bouton)
			continue
		}
		if !eteint.MatchString(voisin[1]) {
			t.Errorf("l'« Annuler » de %s n'est pas éteint d'avance", bouton)
		}
		id := identifiant.FindStringSubmatch(voisin[1])
		if id == nil {
			t.Errorf("l'« Annuler » de %s n'a pas d'id : rien ne peut le suivre", bouton)
			continue
		}
		suivi := regexp.MustCompile(`suivreModifications\([^;]*\$\('` + regexp.QuoteMeta(id[1]) + `'\)`)
		if !suivi.MatchString(script) {
			t.Errorf("« %s » n'est confié à aucun suivreModifications : il resterait éteint", id[1])
		}
	}

	// Un dialogue enregistre aussi — une date cible, un nom. Son « Annuler »
	// reste allumé : rien n'est écrit avant qu'on confirme, et c'est aussi par
	// lui qu'on sort.
	premier := regexp.MustCompile(`<button[^>]*>([^<]*)</button>`)
	for _, menu := range regexp.MustCompile(`(?s)<menu>(.*?)</menu>`).FindAllStringSubmatch(page, -1) {
		bouton := premier.FindStringSubmatch(menu[1])
		if bouton == nil || bouton[1] != "Annuler" {
			t.Errorf("un dialogue ne commence pas par « Annuler » : %s", strings.TrimSpace(menu[1]))
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
